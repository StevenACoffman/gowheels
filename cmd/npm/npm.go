// Package npm implements the "npm" subcommand.
package npm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/peterbourgon/ff/v4"

	"github.com/StevenACoffman/gowheels/cmd/root"
	"github.com/StevenACoffman/gowheels/internal/npm"
	"github.com/StevenACoffman/gowheels/internal/platforms"
	internver "github.com/StevenACoffman/gowheels/internal/version"
)

// Config holds the configuration for the npm subcommand.
type Config struct {
	*root.Config

	Name       string
	Org        string
	Artifacts  []string
	Version    string
	Summary    string
	License    string
	Tag        string
	Provenance bool
	Repository string
	ReadmePath string
	NoReadme   bool

	Flags   *ff.FlagSet
	Command *ff.Command
}

// New creates and registers the npm subcommand with the given parent config.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("npm").SetParent(parent.Flags)

	cfg.Flags.StringVar(&cfg.Name, 0, "name", "", "binary name (required)")
	cfg.Flags.StringVar(&cfg.Org, 0, "org", "", "npm org scope, e.g. myorg → @myorg/name-linux-x64 (required)")
	cfg.Flags.StringListVar(&cfg.Artifacts, 0, "artifact", "artifact mapping os/arch:path, repeatable (required)")
	cfg.Flags.StringVar(&cfg.Version, 0, "version", "", "package version (semver; default: git describe --tags --exact-match)")
	cfg.Flags.StringVar(&cfg.Summary, 0, "summary", "", "one-line description")
	cfg.Flags.StringVar(&cfg.License, 0, "license", "", "license identifier (e.g. MIT)")
	cfg.Flags.StringVar(&cfg.Tag, 0, "tag", "latest", "dist-tag to publish under")
	cfg.Flags.BoolVar(&cfg.Provenance, 0, "provenance", "publish with npm provenance attestation (requires CI)")
	cfg.Flags.StringVar(&cfg.Repository, 0, "repository", "", "repository URL for package.json")
	cfg.Flags.StringVar(&cfg.ReadmePath, 0, "readme", "", "path to README file (default: auto-detect)")
	cfg.Flags.BoolVar(&cfg.NoReadme, 0, "no-readme", "disable README auto-detection")

	cfg.Command = &ff.Command{
		Name:      "npm",
		Usage:     "gowheels npm --name <name> --org <org> --artifact os/arch:path ... [flags]",
		ShortHelp: "publish Go binaries to npm as platform-specific packages",
		LongHelp: `Build and publish platform-specific npm packages wrapping Go binaries.

Each --artifact produces a scoped platform package (@org/name-linux-x64, etc.).
After all platform packages are published, a root coordinator package (name) is
published that lists them as optional dependencies.

npm must be installed and authenticated (NODE_AUTH_TOKEN or npm login).

Example:
  gowheels npm --name mytool --org myorg \
    --artifact linux/amd64:dist/mytool_linux_amd64/mytool \
    --artifact darwin/arm64:dist/mytool_darwin_arm64/mytool \
    --artifact windows/amd64:dist/mytool_windows_amd64/mytool.exe`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

// npmPlatformMap maps goos/goarch to npm OS/CPU/suffix strings.
var npmPlatformMap = map[string][3]string{
	"linux/amd64":   {"linux", "x64", "linux-x64"},
	"linux/arm64":   {"linux", "arm64", "linux-arm64"},
	"darwin/amd64":  {"darwin", "x64", "darwin-x64"},
	"darwin/arm64":  {"darwin", "arm64", "darwin-arm64"},
	"windows/amd64": {"win32", "x64", "win32-x64"},
	"windows/arm64": {"win32", "arm64", "win32-arm64"},
}

func (cfg *Config) exec(ctx context.Context, _ []string) error {
	if cfg.Name == "" {
		return errors.New("--name is required")
	}
	if cfg.Org == "" {
		return errors.New("--org is required")
	}
	if len(cfg.Artifacts) == 0 {
		return errors.New("at least one --artifact is required")
	}

	// Resolve version.
	ver, err := internver.Resolve(cfg.Version)
	if err != nil {
		return err
	}

	// Parse artifacts into npm.Artifact values.
	var artifacts []npm.Artifact
	for _, entry := range cfg.Artifacts {
		platformStr, path, ok := strings.Cut(entry, ":")
		if !ok {
			return fmt.Errorf("invalid --artifact %q: expected os/arch:path", entry)
		}
		goos, goarch, ok := strings.Cut(platformStr, "/")
		if !ok {
			return fmt.Errorf("invalid --artifact %q: platform must be os/arch", entry)
		}
		if _, err := platforms.Lookup(goos, goarch); err != nil {
			return fmt.Errorf("--artifact %q: %w", entry, err)
		}
		npm3, ok := npmPlatformMap[platformStr]
		if !ok {
			return fmt.Errorf("--artifact %q: no npm mapping for %s", entry, platformStr)
		}
		artifacts = append(artifacts, npm.Artifact{
			GOOS:          goos,
			GOARCH:        goarch,
			Path:          path,
			NpmOS:         npm3[0],
			NpmCPU:        npm3[1],
			PackageSuffix: npm3[2],
		})
	}

	readmePath := cfg.ReadmePath
	if cfg.NoReadme {
		readmePath = ""
	}

	npmCfg := &npm.Config{
		Name:       cfg.Name,
		Version:    ver,
		Summary:    cfg.Summary,
		License:    cfg.License,
		Artifacts:  artifacts,
		DryRun:     cfg.DryRun,
		Org:        cfg.Org,
		Tag:        cfg.Tag,
		Provenance: cfg.Provenance,
		ReadmePath: readmePath,
		Repository: cfg.Repository,
		Stdout:     cfg.Stdout,
	}

	return npm.Publish(ctx, npmCfg)
}

