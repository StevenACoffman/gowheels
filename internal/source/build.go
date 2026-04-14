package source

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/StevenACoffman/gowheels/internal/platforms"
)

// BuildSource compiles Go source code for each target platform using go build.
// It caches compiled binary bytes so that multiple wheel tags sharing the same
// os/arch pair (e.g. manylinux + musllinux on linux/amd64) do not trigger
// redundant compilations.
type BuildSource struct {
	pkg     string // Go package path (e.g. "." or "./cmd/mytool")
	modDir  string // directory containing go.mod
	ldflags string
	stdout  io.Writer // progress output
	stderr  io.Writer // go build stderr
}

type buildKey struct{ goos, goarch string }

// NewBuildSource creates a BuildSource. Empty pkg and modDir default to ".";
// empty ldflags defaults to "-s". stdout and stderr receive progress and
// compiler output respectively; pass io.Discard to suppress.
func NewBuildSource(pkg, modDir, ldflags string, stdout, stderr io.Writer) *BuildSource {
	if pkg == "" {
		pkg = "."
	}
	if modDir == "" {
		modDir = "."
	}
	if ldflags == "" {
		ldflags = "-s"
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	return &BuildSource{pkg: pkg, modDir: modDir, ldflags: ldflags, stdout: stdout, stderr: stderr}
}

// Resolve compiles each unique os/arch pair once and returns one Binary per
// platform (not per wheel tag — the wheel builder handles tag expansion).
func (s *BuildSource) Resolve(
	ctx context.Context,
	name string,
	plats []platforms.Platform,
) ([]Binary, error) {
	tmpDir, err := os.MkdirTemp("", "gowheels-build-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	cache := make(map[buildKey][]byte)
	var result []Binary

	for _, p := range plats {
		key := buildKey{p.GOOS, p.GOARCH}

		if data, seen := cache[key]; seen {
			result = append(result, Binary{Platform: p, Data: data, Filename: name + p.BinaryExt()})
			continue
		}

		outPath := filepath.Join(
			tmpDir,
			fmt.Sprintf("%s_%s_%s%s", name, p.GOOS, p.GOARCH, p.BinaryExt()),
		)
		fmt.Fprintf(s.stdout, "  building %s/%s...\n", p.GOOS, p.GOARCH)

		if err := compileGo(
			ctx,
			s.modDir,
			outPath,
			p.GOOS,
			p.GOARCH,
			s.pkg,
			s.ldflags,
			s.stderr,
		); err != nil {
			return nil, err
		}

		data, err := os.ReadFile(outPath)
		if err != nil {
			return nil, fmt.Errorf("reading compiled binary for %s/%s: %w", p.GOOS, p.GOARCH, err)
		}
		cache[key] = data
		result = append(result, Binary{Platform: p, Data: data, Filename: name + p.BinaryExt()})
	}

	return result, nil
}

func compileGo(
	ctx context.Context,
	modDir, output, goos, goarch, pkg, ldflags string,
	stderr io.Writer,
) error {
	cmd := exec.CommandContext( //nolint:gosec // binary path is resolved from Go toolchain; user-controlled only in build mode where this is expected
		ctx,
		"go",
		"build",
		"-ldflags="+ldflags,
		"-o",
		output,
		pkg,
	)
	cmd.Dir = modDir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+goos, "GOARCH="+goarch)
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build %s/%s: %w", goos, goarch, err)
	}
	return nil
}
