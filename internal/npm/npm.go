// Package npm builds and publishes platform-specific npm packages wrapping Go
// binaries. It delegates publishing to the npm CLI via os/exec.
package npm

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	registryPollInterval = 5 * time.Second
	registryPollTimeout  = 2 * time.Minute
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

//go:embed wrapper.js
var wrapperJS string

// Artifact is a resolved binary file path for one os/arch pair.
type Artifact struct {
	GOOS          string
	GOARCH        string
	Path          string
	NpmOS         string // e.g. "linux"
	NpmCPU        string // e.g. "x64"
	PackageSuffix string // e.g. "linux-x64"
}

// Config parameterises an npm publish run.
type Config struct {
	Name       string
	Version    string
	Summary    string
	License    string
	Artifacts  []Artifact
	DryRun     bool
	Org        string
	Tag        string
	Provenance bool
	ReadmePath string
	Repository string
	Stdout     io.Writer
}

type builtPackage struct {
	dir  string
	name string
}

type packageJSON struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Description  string            `json:"description,omitempty"`
	License      string            `json:"license,omitempty"`
	Repository   string            `json:"repository,omitempty"`
	OS           []string          `json:"os,omitempty"`
	CPU          []string          `json:"cpu,omitempty"`
	Files        []string          `json:"files"`
	Bin          map[string]string `json:"bin,omitempty"`
	OptionalDeps map[string]string `json:"optionalDependencies,omitempty"`
}

// Publish builds platform npm packages and a root coordinator package, then
// publishes all of them via `npm publish`.
func Publish(ctx context.Context, cfg *Config) error {
	stdout := cfg.Stdout
	if stdout == nil {
		stdout = io.Discard
	}
	verb := "publishing"
	if cfg.DryRun {
		verb = "would publish"
	}
	fmt.Fprintf(stdout, "npm: %s version %s\n", verb, cfg.Version)

	platforms, cleanup, err := buildPlatformPackages(cfg)
	if err != nil {
		return err
	}
	defer cleanup()

	for _, pkg := range platforms {
		fmt.Fprintf(stdout, "npm: %s %s...\n", verb, pkg.name)
		if err := npmPublish(
			ctx,
			pkg.dir,
			cfg.Tag,
			cfg.Provenance,
			cfg.DryRun,
			stdout,
		); err != nil {
			return fmt.Errorf("npm: publishing %s: %w", pkg.name, err)
		}
	}

	if !cfg.DryRun {
		fmt.Fprintf(stdout, "npm: waiting for registry propagation...\n")
		if err := pollAllVisible(ctx, platforms, cfg.Version); err != nil {
			return err
		}
	}

	root, rootCleanup, err := buildRootPackage(cfg, platforms)
	if err != nil {
		return err
	}
	defer rootCleanup()

	fmt.Fprintf(stdout, "npm: %s %s...\n", verb, root.name)
	if err := npmPublish(ctx, root.dir, cfg.Tag, cfg.Provenance, cfg.DryRun, stdout); err != nil {
		return fmt.Errorf("npm: publishing root package %s: %w", root.name, err)
	}

	fmt.Fprintf(stdout, "npm: done\n")
	return nil
}

func buildPlatformPackages(cfg *Config) ([]builtPackage, func(), error) {
	var packages []builtPackage
	var dirs []string
	cleanup := func() {
		for _, d := range dirs {
			_ = os.RemoveAll(d)
		}
	}

	for _, a := range cfg.Artifacts {
		pkgName := fmt.Sprintf("@%s/%s-%s", cfg.Org, cfg.Name, a.PackageSuffix)

		dir, err := os.MkdirTemp("", "gowheels-npm-*")
		if err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("creating temp dir for %s: %w", pkgName, err)
		}
		dirs = append(dirs, dir)

		binDir := filepath.Join(dir, "bin")
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("creating bin dir for %s: %w", pkgName, err)
		}

		binaryName := cfg.Name
		if a.GOOS == "windows" {
			binaryName += ".exe"
		}
		if err := copyFile(a.Path, filepath.Join(binDir, binaryName), 0o755); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("copying binary for %s: %w", pkgName, err)
		}

		pkg := packageJSON{
			Name:        pkgName,
			Version:     cfg.Version,
			Description: fmt.Sprintf("%s binary for %s", cfg.Name, a.PackageSuffix),
			License:     cfg.License,
			Repository:  cfg.Repository,
			OS:          []string{a.NpmOS},
			CPU:         []string{a.NpmCPU},
			Files:       []string{"bin"},
		}
		if err := writeJSON(filepath.Join(dir, "package.json"), pkg); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("writing package.json for %s: %w", pkgName, err)
		}

		packages = append(packages, builtPackage{dir: dir, name: pkgName})
	}

	return packages, cleanup, nil
}

