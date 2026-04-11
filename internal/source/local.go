package source

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/StevenACoffman/gowheels/internal/platforms"
)

// LocalSource reads binaries from local artifact paths provided by the caller.
type LocalSource struct {
	// artifacts maps "goos/goarch" → local file path
	artifacts map[string]string
}

// NewLocalSource creates a LocalSource from a slice of "goos/goarch:path"
// strings, as supplied via --artifact flags. All entries are validated eagerly;
// a non-nil error means at least one entry is invalid.
func NewLocalSource(entries []string) (*LocalSource, error) {
	artifacts := make(map[string]string, len(entries))
	var errs []string

	for _, entry := range entries {
		platform, path, ok := strings.Cut(entry, ":")
		if !ok {
			errs = append(errs, fmt.Sprintf("invalid --artifact %q: expected os/arch:path", entry))
			continue
		}
		goos, goarch, ok := strings.Cut(platform, "/")
		if !ok {
			errs = append(errs, fmt.Sprintf("invalid --artifact %q: platform must be os/arch", entry))
			continue
		}
		key := goos + "/" + goarch
		if _, dup := artifacts[key]; dup {
			errs = append(errs, fmt.Sprintf("duplicate --artifact for %s", key))
			continue
		}
		if _, err := platforms.Lookup(goos, goarch); err != nil {
			errs = append(errs, fmt.Sprintf("--artifact %q: %v", entry, err))
			continue
		}
		artifacts[key] = path
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("artifact errors:\n  %s", strings.Join(errs, "\n  "))
	}
	return &LocalSource{artifacts: artifacts}, nil
}

// Resolve reads each requested platform's binary from the local filesystem.
// Duplicate os/arch pairs (caused by multi-tag Linux platforms) are resolved
// only once.
func (s *LocalSource) Resolve(_ context.Context, name string, plats []platforms.Platform) ([]Binary, error) {
	seen := make(map[string]bool)
	var result []Binary
	var errs []string

	for _, p := range plats {
		key := p.GOOS + "/" + p.GOARCH
		if seen[key] {
			continue
		}
		seen[key] = true

		path, ok := s.artifacts[key]
		if !ok {
			errs = append(errs, fmt.Sprintf("no --artifact provided for %s", key))
			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			errs = append(errs, fmt.Sprintf("--artifact %s: %v", key, err))
			continue
		}
		if info.IsDir() {
			errs = append(errs, fmt.Sprintf("--artifact %s: path is a directory, not a file", key))
			continue
		}
		// Check executable bit when building on Unix for a non-Windows target.
		if runtime.GOOS != "windows" && p.GOOS != "windows" && info.Mode()&0111 == 0 {
			errs = append(errs, fmt.Sprintf("--artifact %s: file is not executable", key))
			continue
		}

		data, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, fmt.Sprintf("--artifact %s: reading file: %v", key, err))
			continue
		}

		result = append(result, Binary{
			Platform: p,
			Data:     data,
			Filename: name + p.BinaryExt(),
		})
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("artifact errors:\n  %s", strings.Join(errs, "\n  "))
	}
	return result, nil
}
