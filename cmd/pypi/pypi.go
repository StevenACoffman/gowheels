// Package pypi implements the "pypi" subcommand.
package pypi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/gowheels/cmd/root"
	"github.com/StevenACoffman/gowheels/internal/gomod"
	"github.com/StevenACoffman/gowheels/internal/platforms"
	pypiclient "github.com/StevenACoffman/gowheels/internal/pypi"
	"github.com/StevenACoffman/gowheels/internal/repometa"
	"github.com/StevenACoffman/gowheels/internal/source"
	internver "github.com/StevenACoffman/gowheels/internal/version"
	"github.com/StevenACoffman/gowheels/internal/wheel"
)

// Config holds the configuration for the pypi subcommand.
type Config struct {
	*root.Config

	// Package identity
	Name        string
	PackageName string
	EntryPoint  string

	// Metadata
	Summary     string
	LicenseExpr string
	LicensePath string
	ReadmePath  string
	NoReadme    bool
	URL         string

	// Version
	Version   string
	PyVersion string

	// Platform selection
	PlatformFilter string

	// Output / upload
	OutputDir string
	Upload    bool
	PyPIURL   string
	PyPIToken string

	// Mode: "release", "local", or "build"; inferred from flags when empty.
	Mode string

	// Release mode (--mode release or --repo provided)
	Repo        string
	Assets      string
	Cache       string
	GitHubToken string

	// Local mode (--mode local or --artifact provided)
	Artifacts []string

	// Build mode (--mode build or --package / --mod-dir provided)
	Package string
	ModDir  string
	LDFlags string

	Flags   *ff.FlagSet
	Command *ff.Command
}