func buildRootPackage(cfg *Config, platforms []builtPackage) (builtPackage, func(), error) {
	dir, err := os.MkdirTemp("", "gowheels-npm-root-*")
	if err != nil {
		return builtPackage{}, nil, fmt.Errorf("creating root package dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }

	// Write wrapper script
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		cleanup()
		return builtPackage{}, nil, fmt.Errorf("creating bin dir: %w", err)
	}
	wrapper := strings.NewReplacer("__BIN_NAME__", cfg.Name, "__ORG_NAME__", cfg.Org).
		Replace(wrapperJS)
	if err := os.WriteFile(filepath.Join(binDir, cfg.Name), []byte(wrapper), 0o755); err != nil {
		cleanup()
		return builtPackage{}, nil, fmt.Errorf("writing wrapper script: %w", err)
	}

	optDeps := make(map[string]string, len(platforms))
	for _, p := range platforms {
		optDeps[p.name] = cfg.Version
	}

	pkg := packageJSON{
		Name:         cfg.Name,
		Version:      cfg.Version,
		Description:  cfg.Summary,
		License:      cfg.License,
		Repository:   cfg.Repository,
		Files:        []string{"bin"},
		Bin:          map[string]string{cfg.Name: fmt.Sprintf("bin/%s", cfg.Name)},
		OptionalDeps: optDeps,
	}
	if err := writeJSON(filepath.Join(dir, "package.json"), pkg); err != nil {
		cleanup()
		return builtPackage{}, nil, fmt.Errorf("writing root package.json: %w", err)
	}

	if cfg.ReadmePath != "" {
		if err := copyFile(cfg.ReadmePath, filepath.Join(dir, "README.md"), 0o644); err != nil {
			cleanup()
			return builtPackage{}, nil, fmt.Errorf("copying README: %w", err)
		}
	}

	return builtPackage{dir: dir, name: cfg.Name}, cleanup, nil
}

func npmPublish(
	ctx context.Context,
	dir, tag string,
	provenance, dryRun bool,
	stdout io.Writer,
) error {
	args := []string{"publish", "--access", "public", "--tag", tag}
	if provenance {
		args = append(args, "--provenance")
	}
	if dryRun {
		fmt.Fprintf(stdout, "npm: [dry run] npm %s (in %s)\n", strings.Join(args, " "), dir)
		return nil
	}
	//nolint:gosec // npm is a well-known binary; args are constructed internally, not from user input
	cmd := exec.CommandContext(ctx, "npm", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.New(npmError(out))
	}
	return nil
}

func npmError(out []byte) string {
	outStr := string(out)
	for _, line := range strings.Split(outStr, "\n") {
		line = strings.TrimSpace(line)
		code, ok := strings.CutPrefix(line, "npm ERR! code ")
		if !ok {
			code, ok = strings.CutPrefix(line, "npm error code ")
		}
		if !ok {
			continue
		}
		switch code {
		case "EOTP":
			return "2FA is blocking publish: generate and use a token with 2FA disabled"
		case "ENEEDAUTH", "E401":
			return "not authenticated: set NODE_AUTH_TOKEN or run npm login"
		case "E403":
			return "permission denied: ensure the token has write access to this package or org"
		case "E409", "EPUBLISHCONFLICT":
			return "version already exists: this version has already been published"
		case "ENOTFOUND", "ETIMEDOUT", "ECONNREFUSED":
			return "network error: unable to reach the npm registry"
		}
	}
	return "publish failed: " + strings.TrimSpace(outStr)
}

// pollAllVisible polls all platform packages concurrently and returns the
// first error, or nil if all become visible before the deadline.
func pollAllVisible(ctx context.Context, pkgs []builtPackage, version string) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(pkgs))
	for _, pkg := range pkgs {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			if err := pollUntilVisible(ctx, name, version); err != nil {
				errCh <- fmt.Errorf("npm: registry propagation timed out for %s: %w", name, err)
			}
		}(pkg.name)
	}
	wg.Wait()
	close(errCh)
	return <-errCh // nil if channel is empty
}

func pollUntilVisible(ctx context.Context, pkgName, version string) error {
	registryURL := "https://registry.npmjs.org/" + url.PathEscape(
		pkgName,
	) + "/" + url.PathEscape(
		version,
	)
	deadline := time.Now().Add(registryPollTimeout)
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, registryURL, nil)
		if err != nil {
			return fmt.Errorf("building registry poll request: %w", err)
		}
		if resp, err := httpClient.Do(req); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("registry poll cancelled: %w", ctx.Err())
		case <-time.After(registryPollInterval):
		}
	}
	return fmt.Errorf("package %s@%s not visible after %s", pkgName, version, registryPollTimeout)
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening source file: %w", err)
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("opening destination file: %w", err)
	}

	if _, err = io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("copying file data: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("closing destination file: %w", err)
	}
	return nil
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling JSON: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing JSON file: %w", err)
	}
	return nil
}
