package oidc

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// supportedAlgs are the signature algorithms we accept.
// RS256 is used by GitHub and most OIDC providers; ES256 by some newer ones.
var supportedAlgs = []jose.SignatureAlgorithm{
	jose.RS256,
	jose.RS384,
	jose.RS512,
	jose.ES256,
	jose.ES384,
	jose.ES512,
}

// Verify validates a JWT bearer token against the issuer's JWKS and returns
// the token's claims as a flat string map.
//
// discoveryBase is the base URL used to locate the OIDC discovery document.
// For most providers it equals issuerURL. Vault is the exception: its discovery
// document lives at {url}/v1/identity/oidc/.well-known/openid-configuration,
// so callers pass {url}/v1/identity/oidc as discoveryBase.
//
// It checks:
//   - Signature validity (via cached JWKS)
//   - Expiry (exp claim)
//   - Issuer match (iss claim must equal issuerURL)
//   - Audience (aud claim must contain audience, when audience is non-Empty()
//
// The returned map contains every claim in the JWT payload whose value is a
// string or number. Structured claims (arrays, objects) are skipped — callers
// that need them can re-parse the raw payload.
func (c *Cache) Verify(issuerURL, discoveryBase, token, audience string) (map[string]string, error) {
	ks, err := c.keys(issuerURL, discoveryBase)
	if err != nil {
		return nil, err
	}

	// Parse the signed JWT — this selects the key by kid and verifies the signature.
	sig, err := jose.ParseSigned(token, supportedAlgs)
	if err != nil {
		return nil, fmt.Errorf("parsing token: %w", err)
	}

	// sig.Headers[0].KeyID is the kid from the JWT header.
	// We need to find the matching key in the JWKS.
	kid := ""
	if len(sig.Signatures) > 0 {
		kid = sig.Signatures[0].Header.KeyID
	}
	key, err := lookupKey(ks, kid)
	if err != nil {
		return nil, err
	}

	payload, err := sig.Verify(key)
	if err != nil {
		return nil, fmt.Errorf("signature verification failed: %w", err)
	}

	// Decode the payload into a raw claims map.
	var raw map[string]interface{}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("decoding token payload: %w", err)
	}

	if err := checkStandardClaims(raw, issuerURL, audience); err != nil {
		return nil, err
	}

	return flattenClaims(raw), nil
}

// lookupKey finds the signing key in the JWKS by kid.
// When kid is empty (some providers omit it) and there is exactly one key,
// that key is used.
func lookupKey(ks *jose.JSONWebKeySet, kid string) (interface{}, error) {
	if kid != "" {
		keys := ks.Key(kid)
		if len(keys) == 0 {
			return nil, fmt.Errorf("no key with kid %q in JWKS", kid)
		}
		return keys[0].Public().Key, nil
	}
	if len(ks.Keys) == 1 {
		return ks.Keys[0].Public().Key, nil
	}
	return nil, fmt.Errorf("token has no kid and JWKS has %d keys — cannot select key", len(ks.Keys))
}

func checkStandardClaims(raw map[string]interface{}, issuerURL, audience string) error {
	// exp — must not be in the past
	if exp, ok := raw["exp"]; ok {
		var expTime time.Time
		switch v := exp.(type) {
		case float64:
			expTime = time.Unix(int64(v), 0)
		case json.Number:
			n, _ := v.Int64()
			expTime = time.Unix(n, 0)
		}
		if !expTime.IsZero() && time.Now().After(expTime) {
			return fmt.Errorf("token has expired")
		}
	}

	// iss — must match issuerURL exactly
	if iss, ok := raw["iss"].(string); !ok || iss != issuerURL {
		got := ""
		if s, ok := raw["iss"].(string); ok {
			got = s
		}
		return fmt.Errorf("token issuer %q does not match expected %q", got, issuerURL)
	}

	// aud — when audience is configured, the claim must contain it
	if audience != "" {
		if !audContains(raw["aud"], audience) {
			return fmt.Errorf("token audience does not include %q", audience)
		}
	}

	return nil
}

// audContains checks whether the aud claim (string or []string) contains target.
func audContains(aud interface{}, target string) bool {
	switch v := aud.(type) {
	case string:
		return v == target
	case []interface{}:
		for _, a := range v {
			if s, ok := a.(string); ok && s == target {
				return true
			}
		}
	}
	return false
}

// IssuerFromToken decodes the payload of a JWT without verifying the signature
// and returns the iss claim. Returns ("", false) if the value is not a
// three-segment JWT or has no iss claim.
//
// This is used only for provider selection — picking which OIDC token entry to
// try. Actual signature verification always happens in Cache.Verify.
func IssuerFromToken(token string) (string, bool) {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return "", false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", false
	}
	var claims struct {
		Iss string `json:"iss"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", false
	}
	return claims.Iss, claims.Iss != ""
}

// flattenClaims converts a raw claims map to map[string]string.
// Scalar values (strings, numbers, booleans) are converted to their string
// representation. Arrays and objects are skipped.
func flattenClaims(raw map[string]interface{}) map[string]string {
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		switch val := v.(type) {
		case string:
			out[k] = val
		case float64:
			out[k] = strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", val), "0"), ".")
		case json.Number:
			out[k] = val.String()
		case bool:
			if val {
				out[k] = "true"
			} else {
				out[k] = "false"
			}
		}
	}
	return out
}
