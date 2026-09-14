//go:build !runtime && !gateway

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	oidcpkg "github.com/orkspace/orkestra/pkg/gateway/oidc"
	orktypes "github.com/orkspace/orkestra/pkg/types"
	"github.com/spf13/cobra"
)

const fileToken = "token.jwt"

// ── ork token ─────────────────────────────────────────────────────────────────

var tokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Inspect and verify Gateway API tokens",
	Long: `Inspect and verify tokens configured in gateway.api.auth.tokens.

Subcommands:
  verify    Verify a JWT against the configured token entries
  probe     Probe the OIDC discovery endpoint for a token entry
  list      List all configured token entries`,
}

// ── ork token verify ──────────────────────────────────────────────────────────

var tokenVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify a JWT against the configured token entries",
	Long: `Verify a JWT against gateway.api.auth.tokens in the katalog.

Local mode (default): loads the katalog, fetches JWKS from the real provider,
verifies the token signature and claims, and shows which entry matched.

Live mode (--api): sends the token to a running gateway and reports accept/reject.
Use ork proxy to expose the gateway locally first.

Examples:
  ork token verify
  ork token verify -f katalog.yaml -t token.jwt
  ork token verify --api https://gateway.myorg.io -t token.jwt
  ork token verify --api http://localhost:8443`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		tokenFile, _ := cmd.Flags().GetString("token")
		apiURL, _ := cmd.Flags().GetString("api")
		audienceOverride, _ := cmd.Flags().GetString("audience")

		if tokenFile == "" {
			tokenFile = fileToken
		}

		jwt, err := readTokenFile(tokenFile)
		if err != nil {
			return fmt.Errorf("%s reading token file %q: %w", failureMark(), tokenFile, err)
		}

		if apiURL != "" {
			return runTokenVerifyLive(cmd, apiURL, jwt)
		}
		return runTokenVerifyLocal(cmd, jwt, audienceOverride, tokenFile)
	},
}

func runTokenVerifyLive(cmd *cobra.Command, apiURL, jwt string) error {
	endpoint := strings.TrimRight(apiURL, "/") + "/api/v1/schema"
	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("%s building request: %w", failureMark(), err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s gateway unreachable at %s: %w", failureMark(), apiURL, err)
	}
	defer resp.Body.Close()

	fmt.Printf("\n  %s  %s\n\n", gray("token verify"), cyan("live"))
	fmt.Printf("  gateway  %s\n", bold(apiURL))
	fmt.Printf("  issuer   %s\n\n", dim(issuerFromJWT(jwt)))

	if resp.StatusCode == http.StatusUnauthorized {
		fmt.Printf("  %s %s\n\n", red("✗"), red("token rejected (401 Unauthorized)"))
		return fmt.Errorf("token rejected")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s unexpected status %d: %s", failureMark(), resp.StatusCode, strings.TrimSpace(string(body)))
	}

	fmt.Printf("  %s token accepted\n\n", green("✓"))
	return nil
}

