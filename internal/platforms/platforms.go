// Package platforms defines the GOOS/GOARCH targets gowheels supports and
// their corresponding Python wheel platform tags.
package platforms

import (
	"fmt"
	"strings"
)

// All lists every platform gowheels supports. Linux targets produce two wheel
// tags (manylinux glibc + musllinux) from a single static binary.
var All = []Platform{
	{
		GOOS:       "linux",
		GOARCH:     "amd64",
		WheelTags:  []string{"manylinux_2_17_x86_64.manylinux2014_x86_64", "musllinux_1_2_x86_64"},
		ArchiveExt: "tar.gz",
	},
	{
		GOOS:   "linux",
		GOARCH: "arm64",
		WheelTags: []string{
			"manylinux_2_17_aarch64.manylinux2014_aarch64",
			"musllinux_1_2_aarch64",
		},
		ArchiveExt: "tar.gz",
	},
	{
		GOOS:       "darwin",
		GOARCH:     "amd64",
		WheelTags:  []string{"macosx_10_9_x86_64"},
		ArchiveExt: "tar.gz",
	},
	{
		GOOS:       "darwin",
		GOARCH:     "arm64",
		WheelTags:  []string{"macosx_11_0_arm64"},
		ArchiveExt: "tar.gz",
	},
	{
		GOOS:       "windows",
		GOARCH:     "amd64",
		WheelTags:  []string{"win_amd64"},
		ArchiveExt: "zip",
	},
	{
		GOOS:       "windows",
		GOARCH:     "arm64",
		WheelTags:  []string{"win_arm64"},
		ArchiveExt: "zip",
	},
}

// Platform describes a single GOOS/GOARCH target and the Python wheel tags it
// maps to. A single compiled binary covers all WheelTags for its os/arch pair
// (valid when CGO_ENABLED=0 produces a fully static binary).
type Platform struct {
	GOOS       string
	GOARCH     string
	WheelTags  []string // one or more PEP 425 platform tags
	ArchiveExt string   // archive extension used by GoReleaser ("tar.gz" or "zip")
}

// Windows reports whether this is a Windows target.
func (p Platform) Windows() bool { return p.GOOS == "windows" }

// BinaryExt returns ".exe" on Windows, "" otherwise.
func (p Platform) BinaryExt() string {
	if p.Windows() {
		return ".exe"
	}
	return ""
}

// GoReleaserOS returns the title-cased OS name used in GoReleaser archive names.
func (p Platform) GoReleaserOS() string {
	switch p.GOOS {
	case "linux":
		return "Linux"
	case "darwin":
		return "Darwin"
	case "windows":
		return "Windows"
	default:
		return strings.ToUpper(p.GOOS[:1]) + p.GOOS[1:]
	}
}

// GoReleaserArch returns the arch string used in GoReleaser archive names.
func (p Platform) GoReleaserArch() string {
	switch p.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "arm64"
	case "386":
		return "i386"
	default:
		return p.GOARCH
	}
}

// Lookup returns the Platform for the given GOOS/GOARCH pair, or an error if
// it is not in the supported set.
func Lookup(goos, goarch string) (Platform, error) {
	for _, p := range All {
		if p.GOOS == goos && p.GOARCH == goarch {
			return p, nil
		}
	}
	return Platform{}, fmt.Errorf("unsupported platform: %s/%s", goos, goarch)
}

// Filter returns platforms matching the comma-separated os/arch filter string.
// An empty filter returns all platforms.
func Filter(filter string) ([]Platform, error) {
	if filter == "" {
		return All, nil
	}
	var result []Platform
	for _, part := range strings.Split(filter, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		goos, goarch, ok := strings.Cut(part, "/")
		if !ok {
			return nil, fmt.Errorf("invalid platform %q: expected os/arch", part)
		}
		p, err := Lookup(goos, goarch)
		if err != nil {
			return nil, err
		}
		result = append(result, p)
	}
	return result, nil
}