// New creates and registers the pypi subcommand with the given parent config.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("pypi").SetParent(parent.Flags)

	// Identity
	cfg.Flags.StringVar(&cfg.Name, 0, "name", "", "binary name (required)")
	cfg.Flags.StringVar(
		&cfg.PackageName,
		0,
		"package-name",
		"",
		"Python package name on PyPI (default: --name)",
	)
	cfg.Flags.StringVar(
		&cfg.EntryPoint,
		0,
		"entry-point",
		"",
		"console_scripts entry point (default: --name)",
	)

	// Metadata
	cfg.Flags.StringVar(&cfg.Summary, 0, "summary", "", "one-line package description")
	cfg.Flags.StringVar(
		&cfg.LicenseExpr,
		0,
		"license-expr",
		"",
		"SPDX license expression (Metadata-Version 2.4); auto-detected from GitHub when --repo is set",
	)
	cfg.Flags.StringVar(
		&cfg.LicensePath,
		0,
		"license",
		"",
		"path to local license file; bundled in dist-info/licenses/",
	)
	cfg.Flags.StringVar(
		&cfg.ReadmePath,
		0,
		"readme",
		"",
		"path to README (default: auto-detect; use - to disable)",
	)
	cfg.Flags.BoolVar(&cfg.NoReadme, 0, "no-readme", "disable README auto-detection")
	cfg.Flags.StringVar(
		&cfg.URL,
		0,
		"url",
		"",
		"project URL (auto-detected from go.mod and GitHub when --repo is set)",
	)

	// Version
	cfg.Flags.StringVar(
		&cfg.Version,
		0,
		"version",
		"",
		"version (semver or PEP 440; default: git describe --tags --exact-match)",
	)
	cfg.Flags.StringVar(
		&cfg.PyVersion,
		0,
		"py-version",
		"",
		"override Python package version independently of binary version",
	)

	// Platform filter
	cfg.Flags.StringVar(
		&cfg.PlatformFilter,
		0,
		"platforms",
		"",
		"comma-separated os/arch filter (default: all supported platforms)",
	)

	// Output / upload
	cfg.Flags.StringVar(&cfg.OutputDir, 0, "output", "dist", "output directory for .whl files")
	cfg.Flags.BoolVar(&cfg.Upload, 0, "upload", "upload wheels to PyPI after building")
	cfg.Flags.StringVar(
		&cfg.PyPIURL,
		0,
		"pypi-url",
		"",
		"PyPI upload endpoint (default: https://upload.pypi.org/legacy/)",
	)
	cfg.Flags.StringVar(
		&cfg.PyPIToken,
		0,
		"pypi-token",
		"",
		"PyPI API token; when absent, GitHub Actions OIDC is used",
	)

	// Mode
	cfg.Flags.StringVar(
		&cfg.Mode,
		0,
		"mode",
		"",
		"binary source: release, local, or build (inferred from other flags when omitted)",
	)

	// Release mode flags
	cfg.Flags.StringVar(&cfg.Repo, 0, "repo", "", "GitHub repo in owner/name format (release mode)")
	cfg.Flags.StringVar(
		&cfg.Assets,
		0,
		"assets",
		"",
		"comma-separated asset name overrides (release mode)",
	)
	cfg.Flags.StringVar(
		&cfg.Cache,
		0,
		"cache",
		"",
		"binary cache directory (release mode; default: $XDG_CACHE_HOME/gowheels)",
	)
	cfg.Flags.StringVar(
		&cfg.GitHubToken,
		0,
		"github-token",
		"",
		"GitHub personal access token (release mode; avoids API rate limits)",
	)

	// Local mode flags
	cfg.Flags.StringListVar(
		&cfg.Artifacts,
		0,
		"artifact",
		"artifact mapping os/arch:path, repeatable (local mode)",
	)

	// Build mode flags
	cfg.Flags.StringVar(
		&cfg.Package,
		0,
		"package",
		"",
		"Go package path to build (build mode; default: .)",
	)
	cfg.Flags.StringVar(
		&cfg.ModDir,
		0,
		"mod-dir",
		"",
		"directory containing go.mod (build mode; default: .)",
	)
	cfg.Flags.StringVar(&cfg.LDFlags, 0, "ldflags", "", "Go linker flags (build mode; default: -s)")

	cfg.Command = &ff.Command{
		Name:      "pypi",
		Usage:     "gowheels pypi --name <name> [--mode release|local|build] [flags]",
		ShortHelp: "build Python wheels and optionally publish to PyPI",
		LongHelp: `Build platform-specific Python wheels wrapping a Go binary and optionally upload to PyPI.

Binary source (--mode or inferred):
  release   download binaries from a GitHub Release (inferred when --repo is set)
  local     use pre-built binary files (inferred when --artifact is set)
  build     compile from Go source (inferred when --package or --mod-dir is set)

Authentication (--upload):
  GOWHEELS_PYPI_TOKEN env var takes precedence. When absent, GitHub Actions OIDC is used
  (requires id-token: write permission and a configured trusted publisher).

Examples:
  # From a GoReleaser-produced GitHub release:
  gowheels pypi --name mytool --repo owner/mytool --upload

  # From pre-built local binaries:
  gowheels pypi --name mytool \
    --artifact linux/amd64:dist/mytool_linux_amd64/mytool \
    --artifact darwin/arm64:dist/mytool_darwin_arm64/mytool \
    --upload

  # Compile from source:
  gowheels pypi --name mytool --mode build --upload`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

func (cfg *Config) exec(ctx context.Context, _ []string) error {
	if cfg.Name == "" {
		return errors.New("--name is required")
	}

	// Read GitHub token from environment when not supplied via flag.
	// GOWHEELS_GITHUB_TOKEN takes precedence; GITHUB_TOKEN is the standard
	// Actions token and is a useful fallback for avoiding rate limits.
	if cfg.GitHubToken == "" {
		for _, envVar := range []string{"GOWHEELS_GITHUB_TOKEN", "GITHUB_TOKEN"} {
			if v := os.Getenv(envVar); v != "" {
				cfg.GitHubToken = v
				break
			}
		}
	}

	// Resolve the GitHub repository for metadata purposes (summary, license,
	// topics, URLs). This is kept separate from cfg.Repo — which is only
	// set by --repo — so that the env-var fallback does not trigger release
	// mode when the user is running in local or build mode inside Actions.
	githubRepo := cfg.Repo
	if githubRepo == "" {
		githubRepo = os.Getenv("GITHUB_REPOSITORY")
	}

	// Setup structured logging.
	level := slog.LevelInfo
	if cfg.Debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(cfg.Stderr, &slog.HandlerOptions{Level: level}))

	// Resolve binary version.
	ver, err := internver.Resolve(ctx, cfg.Version)
	if err != nil {
		return fmt.Errorf("resolving version: %w", err)
	}

	// Python package version may differ (e.g. custom pre-release labeling).
	pyVer := ver
	if cfg.PyVersion != "" {
		pyVer, err = internver.Normalize(cfg.PyVersion)
		if err != nil {
			return fmt.Errorf("--py-version: %w", err)
		}
	}

	// Resolve target platforms.
	plats, err := platforms.Filter(cfg.PlatformFilter)
	if err != nil {
		return fmt.Errorf("--platforms: %w", err)
	}

	// Determine binary source mode using only cfg.Repo (user-specified).
	mode, err := cfg.inferMode()
	if err != nil {
		return err
	}
	logger.DebugContext(ctx, "resolved", "mode", mode, "version", ver, "py_version", pyVer,
		"platforms", len(plats))

	// URL from go.mod: zero network calls, works in all modes.
	if cfg.URL == "" {
		modDir := cfg.ModDir
		if modDir == "" {
			modDir = "."
		}
		cfg.URL = gomod.RepoURL(modDir)
	}

	// Fetch GitHub repository metadata. Always fetched when a repo is known
	// because topics (keywords) and Homepage are useful even when the other
	// fields are explicitly set. Non-fatal on failure.
	if githubRepo == "" {
		logger.WarnContext(
			ctx,
			"no GitHub repository configured; summary, license, keywords, and extra URLs will not be auto-populated (use --repo or set GITHUB_REPOSITORY)",
		)
	}
	ghMeta := cfg.fetchGitHubMetaFor(ctx, logger, githubRepo)
	cfg.applyGitHubMeta(ctx, ghMeta, logger)

	// Warn if summary is still empty after all resolution attempts.
	if cfg.Summary == "" {
		logger.WarnContext(
			ctx,
			"summary will be empty in the wheel (use --summary or configure --repo with a GitHub description)",
		)
	}

	// Build extra Project-URL entries and keywords from GitHub metadata.
	extraURLs := cfg.buildExtraURLs(ghMeta)
	var keywords []string
	if ghMeta != nil {
		keywords = ghMeta.Topics
	}

	// Instantiate source.
	var src source.Source
	switch mode {
	case "release":
		src = source.NewReleaseSource(
			cfg.Repo, ver, cfg.Assets, cfg.Cache, cfg.GitHubToken, cfg.Stdout,
		)
	case "local":
		src, err = source.NewLocalSource(cfg.Artifacts)
		if err != nil {
			return fmt.Errorf("creating local source: %w", err)
		}
	case "build":
		src = source.NewBuildSource(cfg.Package, cfg.ModDir, cfg.LDFlags, cfg.Stdout, cfg.Stderr)
	}

	if cfg.DryRun {
		return cfg.dryRun(plats, pyVer)
	}

	// Resolve binaries.
	fmt.Fprintf(cfg.Stdout, "gowheels: resolving binaries (%s mode, version %s)...\n", mode, ver)
	binaries, err := src.Resolve(ctx, cfg.Name, plats)
	if err != nil {
		return fmt.Errorf("resolving binaries: %w", err)
	}
	logger.DebugContext(ctx, "resolved binaries", "count", len(binaries))

	// Compute readme path (--no-readme → pass "-" to disable auto-detect) and
	// determine the human-readable status for logging. Both the per-field
	// warning and the pre-build metadata summary use the same status string.
	readmePath := cfg.ReadmePath
	readmeStatus := "not included"
	switch {
	case cfg.NoReadme:
		readmePath = "-"
		readmeStatus = "disabled (--no-readme)"
		logger.DebugContext(
			ctx,
			"README disabled by --no-readme; wheel will have no long description",
		)
	case cfg.ReadmePath != "":
		if _, statErr := os.Stat(cfg.ReadmePath); statErr != nil {
			logger.WarnContext(ctx, "README file not found; wheel will have no long description",
				"path", cfg.ReadmePath)
		} else {
			readmeStatus = cfg.ReadmePath
			logger.DebugContext(ctx, "README resolved", "path", cfg.ReadmePath)
		}
	default:
		// Auto-detect: check once, log result, reuse for the metadata summary.
		for _, name := range []string{"README.md", "README.rst", "README.txt", "README"} {
			if _, statErr := os.Stat(name); statErr == nil {
				readmeStatus = name
				break
			}
		}
		if readmeStatus == "not included" {
			logger.WarnContext(
				ctx,
				"no README file found in current directory; wheel will have no long description",
				"tried",
				[]string{"README.md", "README.rst", "README.txt", "README"},
			)
		} else {
			logger.DebugContext(ctx, "README auto-detected", "path", readmeStatus)
		}
	}

	// Log the metadata that will be written into every wheel. This is the last
	// chance to catch empty fields before upload — PyPI does not allow metadata
	// updates after a version is published (re-uploading the same version with
	// corrected metadata will fail with HTTP 409; the version must be deleted
	// and re-uploaded, or a new version released).
	logger.InfoContext(ctx, "wheel metadata",
		"summary", cfg.Summary,
		"license_expr", cfg.LicenseExpr,
		"readme", readmeStatus,
		"keywords", len(keywords),
		"project_url", cfg.URL,
	)

	// Build wheels.
	whlCfg := wheel.Config{
		RawName:     cfg.Name,
		PackageName: cfg.PackageName,
		EntryPoint:  cfg.EntryPoint,
		Version:     pyVer,
		Summary:     cfg.Summary,
		URL:         cfg.URL,
		LicenseExpr: cfg.LicenseExpr,
		LicensePath: cfg.LicensePath,
		ReadmePath:  readmePath,
		OutputDir:   cfg.OutputDir,
		Keywords:    keywords,
		ExtraURLs:   extraURLs,
	}

	// Before committing to the build and upload, show what the metadata looks
	// like versus what is currently on PyPI. This lets the operator verify the
	// correction before it is published. The check is non-fatal: a network
	// failure or a brand-new package that is not yet on PyPI simply skips the
	// diff and proceeds.
	if cfg.Upload {
		cfg.runPreUploadDiff(ctx, logger, pyVer, whlCfg)
	}

	fmt.Fprintf(cfg.Stdout, "gowheels: building wheels...\n")
	wheels, err := wheel.BuildAll(whlCfg, binaries)
	if err != nil {
		return fmt.Errorf("building wheels: %w", err)
	}
	if len(wheels) == 0 {
		logger.WarnContext(
			ctx,
			"no wheels were built; the binary source returned no matching binaries for the target platforms",
		)
		return nil
	}
	for _, w := range wheels {
		fmt.Fprintf(cfg.Stdout, "  built %s\n", w.Filename)
	}

	if !cfg.Upload {
		return nil
	}

	// Authenticate and upload.
	fmt.Fprintf(cfg.Stdout, "gowheels: authenticating with PyPI...\n")
	token, err := pypiclient.MintToken(ctx, cfg.PyPIToken)
	if err != nil {
		return fmt.Errorf("authenticating with PyPI: %w", err)
	}

	fmt.Fprintf(cfg.Stdout, "gowheels: uploading %d wheel(s)...\n", len(wheels))
	for _, w := range wheels {
		fmt.Fprintf(cfg.Stdout, "  uploading %s...\n", w.Filename)
		if err := pypiclient.Upload(ctx, w, token, cfg.PyPIURL); err != nil {
			return fmt.Errorf("uploading %s: %w", w.Filename, err)
		}
	}

	fmt.Fprintf(cfg.Stdout, "gowheels: done\n")
	return nil
}

