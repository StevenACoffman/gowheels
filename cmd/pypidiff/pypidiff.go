// Package pypidiff implements the "pypidiff" CLI command.
//
// It renders the same METADATA that "gowheels pypi" would write into a wheel,
// fetches the current metadata from PyPI's JSON API, and emits the full local
// text followed by a unified diff. Exit code 0 means no differences; exit
// code 1 means at least one field differs.
package pypidiff

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/gowheels/cmd/root"
	"github.com/StevenACoffman/gowheels/internal/gomod"
	pypiclient "github.com/StevenACoffman/gowheels/internal/pypi"
	"github.com/StevenACoffman/gowheels/internal/repometa"
	"github.com/StevenACoffman/gowheels/internal/wheel"
)

// Config holds the configuration for the pypidiff command.
type Config struct {
	*root.Config

	// Metadata flags — mirror the subset of pypi flags that affect METADATA.
	Name        string
	PackageName string
	Summary     string
	LicenseExpr string
	URL         string

	// ReadmePath and NoReadme mirror the pypi command's readme flags.
	// ReadmePath empty → auto-detect (README.md/.rst/.txt/README in CWD).
	// NoReadme true → no long description, matching --no-readme behaviour.
	ReadmePath string
	NoReadme   bool

	// Version pins the PyPI release to compare against; empty means latest.
	Version string

	// GitHub release-mode flags (metadata auto-population only).
	Repo        string
	GitHubToken string

	// ModDir is used only for go.mod URL auto-detection.
	ModDir string

	Flags   *ff.FlagSet
	Command *ff.Command
}

// New creates and registers the pypidiff command with the given parent config.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("pypidiff").SetParent(parent.Flags)
	cfg.registerFlags()
	cfg.Command = &ff.Command{
		Name:      "pypidiff",
		Usage:     "gowheels pypidiff --name <name> [flags]",
		ShortHelp: "diff local metadata against what is published on PyPI",
		LongHelp: `Print the METADATA that "gowheels pypi" would generate and show a unified
diff against the current metadata published on PyPI.

Metadata is assembled the same way "gowheels pypi" does: go.mod provides the
repository URL, GitHub auto-populates summary, license, and keywords (topics)
when --repo is set, and explicit flags override those defaults.

Output has two sections:
  1. The full local METADATA text (what would be embedded in the wheel).
  2. A unified diff of that text against the PyPI-registered metadata.
     README bodies longer than 512 bytes are truncated in the diff to keep
     output readable; the full body appears in section 1.

By default the latest PyPI release is compared. Use --version to pin to a
specific release — useful for verifying that a past upload matches expectations.

Exit codes:
  0  no differences found
  1  one or more fields differ
  2  invocation error (missing flag, network failure, package not found)

Examples:
  # Auto-detect metadata from GitHub, compare against latest PyPI release:
  gowheels pypidiff --name mytool --repo owner/mytool

  # Compare against a specific release:
  gowheels pypidiff --name mytool --repo owner/mytool --version 1.2.0

  # Explicit overrides (no GitHub API call):
  gowheels pypidiff --name mytool --summary "my tool" --license-expr MIT`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) registerFlags() {
	cfg.Flags.StringVar(&cfg.Name, 0, "name", "", "binary name / PyPI package name (required)")
	cfg.Flags.StringVar(
		&cfg.PackageName,
		0,
		"package-name",
		"",
		"Python package name on PyPI (default: --name)",
	)
	cfg.Flags.StringVar(&cfg.Summary, 0, "summary", "", "one-line package description")
	cfg.Flags.StringVar(
		&cfg.LicenseExpr,
		0,
		"license-expr",
		"",
		"SPDX license expression (auto-detected from GitHub when --repo is set)",
	)
	cfg.Flags.StringVar(
		&cfg.URL,
		0,
		"url",
		"",
		"project URL (auto-detected from go.mod and GitHub when --repo is set)",
	)
	cfg.Flags.StringVar(
		&cfg.Version,
		0,
		"version",
		"",
		"PyPI release version to compare against (default: latest)",
	)
	cfg.Flags.StringVar(&cfg.Repo, 0, "repo", "", "GitHub repo in owner/name format")
	cfg.Flags.StringVar(
		&cfg.GitHubToken,
		0,
		"github-token",
		"",
		"GitHub personal access token (avoids API rate limits)",
	)
	cfg.Flags.StringVar(
		&cfg.ModDir,
		0,
		"mod-dir",
		"",
		"directory containing go.mod for URL auto-detection (default: .)",
	)
	cfg.Flags.StringVar(
		&cfg.ReadmePath,
		0,
		"readme",
		"",
		"path to README file for long-description comparison (default: auto-detect README.md/.rst/.txt/README in .)",
	)
	cfg.Flags.BoolVar(
		&cfg.NoReadme,
		0,
		"no-readme",
		"treat local long description as absent (mirrors gowheels pypi --no-readme)",
	)
}

