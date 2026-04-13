// Package pypi implements the "pypi" subcommand.
package pypi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/gowheels/cmd/root"
	"github.com/StevenACoffman/gowheels/internal/platforms"
	pypiclient "github.com/StevenACoffman/gowheels/internal/pypi"
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
	cfg.Flags.StringVar(&cfg.PackageName, 0, "package-name", "", "Python package name on PyPI (default: --name)")
	cfg.Flags.StringVar(&cfg.EntryPoint, 0, "entry-point", "", "console_scripts entry point (default: --name)")

	// Metadata
	cfg.Flags.StringVar(&cfg.Summary, 0, "summary", "", "one-line package description")
	cfg.Flags.StringVar(&cfg.LicenseExpr, 0, "license-expr", "MIT", "SPDX license expression (Metadata-Version 2.4)")
	cfg.Flags.StringVar(&cfg.LicensePath, 0, "license", "", "path to local license file; bundled in dist-info/licenses/")
	cfg.Flags.StringVar(&cfg.ReadmePath, 0, "readme", "", "path to README (default: auto-detect; use - to disable)")
	cfg.Flags.BoolVar(&cfg.NoReadme, 0, "no-readme", "disable README auto-detection")
	cfg.Flags.StringVar(&cfg.URL, 0, "url", "", "project repository URL embedded as Project-URL")

	// Version
	cfg.Flags.StringVar(&cfg.Version, 0, "version", "", "version (semver or PEP 440; default: git describe --tags --exact-match)")
	cfg.Flags.StringVar(&cfg.PyVersion, 0, "py-version", "", "override Python package version independently of binary version")

	// Platform filter
	cfg.Flags.StringVar(&cfg.PlatformFilter, 0, "platforms", "", "comma-separated os/arch filter (default: all supported platforms)")

	// Output / upload
	cfg.Flags.StringVar(&cfg.OutputDir, 0, "output", "dist", "output directory for .whl files")
	cfg.Flags.BoolVar(&cfg.Upload, 0, "upload", "upload wheels to PyPI after building")
	cfg.Flags.StringVar(&cfg.PyPIURL, 0, "pypi-url", "", "PyPI upload endpoint (default: https://upload.pypi.org/legacy/)")
	cfg.Flags.StringVar(&cfg.PyPIToken, 0, "pypi-token", "", "PyPI API token; when absent, GitHub Actions OIDC is used")

	// Mode
	cfg.Flags.StringVar(&cfg.Mode, 0, "mode", "", "binary source: release, local, or build (inferred from other flags when omitted)")

	// Release mode flags
	cfg.Flags.StringVar(&cfg.Repo, 0, "repo", "", "GitHub repo in owner/name format (release mode)")
	cfg.Flags.StringVar(&cfg.Assets, 0, "assets", "", "comma-separated asset name overrides (release mode)")
	cfg.Flags.StringVar(&cfg.Cache, 0, "cache", "", "binary cache directory (release mode; default: $XDG_CACHE_HOME/gowheels)")
	cfg.Flags.StringVar(&cfg.GitHubToken, 0, "github-token", "", "GitHub personal access token (release mode; avoids API rate limits)")

	// Local mode flags
	cfg.Flags.StringListVar(&cfg.Artifacts, 0, "artifact", "artifact mapping os/arch:path, repeatable (local mode)")

	// Build mode flags
	cfg.Flags.StringVar(&cfg.Package, 0, "package", "", "Go package path to build (build mode; default: .)")
	cfg.Flags.StringVar(&cfg.ModDir, 0, "mod-dir", "", "directory containing go.mod (build mode; default: .)")
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

	// Setup structured logging.
	level := slog.LevelInfo
	if cfg.Debug {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(cfg.Stderr, &slog.HandlerOptions{Level: level}))

	// Resolve binary version.
	ver, err := internver.Resolve(cfg.Version)
	if err != nil {
		return err
	}

	// Python package version may differ (e.g. custom pre-release labelling).
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

	// Determine binary source mode.
	mode, err := cfg.inferMode()
	if err != nil {
		return err
	}
	logger.Debug("resolved", "mode", mode, "version", ver, "py_version", pyVer, "platforms", len(plats))

	// Instantiate source.
	var src source.Source
	switch mode {
	case "release":
		src = source.NewReleaseSource(cfg.Repo, ver, cfg.Assets, cfg.Cache, cfg.GitHubToken, cfg.Stdout)
	case "local":
		src, err = source.NewLocalSource(cfg.Artifacts)
		if err != nil {
			return err
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
	logger.Debug("resolved binaries", "count", len(binaries))

	// Compute readme path (--no-readme → pass "-" to disable auto-detect).
	readmePath := cfg.ReadmePath
	if cfg.NoReadme {
		readmePath = "-"
	}

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
	}

	fmt.Fprintf(cfg.Stdout, "gowheels: building wheels...\n")
	wheels, err := wheel.BuildAll(whlCfg, binaries)
	if err != nil {
		return fmt.Errorf("building wheels: %w", err)
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
		return err
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
		return "", errors.New("ambiguous mode: --repo (release) and --artifact (local) both set; use --mode to disambiguate")
	case hasRepo && hasBuild:
		return "", errors.New("ambiguous mode: --repo (release) and --package/--mod-dir (build) both set; use --mode to disambiguate")
	case hasArtifacts && hasBuild:
		return "", errors.New("ambiguous mode: --artifact (local) and --package/--mod-dir (build) both set; use --mode to disambiguate")
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