// fetchGitHubMetaFor fetches repository metadata from the GitHub API.
// It returns nil on failure — callers treat GitHub metadata as best-effort.
func (cfg *Config) fetchGitHubMetaFor(
	ctx context.Context,
	logger *slog.Logger,
	repo string,
) *repometa.Repo {
	if repo == "" {
		return nil
	}
	logger.DebugContext(ctx, "fetching GitHub repository metadata", "repo", repo)
	meta, err := repometa.Fetch(ctx, repo, cfg.GitHubToken)
	if err != nil {
		logger.WarnContext(
			ctx,
			"could not fetch GitHub metadata; summary, license, keywords, and extra URLs will not be auto-populated",
			"error",
			err,
		)
		return nil
	}
	return meta
}

// applyGitHubMeta fills any still-empty metadata fields from the GitHub
// repository response and logs each field that was auto-populated.
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
	} else if cfg.Summary == "" && meta.Description == "" {
		logger.WarnContext(
			ctx,
			"GitHub repository has no description; summary will be empty (set it on GitHub or use --summary)",
		)
	}
	if cfg.LicenseExpr == "" && meta.LicenseSPDX != "" {
		cfg.LicenseExpr = meta.LicenseSPDX
		logger.InfoContext(
			ctx, "auto-populated from GitHub", "field", "license-expr", "value", cfg.LicenseExpr,
		)
	} else if cfg.LicenseExpr == "" && meta.LicenseSPDX == "" {
		logger.WarnContext(
			ctx,
			"GitHub repository has no detectable SPDX license; license-expr will be empty (add a LICENSE file on GitHub or use --license-expr)",
		)
	}
	if cfg.URL == "" && meta.HTMLURL != "" {
		cfg.URL = meta.HTMLURL
		logger.InfoContext(ctx, "auto-populated from GitHub", "field", "url", "value", cfg.URL)
	}
}