func (cfg *Config) exec(ctx context.Context, _ []string) error {
	if cfg.Name == "" {
		return errors.New("--name is required")
	}

	level := slog.LevelInfo
	if cfg.Debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(cfg.Stderr, &slog.HandlerOptions{Level: level}))

	cfg.resolveEnvTokens()
	cfg.resolveURL()

	pypiName := cfg.Name
	if cfg.PackageName != "" {
		pypiName = cfg.PackageName
	}
	normName := wheel.NormalizeName(pypiName)

	// Fetch GitHub metadata and PyPI info concurrently — they are independent.
	ghMeta, remote, err := cfg.fetchAll(ctx, logger, normName, cfg.Version)
	if err != nil {
		return err
	}
	cfg.applyGitHubMeta(ctx, ghMeta, logger)

	if remote.Yanked {
		reason := remote.YankedReason
		if reason == "" {
			reason = "no reason given"
		}
		fmt.Fprintf(cfg.Stderr, "pypidiff: warning: %s v%s was yanked (%s)\n",
			normName, remote.Version, reason)
	}

	// Resolve README path: --no-readme passes "-" to disable auto-detection.
	readmePath := cfg.ReadmePath
	if cfg.NoReadme {
		readmePath = "-"
	}

	// Keywords come from GitHub topics, matching pypi command behaviour.
	var keywords []string
	if ghMeta != nil {
		keywords = ghMeta.Topics
	}

	// Build the wheel.Config that "gowheels pypi" would use for this package.
	// remote.Version is used as the version because pypidiff compares against a
	// specific published release (Development Status classifier is version-dependent).
	whlCfg := wheel.Config{
		RawName:     cfg.Name,
		PackageName: cfg.PackageName,
		Summary:     cfg.Summary,
		URL:         cfg.URL,
		LicenseExpr: cfg.LicenseExpr,
		ReadmePath:  readmePath,
		Keywords:    keywords,
		ExtraURLs:   buildExtraURLPairs(cfg.URL, ghMeta),
	}
	classifiers := buildClassifiers(remote.Version)
	localText := wheel.BuildMetadataText(whlCfg, remote.Version, classifiers)
	return cfg.printDiffReport(localText, remote, normName)
}

// printDiffReport prints the local METADATA text followed by a unified diff
// against the PyPI-registered metadata for the same package. It returns
// root.ExitError(1) when differences are found, nil otherwise.
func (cfg *Config) printDiffReport(
	localText string,
	remote *pypiclient.PackageInfo,
	normName string,
) error {
	// Print the full local METADATA — this is what gowheels pypi would have
	// embedded in the wheel.
	fmt.Fprintf(cfg.Stdout, "=== local METADATA (gowheels pypi would generate) ===\n")
	fmt.Fprint(cfg.Stdout, localText)
	if !strings.HasSuffix(localText, "\n") {
		fmt.Fprintln(cfg.Stdout)
	}
	fmt.Fprintln(cfg.Stdout)

	// Render PyPI-registered metadata in the same RFC 822 format.
	remoteText := pypiclient.RenderAsMetadata(remote)

	// Truncate README bodies before diffing so the output stays readable.
	// The full README was already printed in the local section above.
	localDiff := pypiclient.TruncateReadmeBody(localText)
	remoteDiff := pypiclient.TruncateReadmeBody(remoteText)

	fmt.Fprintf(cfg.Stdout, "=== diff (local vs PyPI %s v%s) ===\n", normName, remote.Version)
	diff := pypiclient.UnifiedDiff(
		"local (gowheels pypi)",
		fmt.Sprintf("remote (PyPI %s v%s)", normName, remote.Version),
		localDiff,
		remoteDiff,
	)
	if diff == "" {
		fmt.Fprintf(cfg.Stdout, "no differences\n")
		return nil
	}
	fmt.Fprint(cfg.Stdout, diff)
	return root.ExitError(1)
}

