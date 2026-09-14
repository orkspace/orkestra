// pkg/utils/loadfile.go
package utils

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// httpClient is the shared client for all remote file fetches.
// Timeout prevents Orkestra startup from hanging on a slow or unresponsive source.
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

// FileAuth holds optional authentication for a remote file source.
// Constructed from the Katalog source declaration and resolved from
// environment variables at load time — credentials never appear in YAML.
type FileAuth struct {
	// Type — authentication scheme.
	// Supported: "bearer", "github", "basic"
	Type string

	// BearerToken — used when Type is "bearer" or "github".
	// Resolved from the environment variable named in the source declaration.
	BearerToken string

	// Username and Password — used when Type is "basic".
	// Each resolved from its own environment variable.
	Username string
	Password string
}

// LoadFile loads a file from local disk or HTTP(S).
// For https:// URLs the local cache is checked first; fetches on miss.
// For local files, auth is ignored and no caching is done.
func LoadFile(path string) ([]byte, error) {
	return LoadFileWithAuthRefresh(path, nil, false)
}

// LoadFileWithAuth loads a file with optional authentication.
// For https:// URLs the local cache is checked first; fetches on miss.
func LoadFileWithAuth(path string, auth *FileAuth) ([]byte, error) {
	return LoadFileWithAuthRefresh(path, auth, false)
}

// LoadFileWithAuthRefresh loads a file with optional authentication.
// When refresh is true the cache is bypassed and the remote is fetched fresh.
// For local files, refresh and auth are both ignored.
func LoadFileWithAuthRefresh(path string, auth *FileAuth, refresh bool) ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("file path is empty")
	}

	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		if !refresh {
			if cached, ok := CachedFileBytes(path); ok {
				return cached, nil
			}
		}
		data, err := fetchRemote(path, auth)
		if err != nil {
			return nil, err
		}
		_ = CacheFileBytes(path, data)
		return data, nil
	}

	return ReadLocal(path)
}

// fetchRemote downloads a file over HTTP or HTTPS.
// Builds the request, applies authentication headers, and reads the body.
func fetchRemote(url string, auth *FileAuth) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("building request for %q: %w", url, err)
	}

	// Apply authentication if provided
	if auth != nil {
		if err := applyAuth(req, auth); err != nil {
			return nil, fmt.Errorf("applying auth for %q: %w", url, err)
		}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %q: %w", url, err)
	}
	defer resp.Body.Close()

	// Distinguish auth failures from other errors for clearer error messages
	switch resp.StatusCode {
	case http.StatusOK:
		// success — fall through to read body
	case http.StatusUnauthorized:
		return nil, fmt.Errorf(
			"fetching %q: authentication required (401) — "+
				"check that auth credentials are set and have not expired",
			url,
		)
	case http.StatusForbidden:
		return nil, fmt.Errorf(
			"fetching %q: access denied (403) — "+
				"check that the token has sufficient permissions",
			url,
		)
	case http.StatusNotFound:
		return nil, fmt.Errorf(
			"fetching %q: not found (404) — "+
				"check the URL is correct and the file exists",
			url,
		)
	default:
		return nil, fmt.Errorf("fetching %q: unexpected status %d", url, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading body from %q: %w", url, err)
	}

	return data, nil
}

// applyAuth sets the appropriate Authorization header on the request.
func applyAuth(req *http.Request, auth *FileAuth) error {
	switch strings.ToLower(auth.Type) {

	case "bearer":
		// Generic bearer token — works with most REST APIs
		// Katalog declaration:
		//   auth:
		//     type: bearer
		//     fromEnv: MY_API_TOKEN
		if auth.BearerToken == "" {
			return fmt.Errorf("bearer auth: token is empty — check the fromEnv variable is set")
		}
		req.Header.Set("Authorization", "Bearer "+auth.BearerToken)

	case "github":
		// GitHub token — same header as bearer but documents intent clearly.
		// Works for raw.githubusercontent.com (private repos) and GitHub API.
		// Token needs 'repo' scope for private repository content.
		// Katalog declaration:
		//   auth:
		//     type: github
		//     fromEnv: GITHUB_TOKEN
		if auth.BearerToken == "" {
			return fmt.Errorf("github auth: token is empty — check GITHUB_TOKEN is set")
		}
		req.Header.Set("Authorization", "Bearer "+auth.BearerToken)

	case "basic":
		// HTTP Basic auth — username and password.
		// Used for Artifactory, Nexus, and other corporate artifact stores.
		// Katalog declaration:
		//   auth:
		//     type: basic
		//     usernameFromEnv: ARTIFACTORY_USER
		//     passwordFromEnv: ARTIFACTORY_PASSWORD
		if auth.Username == "" {
			return fmt.Errorf("basic auth: username is empty — check the usernameFromEnv variable is set")
		}
		req.SetBasicAuth(auth.Username, auth.Password)

	default:
		return fmt.Errorf("unsupported auth type %q — supported: bearer, github, basic", auth.Type)
	}

	return nil
}

// ReadLocal reads a file from the local filesystem.
func ReadLocal(path string) ([]byte, error) {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("file %q does not exist", path)
	}
	return os.ReadFile(path)
}
