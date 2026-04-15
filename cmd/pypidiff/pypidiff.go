// Package pypidiff implements the "pypidiff" CLI command.
//
// It assembles the same metadata that "gowheels pypi" would write into a
// wheel, fetches the current metadata from PyPI's JSON API, and reports any
// field-level differences. Exit code 0 means no differences; exit code 1
// means at least one field differs.
package pypidiff

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/gowheels/cmd/root"
	"github.com/StevenACoffman/gowheels/internal/gomod"
	pypiclient "github.com/StevenACoffman/gowheels/internal/pypi"
	"github.com/StevenACoffman/gowheels/internal/repometa"
	"github.com/StevenACoffman/gowheels/internal/wheel"
)

// osClassifierPrefix is the trove classifier prefix for OS entries.
// These are excluded from diffMetadata comparisons because they depend on
// which binary platforms were uploaded, which pypidiff cannot know.
const osClassifierPrefix = "Operating System ::"

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

// localMetadata holds the metadata that "gowheels pypi" would write.
type localMetadata struct {
	summary           string
	licenseExpr       string
	keywords          []string
	classifiers       []string
	projectURLs       map[string]string
	requiresPython    string
	readme            string
	readmeContentType string
}

type fieldDiff struct {
	field  string
	local  string
	remote string
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
		LongHelp: `Compare the metadata that "gowheels pypi" would generate against
the current metadata published on PyPI and print any differences.

Metadata is assembled the same way "gowheels pypi" does: go.mod provides the
repository URL, GitHub auto-populates summary, license, and keywords (topics)
when --repo is set, and explicit flags override those defaults.

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
	ghMeta := cfg.fetchGitHubMeta(ctx, logger)
	cfg.applyGitHubMeta(ctx, ghMeta, logger)

	pypiName := cfg.Name
	if cfg.PackageName != "" {
		pypiName = cfg.PackageName
	}
	normName := wheel.NormalizeName(pypiName)

	logger.DebugContext(ctx, "fetching PyPI package info", "name", normName, "version", cfg.Version)
	remote, err := pypiclient.FetchPackageInfo(ctx, normName, cfg.Version)
	if err != nil {
		return fmt.Errorf("fetching PyPI data: %w", err)
	}

	if remote.Yanked {
		reason := remote.YankedReason
		if reason == "" {
			reason = "no reason given"
		}
		fmt.Fprintf(cfg.Stderr, "pypidiff: warning: %s v%s was yanked (%s)\n",
			normName, remote.Version, reason)
	}

	local := cfg.buildLocalMetadata(remote, ghMeta)

	diffs := diffMetadata(local, remote)
	if len(diffs) == 0 {
		fmt.Fprintf(cfg.Stdout, "pypidiff: %s v%s – no differences\n", normName, remote.Version)
		return nil
	}

	cfg.printDiffs(normName, remote.Version, diffs)
	return root.ExitError(1)
}

// buildLocalMetadata assembles the metadata that "gowheels pypi" would write
// into the wheel for the given PyPI release and GitHub repository state.
func (cfg *Config) buildLocalMetadata(
	remote *pypiclient.PackageInfo,
	ghMeta *repometa.Repo,
) *localMetadata {
	var localKeywords []string
	if ghMeta != nil {
		localKeywords = ghMeta.Topics
	}
	localReadme, localReadmeCT := resolveLocalReadme(cfg.ReadmePath, cfg.NoReadme)
	return &localMetadata{
		summary:           cfg.Summary,
		licenseExpr:       cfg.LicenseExpr,
		keywords:          localKeywords,
		classifiers:       buildClassifiers(remote.Version),
		projectURLs:       buildProjectURLs(cfg.URL, ghMeta),
		requiresPython:    ">=3.9",
		readme:            localReadme,
		readmeContentType: localReadmeCT,
	}
}

// printDiffs writes the diff report to cfg.Stdout.
func (cfg *Config) printDiffs(normName, version string, diffs []fieldDiff) {
	fmt.Fprintf(cfg.Stdout, "pypidiff: %s v%s – %d field(s) differ\n\n",
		normName, version, len(diffs))
	for _, d := range diffs {
		fmt.Fprintf(cfg.Stdout, "  %s:\n", d.field)
		printDiffValue(cfg.Stdout, "local", d.local)
		printDiffValue(cfg.Stdout, "remote", d.remote)
		fmt.Fprintln(cfg.Stdout)
	}
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
			ctx, "auto-populated from GitHub", "field", "license-expr", "value", cfg.LicenseExpr,
		)
	}
	if cfg.URL == "" && meta.HTMLURL != "" {
		cfg.URL = meta.HTMLURL
		logger.InfoContext(ctx, "auto-populated from GitHub", "field", "url", "value", cfg.URL)
	}
}

func diffMetadata(local *localMetadata, remote *pypiclient.PackageInfo) []fieldDiff {
	var diffs []fieldDiff

	if local.summary != remote.Summary {
		diffs = append(diffs, fieldDiff{"Summary", local.summary, remote.Summary})
	}
	if local.licenseExpr != remote.LicenseExpression {
		diffs = append(
			diffs,
			fieldDiff{"License-Expression", local.licenseExpr, remote.LicenseExpression},
		)
	}

	// Keywords: gowheels joins GitHub topics with spaces; PyPI stores them as-is.
	localKW := strings.Join(local.keywords, " ")
	if !equalKeywords(localKW, remote.Keywords) {
		diffs = append(diffs, fieldDiff{"Keywords", localKW, remote.Keywords})
	}

	if local.requiresPython != remote.RequiresPython {
		diffs = append(
			diffs,
			fieldDiff{"Requires-Python", local.requiresPython, remote.RequiresPython},
		)
	}

	// Classifiers: compare platform-independent entries only. OS classifiers
	// (e.g. "Operating System :: POSIX :: Linux") depend on which binary
	// platforms were uploaded and cannot be predicted without binaries.
	remoteClassifiers := filterOSClassifiers(remote.Classifiers)
	if !slices.Equal(local.classifiers, remoteClassifiers) {
		diffs = append(diffs, fieldDiff{
			"Classifiers",
			formatClassifiers(local.classifiers),
			formatClassifiers(remoteClassifiers),
		})
	}

	if !equalURLMaps(local.projectURLs, remote.ProjectURLs) {
		diffs = append(diffs, fieldDiff{
			"Project-URLs",
			formatURLLines(local.projectURLs),
			formatURLLines(remote.ProjectURLs),
		})
	}

	// Description (long description / README): compare presence, content-type,
	// and byte-count rather than raw content — README bodies can be thousands
	// of lines and a full text diff would be unreadable here. Use pypidiff
	// only to detect whether the README is missing or the wrong type; use a
	// dedicated diff tool for line-by-line content comparison.
	localDesc := formatDescriptionSummary(local.readme, local.readmeContentType)
	remoteDesc := formatDescriptionSummary(remote.Description, remote.DescriptionContentType)
	if localDesc != remoteDesc {
		diffs = append(diffs, fieldDiff{"Description", localDesc, remoteDesc})
	}

	return diffs
}

// equalKeywords compares two keyword strings after normalising whitespace.
func equalKeywords(a, b string) bool {
	return strings.Join(strings.Fields(a), " ") == strings.Join(strings.Fields(b), " ")
}

// equalURLMaps reports whether two Project-URL maps have the same entries,
// using case-insensitive key comparison.
func equalURLMaps(a, b map[string]string) bool {
	norm := func(m map[string]string) map[string]string {
		n := make(map[string]string, len(m))
		for k, v := range m {
			n[strings.ToLower(k)] = v
		}
		return n
	}
	na, nb := norm(a), norm(b)
	if len(na) != len(nb) {
		return false
	}
	for k, va := range na {
		if vb, ok := nb[k]; !ok || va != vb {
			return false
		}
	}
	return true
}

// formatURLLines formats a Project-URL map as a sorted newline-separated
// "Label: URL" string. Callers are responsible for indenting continuation
// lines in the output.
func formatURLLines(urls map[string]string) string {
	if len(urls) == 0 {
		return "(none)"
	}
	keys := make([]string, 0, len(urls))
	for k := range urls {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+": "+urls[k])
	}
	return strings.Join(lines, "\n")
}

// printDiffValue writes a labelled value to w. If value spans multiple lines,
// continuation lines are indented to align with the first value character.
// An empty value is displayed as "(empty)".
func printDiffValue(w io.Writer, label, value string) {
	prefix := fmt.Sprintf("    %-7s ", label+":")
	if value == "" {
		fmt.Fprintf(w, "%s(empty)\n", prefix)
		return
	}
	lines := strings.Split(value, "\n")
	fmt.Fprintf(w, "%s%s\n", prefix, lines[0])
	cont := strings.Repeat(" ", len(prefix))
	for _, line := range lines[1:] {
		fmt.Fprintf(w, "%s%s\n", cont, line)
	}
}

// buildClassifiers returns the platform-independent trove classifiers that
// "gowheels pypi" always emits for any upload. version is the published PyPI
// version string, used to derive the Development Status classifier. OS-specific
// classifiers are omitted because they depend on the binary platforms chosen at
// upload time.
func buildClassifiers(version string) []string {
	return []string{
		wheel.DevelopmentStatus(version),
		"Environment :: Console",
		"Programming Language :: Python :: 3",
	}
}

// filterOSClassifiers returns a sorted copy of c with all "Operating System ::"
// entries removed, so that the remote classifier list is comparable to the
// local platform-independent set.
func filterOSClassifiers(c []string) []string {
	out := make([]string, 0, len(c))
	for _, cl := range c {
		if !strings.HasPrefix(cl, osClassifierPrefix) {
			out = append(out, cl)
		}
	}
	slices.Sort(out)
	return out
}

// formatClassifiers formats a sorted classifier slice as a newline-separated
// string for use in diff output.
func formatClassifiers(classifiers []string) string {
	if len(classifiers) == 0 {
		return "(none)"
	}
	return strings.Join(classifiers, "\n")
}

// resolveLocalReadme reads the README that "gowheels pypi" would embed as the
// long description. It mirrors the resolveReadme / detectContentType logic in
// internal/wheel so that pypidiff sees exactly the same content the wheel would
// contain. Returns ("", "") when no README is present or --no-readme is set.
func resolveLocalReadme(path string, noReadme bool) (content, contentType string) {
	if noReadme {
		return "", ""
	}
	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", ""
		}
		return string(data), detectReadmeContentType(path)
	}
	for _, name := range []string{"README.md", "README.rst", "README.txt", "README"} {
		data, err := os.ReadFile(name)
		if err == nil {
			return string(data), detectReadmeContentType(name)
		}
	}
	return "", ""
}

// detectReadmeContentType mirrors wheel.detectContentType.
func detectReadmeContentType(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".md"), strings.HasSuffix(lower, ".markdown"):
		return "text/markdown"
	case strings.HasSuffix(lower, ".rst"):
		return "text/x-rst"
	default:
		return "text/plain"
	}
}

// formatDescriptionSummary returns a human-readable summary of a long
// description suitable for diff output. Comparing full README bodies would
// produce unreadable diffs, so we summarise as "<content-type> (<N> bytes)"
// or "(none)" when absent. A mismatch in presence, content-type, or byte-count
// signals a meaningful difference worth investigating with a dedicated diff tool.
func formatDescriptionSummary(content, contentType string) string {
	if strings.TrimSpace(content) == "" {
		return "(none)"
	}
	ct := contentType
	if ct == "" {
		ct = "text/plain"
	}
	return fmt.Sprintf("%s (%d bytes)", ct, len(content))
}

// buildProjectURLs constructs the Project-URL map the same way pypi command
// does, with "Repository" as the primary entry plus extra URLs from GitHub.
func buildProjectURLs(repoURL string, meta *repometa.Repo) map[string]string {
	urls := make(map[string]string)
	if repoURL != "" {
		urls["Repository"] = repoURL
	}
	if meta != nil && meta.Homepage != "" {
		urls["Homepage"] = meta.Homepage
	}
	if repoURL != "" && isGitHostingURL(repoURL) {
		urls["Bug Tracker"] = repoURL + "/issues"
		urls["Changelog"] = repoURL + "/releases"
	}
	return urls
}

// isGitHostingURL reports whether rawURL belongs to a known Git-hosting
// domain where /issues and /releases are standard paths.
func isGitHostingURL(rawURL string) bool {
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