// resolveEnvTokens reads the GitHub token from the environment when it was
// not supplied via --github-token, mirroring pypi command behaviour.
func (cfg *Config) resolveEnvTokens() {
	if cfg.GitHubToken != "" {
		return
	}
	for _, env := range []string{"GOWHEELS_GITHUB_TOKEN", "GITHUB_TOKEN"} {
		if v := os.Getenv(env); v != "" {
			cfg.GitHubToken = v
			return
		}
	}
}

// resolveURL populates cfg.URL from go.mod when not already set.
func (cfg *Config) resolveURL() {
	if cfg.URL != "" {
		return
	}
	modDir := cfg.ModDir
	if modDir == "" {
		modDir = "."
	}
	cfg.URL = gomod.RepoURL(modDir)
}

// fetchGitHubMeta fetches GitHub repository metadata. Returns nil on failure
// (non-fatal; callers treat GitHub metadata as best-effort).
func (cfg *Config) fetchGitHubMeta(ctx context.Context, logger *slog.Logger) *repometa.Repo {
	repo := cfg.Repo
	if repo == "" {
		repo = os.Getenv("GITHUB_REPOSITORY")
	}
	if repo == "" {
		return nil
	}
	logger.DebugContext(ctx, "fetching GitHub repository metadata", "repo", repo)
	meta, err := repometa.Fetch(ctx, repo, cfg.GitHubToken)
	if err != nil {
		logger.DebugContext(ctx, "could not fetch GitHub metadata (non-fatal)", "error", err)
		return nil
	}
	return meta
}

// fetchAll retrieves GitHub metadata and PyPI package info concurrently.
// The two calls are independent; running them in parallel halves the total
// round-trip time on typical network conditions.
func (cfg *Config) fetchAll(
	ctx context.Context,
	logger *slog.Logger,
	normName, version string,
) (*repometa.Repo, *pypiclient.PackageInfo, error) {
	var (
		ghMeta    *repometa.Repo
		remote    *pypiclient.PackageInfo
		remoteErr error
	)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		ghMeta = cfg.fetchGitHubMeta(ctx, logger)
	}()
	go func() {
		defer wg.Done()
		logger.DebugContext(ctx, "fetching PyPI package info", "name", normName, "version", version)
		remote, remoteErr = pypiclient.FetchPackageInfo(ctx, normName, version)
	}()
	wg.Wait()
	if remoteErr != nil {
		return nil, nil, fmt.Errorf("fetching PyPI data: %w", remoteErr)
	}
	return ghMeta, remote, nil
}

// applyGitHubMeta fills empty metadata fields from the GitHub repo response.
func (cfg *Config) applyGitHubMeta(ctx context.Context, meta *repometa.Repo, logger *slog.Logger) {
	if meta == nil {
		return
	}
	if cfg.Summary == "" && meta.Description != "" {
		cfg.Summary = meta.Description
		logger.InfoContext(
			ctx,
			"auto-populated from GitHub",
			"field",
			"summary",
			"value",
			cfg.Summary,
		)
	}
	if cfg.LicenseExpr == "" && meta.LicenseSPDX != "" {
		cfg.LicenseExpr = meta.LicenseSPDX
		logger.InfoContext(
			ctx,
			"auto-populated from GitHub",
			"field",
			"license-expr",
			"value",
			cfg.LicenseExpr,
		)
	}
	if cfg.URL == "" && meta.HTMLURL != "" {
		cfg.URL = meta.HTMLURL
		logger.InfoContext(ctx, "auto-populated from GitHub", "field", "url", "value", cfg.URL)
	}
}

// buildClassifiers returns the platform-independent trove classifiers that
// "gowheels pypi" always emits. OS-specific classifiers are omitted because
// they depend on the binary platforms chosen at upload time.
func buildClassifiers(version string) []string {
	return wheel.PlatformIndependentClassifiers(version)
}

// buildExtraURLPairs constructs the extra Project-URL entries (beyond
// Repository) that the pypi command appends. Returns [][2]string for direct
// use in wheel.Config.ExtraURLs.
func buildExtraURLPairs(repoURL string, meta *repometa.Repo) [][2]string {
	var pairs [][2]string
	if meta != nil && meta.Homepage != "" {
		pairs = append(pairs, [2]string{"Homepage", meta.Homepage})
	}
	if repoURL != "" && pypiclient.IsGitHostingURL(repoURL) {
		pairs = append(pairs,
			[2]string{"Bug Tracker", repoURL + "/issues"},
			[2]string{"Changelog", repoURL + "/releases"},
		)
	}
	return pairs
}