// buildExtraURLs returns additional Project-URL entries derived from the
// primary URL and, when available, the GitHub repository's homepage field.
// /issues and /releases are only appended for known Git-hosting domains;
// arbitrary documentation URLs would produce broken sidebar links.
func (cfg *Config) buildExtraURLs(meta *repometa.Repo) [][2]string {
	var urls [][2]string
	if meta != nil && meta.Homepage != "" {
		urls = append(urls, [2]string{"Homepage", meta.Homepage})
	}
	if cfg.URL != "" && pypiclient.IsGitHostingURL(cfg.URL) {
		urls = append(urls,
			[2]string{"Bug Tracker", cfg.URL + "/issues"},
			[2]string{"Changelog", cfg.URL + "/releases"},
		)
	}
	return urls
}

// dryRun prints what would be built without doing any work.
func (cfg *Config) dryRun(plats []platforms.Platform, pyVer string) error {
	normName := wheel.NormalizeName(cfg.Name)
	if cfg.PackageName != "" {
		normName = wheel.NormalizeName(cfg.PackageName)
	}
	fmt.Fprintf(cfg.Stdout, "gowheels: [dry run] would build wheels into %s/\n", cfg.OutputDir)
	for _, p := range plats {
		for _, tag := range p.WheelTags {
			fmt.Fprintf(cfg.Stdout, "  %s-%s-py3-none-%s.whl\n", normName, pyVer, tag)
		}
	}
	return nil
}

