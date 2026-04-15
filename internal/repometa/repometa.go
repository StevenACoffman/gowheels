// Package repometa fetches GitHub repository metadata for use as wheel
// generation defaults (summary, license, topics, URLs).
package repometa

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var (
	httpClient = &http.Client{Timeout: 30 * time.Second}
	apiBase    = "https://api.github.com"
)

// Repo holds the subset of GitHub repository metadata relevant to wheel
// generation.
type Repo struct {
	// Description is the one-line summary from GitHub ("About" field).
	Description string
	// LicenseSPDX is the SPDX identifier detected by GitHub, or "" when
	// GitHub returns NOASSERTION, "other", or cannot detect a license.
	LicenseSPDX string
	// Homepage is the optional website URL set in repository settings.
	Homepage string
	// HTMLURL is the canonical GitHub URL (https://github.com/owner/repo).
	HTMLURL string
	// Topics are the repository topic tags.
	Topics []string
	// DefaultBranch is the default branch name (usually "main" or "master").
	DefaultBranch string
	// OwnerLogin is the repository owner's GitHub login.
	OwnerLogin string
}

// Fetch retrieves metadata for repo (format: "owner/name") from the GitHub
// REST API. token is optional; omitting it uses unauthenticated requests
// (rate limit: 60 requests per hour per IP).
//
// Fetch returns a best-effort result: it never returns a nil *Repo on
// success — callers should check whether individual fields are empty.
func Fetch(ctx context.Context, repo, token string) (*Repo, error) {
	return fetchURL(ctx, apiBase+"/repos/"+repo, token)
}

func fetchURL(ctx context.Context, url, token string) (*Repo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("building repo metadata request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching repo metadata: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return nil, fmt.Errorf("reading repo metadata response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"GitHub API returned %d for %s: %s",
			resp.StatusCode, url, string(body),
		)
	}

	var raw struct {
		Description   string   `json:"description"`
		Homepage      string   `json:"homepage"`
		HTMLURL       string   `json:"html_url"`
		Topics        []string `json:"topics"`
		DefaultBranch string   `json:"default_branch"`
		License       *struct {
			SPDXID string `json:"spdx_id"`
		} `json:"license"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parsing repo metadata response: %w", err)
	}

	r := &Repo{
		Description:   raw.Description,
		Homepage:      raw.Homepage,
		HTMLURL:       raw.HTMLURL,
		Topics:        raw.Topics,
		DefaultBranch: raw.DefaultBranch,
		OwnerLogin:    raw.Owner.Login,
	}
	if raw.License != nil {
		switch strings.ToUpper(raw.License.SPDXID) {
		case "", "NOASSERTION", "OTHER":
			// GitHub could not identify the license; do not propagate a
			// potentially incorrect SPDX expression.
		default:
			r.LicenseSPDX = raw.License.SPDXID
		}
	}
	return r, nil
}
