// Package source defines the Source interface and Binary type used to resolve
// Go executables before they are wrapped into Python wheels.
package source

import (
	"context"

	"github.com/StevenACoffman/gowheels/internal/platforms"
)

// Binary is a resolved, ready-to-embed executable for one Platform.
type Binary struct {
	Platform platforms.Platform
	Data     []byte
	Filename string // "{name}" or "{name}.exe"
}

// Source resolves binaries for all requested platforms.
//
// name is the binary base name (without .exe). plats lists the targets to
// resolve; the implementation may resolve fewer if a platform is unavailable.
// Each returned Binary must have its Platform.WheelTags populated so the wheel
// builder can emit one wheel per tag.
type Source interface {
	Resolve(ctx context.Context, name string, plats []platforms.Platform) ([]Binary, error)
}