func runTokenVerifyLocal(cmd *cobra.Command, jwt, audienceOverride, tokenFile string) error {
	iss := issuerFromJWT(jwt)
	if iss == "" {
		return fmt.Errorf("%s %q does not look like a JWT — could not extract iss claim", failureMark(), tokenFile)
	}

	k, err := buildKatalog(cmd)
	if err != nil {
		return fmt.Errorf("%s %w", failureMark(), err)
	}

	if !k.IsGatewayEnabled() || !k.Gateway.HasAPI() || k.Gateway.API.Auth.Empty() {
		return fmt.Errorf("%s no gateway token auth configured in this katalog", failureMark())
	}

	var candidates []orktypes.APIToken
	for _, t := range k.Gateway.API.Auth.Tokens {
		if t.IsOIDC() && t.OIDCIssuer() == iss {
			candidates = append(candidates, t)
		}
	}

	fmt.Printf("\n  %s  %s\n\n", gray("token verify"), cyan("local"))
	fmt.Printf("  token file  %s\n", bold(tokenFile))
	fmt.Printf("  issuer      %s\n", bold(iss))

	if len(candidates) == 0 {
		fmt.Printf("\n  %s no token entry with issuer %q\n\n", red("✗"), iss)
		return fmt.Errorf("no matching token entry")
	}
	fmt.Printf("  candidates  %s\n", bold(fmt.Sprintf("%d", len(candidates))))

	cache := oidcpkg.NewCache(oidcpkg.DefaultTTL)
	matched := ""

	for _, entry := range candidates {
		fmt.Printf("\n  %s\n", dim(strings.Repeat("─", 52)))
		fmt.Printf("  %s  %s\n\n", cyan(bold(entry.Name)), dim(entry.OIDCKind()))

		audience := entry.OIDCAudience()
		if audienceOverride != "" {
			audience = audienceOverride
		}

		claims, err := cache.Verify(entry.OIDCIssuer(), entry.OIDCDiscoveryBase(), jwt, audience)
		if err != nil {
			fmt.Printf("  %s %s\n", red("✗"), red(err.Error()))
			continue
		}
		fmt.Printf("  %s signature valid\n", green("✓"))
		fmt.Printf("  %s not expired\n", green("✓"))
		fmt.Printf("  %s issuer matched\n", green("✓"))

		if !entry.MatchesOIDCClaims(claims) {
			fmt.Printf("  %s claims did not match allow block\n\n", red("✗"))
			printClaimsTable(claims)
			continue
		}
		fmt.Printf("  %s claims matched\n\n", green("✓"))
		printClaimsTable(claims)
		matched = entry.Name
		break
	}

	fmt.Printf("\n  %s\n", dim(strings.Repeat("─", 52)))
	if matched != "" {
		fmt.Printf("  %s matched: %s\n\n", green("✓"), bold(matched))
		return nil
	}
	fmt.Printf("  %s no matching token entry\n\n", red("✗"))
	return fmt.Errorf("no matching token entry")
}

func printClaimsTable(claims map[string]string) {
	keys := make([]string, 0, len(claims))
	for k := range claims {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, k := range keys {
		fmt.Fprintf(w, "    %s\t%s\n", dim(k), claims[k])
	}
	w.Flush()
}

// ── ork token probe ───────────────────────────────────────────────────────────

var tokenProbeCmd = &cobra.Command{
	Use:   "probe",
	Short: "Probe the OIDC discovery endpoint for a token entry",
	Long: `Probe the OIDC discovery endpoint for a configured token entry.

Fetches the discovery document and JWKS, then reports the result. Useful for
confirming a provider endpoint is reachable before deploying — especially for
Vault, which uses a non-standard discovery path.

Example:
  ork token probe --name vault-ci
  ork token probe -f katalog.yaml --name gh-ci`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		if name == "" {
			return fmt.Errorf("%s --name is required", failureMark())
		}

		k, err := buildKatalog(cmd)
		if err != nil {
			return fmt.Errorf("%s %w", failureMark(), err)
		}

		if !k.IsGatewayEnabled() || !k.Gateway.HasAPI() || k.Gateway.API.Auth.Empty() {
			return fmt.Errorf("%s no gateway token auth configured in this katalog", failureMark())
		}

		var entry *orktypes.APIToken
		for i, t := range k.Gateway.API.Auth.Tokens {
			if t.Name == name {
				entry = &k.Gateway.API.Auth.Tokens[i]
				break
			}
		}
		if entry == nil {
			return fmt.Errorf("%s token entry %q not found", failureMark(), name)
		}
		if !entry.IsOIDC() {
			return fmt.Errorf("%s token entry %q is not an OIDC type (kind: static)", failureMark(), name)
		}

		discoveryBase := entry.OIDCDiscoveryBase()
		discoveryURL := discoveryBase + "/.well-known/openid-configuration"

		fmt.Printf("\n  %s  %s\n\n", gray("token probe"), cyan(name))
		fmt.Printf("  kind       %s\n", bold(entry.OIDCKind()))
		fmt.Printf("  issuer     %s\n", entry.OIDCIssuer())
		fmt.Printf("  discovery  %s\n\n", dim(discoveryURL))

		jwksURI, err := probeDiscovery(discoveryURL)
		if err != nil {
			fmt.Printf("  %s discovery failed: %s\n\n", red("✗"), red(err.Error()))
			return fmt.Errorf("probe failed")
		}
		fmt.Printf("  %s discovery reachable\n", green("✓"))
		fmt.Printf("  jwks_uri   %s\n\n", dim(jwksURI))

		keyCount, algs, err := probeJWKS(jwksURI)
		if err != nil {
			fmt.Printf("  %s JWKS fetch failed: %s\n\n", red("✗"), red(err.Error()))
			return fmt.Errorf("probe failed")
		}
		fmt.Printf("  %s JWKS reachable\n", green("✓"))
		fmt.Printf("  keys       %s  (%s)\n\n", bold(fmt.Sprintf("%d", keyCount)), strings.Join(algs, ", "))

		return nil
	},
}

