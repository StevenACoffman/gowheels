package pypi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var fetchClient = &http.Client{Timeout: 30 * time.Second}

// PackageInfo holds the subset of PyPI JSON API fields used for metadata
// comparison by the "pypidiff" command.
type PackageInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Summary string `json:"summary"`
	// Description is the long description (README body) stored on PyPI.
	// It is the text that appears on the project page under "Project description".
	Description string `json:"description"`
	// DescriptionContentType is the MIME type of Description, e.g.
	// "text/markdown" or "text/x-rst". Empty when no description is present.
	DescriptionContentType string `json:"description_content_type"`
	// Keywords is the space-separated keyword string as stored on PyPI,
	// matching what wheel METADATA writes via "Keywords: a b c".
	Keywords          string            `json:"keywords"`
	Classifiers       []string          `json:"classifiers"`
	ProjectURLs       map[string]string `json:"project_urls"`
	LicenseExpression string            `json:"license_expression"`
	RequiresPython    string            `json:"requires_python"`
	// Yanked is true when the release was removed from the simple index
	// but is still accessible via the JSON API.
	Yanked       bool   `json:"yanked"`
	YankedReason string `json:"yanked_reason"`
}

// IsGitHostingURL reports whether rawURL belongs to a known Git-hosting
// domain (github.com, gitlab.com, codeberg.org) where /issues and
// /releases are standard sidebar paths.
func IsGitHostingURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Hostname()) {
	case "github.com", "gitlab.com", "codeberg.org":
		return true
	}
	return false
}

// FetchPackageInfo retrieves metadata for name from the PyPI JSON API.
// When version is empty the latest release is returned; otherwise the
// specific version is fetched from https://pypi.org/pypi/{name}/{version}/json.
// name may be normalised or not — PyPI accepts both forms.
func FetchPackageInfo(ctx context.Context, name, version string) (*PackageInfo, error) {
	apiURL := "https://pypi.org/pypi/" + name
	if version != "" {
		apiURL += "/" + version
	}
	apiURL += "/json"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("building PyPI request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := fetchClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching PyPI data: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading PyPI response: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		if version != "" {
			return nil, fmt.Errorf("package %q version %q not found on PyPI", name, version)
		}
		return nil, fmt.Errorf("package %q not found on PyPI", name)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("PyPI returned %d: %s", resp.StatusCode, body)
	}

	var envelope struct {
		Info PackageInfo `json:"info"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("parsing PyPI response: %w", err)
	}
	return &envelope.Info, nil
}