// runPreUploadDiff fetches the latest release metadata from PyPI and prints a
// unified diff between that and the METADATA that would be embedded in the
// wheels about to be uploaded. The diff is purely informational — it shows
// what the upload will correct relative to the current published state.
//
// The call is non-fatal: if the package does not yet exist on PyPI (first
// release) or if the network is unavailable, a debug-level message is logged
// and the upload proceeds normally.
func (cfg *Config) runPreUploadDiff(
	ctx context.Context,
	logger *slog.Logger,
	pyVer string,
	whlCfg wheel.Config,
) {
	normName := wheel.NormalizeName(whlCfg.RawName)
	if whlCfg.PackageName != "" {
		normName = wheel.NormalizeName(whlCfg.PackageName)
	}

	remote, err := pypiclient.FetchPackageInfo(ctx, normName, "")
	if err != nil {
		logger.DebugContext(ctx,
			"pre-upload metadata diff skipped (package not yet on PyPI or network unavailable)",
			"error", err)
		return
	}

	localText := wheel.BuildMetadataText(whlCfg, pyVer, wheel.PlatformIndependentClassifiers(pyVer))
	logger.DebugContext(ctx, "local METADATA to be embedded in wheels", "text", localText)

	// Normalize the remote version to pyVer before diffing. The version field
	// will always differ between a new upload and the current published release,
	// which is expected and not a "correction". Normalizing it here means the
	// diff only surfaces meaningful metadata differences (summary, license,
	// keywords, URLs) — the fields the upload can actually improve.
	remoteForDiff := *remote
	remoteForDiff.Version = pyVer
	remoteText := pypiclient.RenderAsMetadata(&remoteForDiff)

	diff := pypiclient.UnifiedDiff(
		fmt.Sprintf("local (gowheels pypi v%s)", pyVer),
		fmt.Sprintf("remote (PyPI %s v%s → normalized to v%s)", normName, remote.Version, pyVer),
		pypiclient.TruncateReadmeBody(localText),
		pypiclient.TruncateReadmeBody(remoteText),
	)

	fmt.Fprintf(cfg.Stdout, "gowheels: metadata diff (local v%s vs PyPI %s v%s):\n",
		pyVer, normName, remote.Version)
	if diff == "" {
		fmt.Fprintf(cfg.Stdout, "  no differences — metadata matches the current PyPI release\n")
		return
	}
	fmt.Fprint(cfg.Stdout, diff)
	fmt.Fprintf(cfg.Stdout,
		"gowheels: uploading will correct the above metadata for %s\n", normName)
}