func probeDiscovery(discoveryURL string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(discoveryURL)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", discoveryURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: status %d", discoveryURL, resp.StatusCode)
	}
	var doc struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", fmt.Errorf("decoding discovery document: %w", err)
	}
	if doc.JWKSURI == "" {
		return "", fmt.Errorf("discovery document missing jwks_uri")
	}
	return doc.JWKSURI, nil
}

func probeJWKS(jwksURI string) (int, []string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(jwksURI)
	if err != nil {
		return 0, nil, fmt.Errorf("GET %s: %w", jwksURI, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, nil, fmt.Errorf("GET %s: status %d", jwksURI, resp.StatusCode)
	}
	var ks struct {
		Keys []struct {
			Alg string `json:"alg"`
			Kty string `json:"kty"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ks); err != nil {
		return 0, nil, fmt.Errorf("decoding JWKS: %w", err)
	}
	algs := make([]string, 0, len(ks.Keys))
	for _, k := range ks.Keys {
		if k.Alg != "" {
			algs = append(algs, k.Alg)
		} else {
			algs = append(algs, k.Kty)
		}
	}
	return len(ks.Keys), algs, nil
}

// ── ork token list ────────────────────────────────────────────────────────────

var tokenListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configured token entries",
	Long: `List all token entries in gateway.api.auth.tokens.

Shows each token's name, type, provider kind, and allow summary.

Example:
  ork token list
  ork token list -f katalog.yaml`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		k, err := buildKatalog(cmd)
		if err != nil {
			return fmt.Errorf("%s %w", failureMark(), err)
		}

		if !k.IsGatewayEnabled() || !k.Gateway.HasAPI() || k.Gateway.API.Auth.Empty() {
			fmt.Printf("\n  %s no gateway token auth configured\n\n", dim("—"))
			return nil
		}

		tokens := k.Gateway.API.Auth.Tokens
		fmt.Printf("\n  %s  %s\n\n", gray("token list"), dim(fmt.Sprintf("%d entries", len(tokens))))

		type row struct{ name, typ, provider, allow string }
		rows := make([]row, len(tokens))
		wName, wType, wProv := len("NAME"), len("TYPE"), len("PROVIDER")
		for i, t := range tokens {
			typ, provider, allow := tokenListRow(t)
			rows[i] = row{t.Name, typ, provider, allow}
			if len(t.Name) > wName {
				wName = len(t.Name)
			}
			if len(typ) > wType {
				wType = len(typ)
			}
			if len(provider) > wProv {
				wProv = len(provider)
			}
		}

		gap := 4
		pad := func(s string, w int) string { return s + strings.Repeat(" ", w-len(s)) }
		fmt.Printf("  %s%s%s%s\n",
			bold(pad("NAME", wName+gap)),
			bold(pad("TYPE", wType+gap)),
			bold(pad("PROVIDER", wProv+gap)),
			bold("ALLOW"),
		)
		for _, r := range rows {
			fmt.Printf("  %s%s%s%s\n",
				cyan(r.name)+strings.Repeat(" ", wName+gap-len(r.name)),
				pad(r.typ, wType+gap),
				pad(r.provider, wProv+gap),
				dim(r.allow),
			)
		}
		fmt.Println()
		return nil
	},
}

func tokenListRow(t orktypes.APIToken) (typ, provider, allow string) {
	switch {
	case t.GitHubOIDC != nil:
		return "oidc", "github", allowSummaryGitHub(t.GitHubOIDC.Allow)
	case t.GitLabOIDC != nil:
		return "oidc", "gitlab", allowSummaryGitLab(t.GitLabOIDC.Allow)
	case t.VaultOIDC != nil:
		return "oidc", "vault", allowSummaryVault(t.VaultOIDC)
	case t.OIDC != nil:
		return "oidc", "generic", "issuer=" + t.OIDC.Issuer + " " + allowSummaryMap(t.OIDC.Allow)
	case t.SecretRef != nil:
		ra := ""
		if t.SecretRef.RotateAfter != "" {
			ra = "rotateAfter=" + t.SecretRef.RotateAfter
		}
		return "static", "secretRef", ra
	case t.Token != "":
		return "static", "token", "(env var)"
	default:
		return "unknown", "—", "—"
	}
}

func allowSummaryGitHub(a orktypes.GitHubOIDCClaims) string {
	var parts []string
	if a.Repository != "" {
		parts = append(parts, "repository="+a.Repository)
	}
	if a.RepositoryOwner != "" {
		parts = append(parts, "repositoryOwner="+a.RepositoryOwner)
	}
	if a.Ref != "" {
		parts = append(parts, "ref="+a.Ref)
	}
	if a.Workflow != "" {
		parts = append(parts, "workflow="+a.Workflow)
	}
	if a.Environment != "" {
		parts = append(parts, "environment="+a.Environment)
	}
	if a.JobWorkflowRef != "" {
		parts = append(parts, "jobWorkflowRef="+a.JobWorkflowRef)
	}
	return strings.Join(parts, " ")
}

func allowSummaryGitLab(a orktypes.GitLabOIDCClaims) string {
	var parts []string
	if a.NamespacePath != "" {
		parts = append(parts, "namespacePath="+a.NamespacePath)
	}
	if a.RefProtected != "" {
		parts = append(parts, "refProtected="+a.RefProtected)
	}
	if a.Environment != "" {
		parts = append(parts, "environment="+a.Environment)
	}
	return strings.Join(parts, " ")
}

func allowSummaryVault(v *orktypes.VaultOIDC) string {
	parts := []string{"url=" + v.URL}
	if v.Allow.EntityName != "" {
		parts = append(parts, "entityName="+v.Allow.EntityName)
	}
	if v.Allow.EntityID != "" {
		parts = append(parts, "entityID="+v.Allow.EntityID)
	}
	if v.Allow.Namespace != "" {
		parts = append(parts, "namespace="+v.Allow.Namespace)
	}
	for k, val := range v.Allow.Allow {
		parts = append(parts, k+"="+val)
	}
	return strings.Join(parts, " ")
}

func allowSummaryMap(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(m))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, " ")
}

// ── helpers ───────────────────────────────────────────────────────────────────

func readTokenFile(path string) (string, error) {
	data, err := readLocal(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// issuerFromJWT decodes the iss claim from a JWT without verifying the signature.
// Returns "" if the string is not a valid JWT.
func issuerFromJWT(token string) string {
	iss, _ := oidcpkg.IssuerFromToken(token)
	return iss
}

// ── init ──────────────────────────────────────────────────────────────────────

func init() {
	tokenVerifyCmd.Flags().StringP("token", "t", "", "File containing the JWT (default: token.jwt)")
	tokenVerifyCmd.Flags().String("api", "", "Gateway base URL for live mode (e.g. http://localhost:8080)")
	tokenVerifyCmd.Flags().String("audience", "", "Override audience check (local mode only)")

	tokenProbeCmd.Flags().StringP("name", "n", "", "Token entry name to probe")

	tokenCmd.AddCommand(tokenVerifyCmd)
	tokenCmd.AddCommand(tokenProbeCmd)
	tokenCmd.AddCommand(tokenListCmd)
	rootCmd.AddCommand(tokenCmd)

	shadowGlobalCommandFlags(tokenCmd)
}
