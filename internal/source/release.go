package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/StevenACoffman/gowheels/internal/archive"
	"github.com/StevenACoffman/gowheels/internal/platforms"
)

// maxAPIResponseBytes caps GitHub API response reads (release metadata is never large).
const maxAPIResponseBytes = 4 << 20 // 4 MB

var httpClient = &http.Client{Timeout: 30 * time.Second}

// ReleaseSource downloads binaries from a GitHub Release, extracts them from
// their GoReleaser-produced archives, and caches archives on disk.
type ReleaseSource struct {
	repo     string    // "owner/name"
	version  string    // git tag (e.g. "v1.2.3") or "latest"
	assets   []string  // explicit asset names (overrides auto-detect)
	cacheDir string    // directory for cached archives; "" disables caching
	token    string    // GitHub personal access token (optional)
	stdout   io.Writer // progress output
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// NewReleaseSource creates a ReleaseSource. repo must be "owner/name". version
// may be a full tag (e.g. "v1.2.3") or "" / "latest" to fetch the latest
// release. assets is a comma-separated list of explicit asset names; when
// empty, GoReleaser naming conventions are used. cacheDir defaults to
// $XDG_CACHE_HOME/gowheels when empty. token is an optional GitHub personal
// access token. stdout receives progress output; pass io.Discard to suppress.
func NewReleaseSource(
	repo, version, assets, cacheDir, token string,
	stdout io.Writer,
) *ReleaseSource {
	if cacheDir == "" {
		cacheDir = defaultCacheDir()
	}
	if stdout == nil {
		stdout = io.Discard
	}
	var assetList []string
	for _, a := range strings.Split(assets, ",") {
		a = strings.TrimSpace(a)
		if a != "" {
			assetList = append(assetList, a)
		}
	}
	return &ReleaseSource{
		repo:     repo,
		version:  version,
		assets:   assetList,
		cacheDir: cacheDir,
		token:    token,
		stdout:   stdout,
	}
}

// Resolve fetches the GitHub release, matches each platform to an archive
// asset, downloads and extracts the binary, and returns one Binary per unique
// os/arch pair.
func (s *ReleaseSource) Resolve(
	ctx context.Context,
	name string,
	plats []platforms.Platform,
) ([]Binary, error) {
	release, err := s.fetchRelease(ctx)
	if err != nil {
		return nil, err
	}
	// Strip leading "v" from tag to get the bare version used in asset names.
	tagVersion := strings.TrimPrefix(release.TagName, "v")

	// Index assets by name for O(1) lookup.
	assetByName := make(map[string]ghAsset, len(release.Assets))
	for _, a := range release.Assets {
		assetByName[a.Name] = a
	}

	seen := make(map[string]bool)
	var result []Binary

	for _, p := range plats {
		key := p.GOOS + "/" + p.GOARCH
		if seen[key] {
			continue
		}
		seen[key] = true

		assetName, err := s.matchAsset(release.Assets, name, tagVersion, p)
		if err != nil {
			return nil, fmt.Errorf("platform %s: %w", key, err)
		}

		asset := assetByName[assetName]
		fmt.Fprintf(s.stdout, "  downloading %s...\n", assetName)

		archiveData, err := s.download(ctx, asset.BrowserDownloadURL)
		if err != nil {
			return nil, fmt.Errorf("downloading %s: %w", assetName, err)
		}

		binaryFilename := name + p.BinaryExt()
		binData, err := archive.ExtractBinary(archiveData, p.ArchiveExt, binaryFilename)
		if err != nil {
			return nil, fmt.Errorf("extracting %s from %s: %w", binaryFilename, assetName, err)
		}

		result = append(result, Binary{
			Platform: p,
			Data:     binData,
			Filename: binaryFilename,
		})
	}

	return result, nil
}

// matchAsset finds the release asset for the given platform.
//
// When s.assets is populated the caller has provided explicit names; we pick
// the one whose name contains the platform's OS and arch strings.
//
// Otherwise we try GoReleaser naming conventions:
//  1. {name}_{version}_{OS}_{Arch}.{ext}  (primary)
//  2. {name}_{OS}_{Arch}.{ext}            (fallback, no version)
func (s *ReleaseSource) matchAsset(
	assets []ghAsset,
	name, version string,
	p platforms.Platform,
) (string, error) {
	if len(s.assets) > 0 {
		for _, assetName := range s.assets {
			n := strings.ToLower(assetName)
			if strings.Contains(n, strings.ToLower(p.GoReleaserOS())) &&
				strings.Contains(n, strings.ToLower(p.GoReleaserArch())) {
				return assetName, nil
			}
		}
		return "", fmt.Errorf("no explicit asset matches %s/%s", p.GOOS, p.GOARCH)
	}

	goOS := p.GoReleaserOS()
	goArch := p.GoReleaserArch()
	ext := p.ArchiveExt

	// Primary GoReleaser pattern: {name}_{version}_{OS}_{Arch}.{ext}
	primary := fmt.Sprintf("%s_%s_%s_%s.%s", name, version, goOS, goArch, ext)
	for _, a := range assets {
		if a.Name == primary {
			return a.Name, nil
		}
	}

	// Fallback: {name}_{OS}_{Arch}.{ext}
	fallback := fmt.Sprintf("%s_%s_%s.%s", name, goOS, goArch, ext)
	for _, a := range assets {
		if a.Name == fallback {
			return a.Name, nil
		}
	}

	return "", fmt.Errorf(
		"no release asset found for %s/%s (tried %q and %q)",
		p.GOOS,
		p.GOARCH,
		primary,
		fallback,
	)
}

func (s *ReleaseSource) fetchRelease(ctx context.Context) (*ghRelease, error) {
	var apiURL string
	if s.version == "" || s.version == "latest" {
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", s.repo)
	} else {
		tag := s.version
		if !strings.HasPrefix(tag, "v") {
			tag = "v" + tag
		}
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s", s.repo, tag)
	}

	data, err := s.ghGet(ctx, apiURL)
	if err != nil {
		return nil, fmt.Errorf("fetching release: %w", err)
	}

	var rel ghRelease
	if err := json.Unmarshal(data, &rel); err != nil {
		return nil, fmt.Errorf("parsing GitHub release response: %w", err)
	}
	if rel.TagName == "" {
		return nil, fmt.Errorf("GitHub release has no tag_name (repo: %s)", s.repo)
	}
	return &rel, nil
}

func (s *ReleaseSource) ghGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("building GitHub API request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending GitHub API request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxAPIResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("reading GitHub API response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API %s returned %d: %s", url, resp.StatusCode, string(body))
	}
	return body, nil
}

func (s *ReleaseSource) download(ctx context.Context, url string) ([]byte, error) {
	filename := filepath.Base(url)

	// Cache hit
	if s.cacheDir != "" {
		cached := filepath.Join(s.cacheDir, filename)
		if data, err := os.ReadFile(cached); err == nil {
			return data, nil
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("building download request: %w", err)
	}
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending download request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d downloading %s", resp.StatusCode, url)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading download response: %w", err)
	}

	// Write to cache (best-effort)
	if s.cacheDir != "" {
		if err := os.MkdirAll(s.cacheDir, 0o750); err == nil {
			_ = os.WriteFile(filepath.Join(s.cacheDir, filename), data, 0o644)
		}
	}

	return data, nil
}

func defaultCacheDir() string {
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "gowheels")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".cache", "gowheels")
	}
	return filepath.Join(os.TempDir(), "gowheels-cache")
}