// inferMode determines the binary source mode from explicit --mode or from
// which mode-specific flags are present.
func (cfg *Config) inferMode() (string, error) {
	if cfg.Mode != "" {
		switch cfg.Mode {
		case "release", "local", "build":
			return cfg.Mode, nil
		default:
			return "", fmt.Errorf("--mode must be release, local, or build (got %q)", cfg.Mode)
		}
	}

	hasRepo := cfg.Repo != ""
	hasArtifacts := len(cfg.Artifacts) > 0
	hasBuild := cfg.Package != "" || cfg.ModDir != ""

	switch {
	case hasRepo && !hasArtifacts && !hasBuild:
		return "release", nil
	case hasArtifacts && !hasRepo && !hasBuild:
		return "local", nil
	case hasBuild && !hasRepo && !hasArtifacts:
		return "build", nil
	case hasRepo && hasArtifacts:
		return "", errors.New(
			"ambiguous mode: --repo (release) and --artifact (local) both set; use --mode to disambiguate",
		)
	case hasRepo && hasBuild:
		return "", errors.New(
			"ambiguous mode: --repo (release) and --package/--mod-dir (build) both set; use --mode to disambiguate",
		)
	case hasArtifacts && hasBuild:
		return "", errors.New(
			"ambiguous mode: --artifact (local) and --package/--mod-dir (build) both set; use --mode to disambiguate",
		)
	default:
		return "", errors.New(
			"cannot infer --mode; provide one of:\n" +
				"  --repo owner/name           (release mode: download from GitHub)\n" +
				"  --artifact os/arch:path     (local mode: use pre-built files)\n" +
				"  --package ./cmd/tool        (build mode: compile from source)\n" +
				"  --mode release|local|build  (explicit override)",
		)
	}
}
