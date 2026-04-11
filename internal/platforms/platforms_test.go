package platforms_test

import (
	"testing"

	"github.com/StevenACoffman/gowheels/internal/platforms"
)

func TestLookup(t *testing.T) {
	tests := []struct {
		goos, goarch string
		wantErr      bool
		wantTags     []string
	}{
		{"linux", "amd64", false, []string{"manylinux_2_17_x86_64.manylinux2014_x86_64", "musllinux_1_2_x86_64"}},
		{"linux", "arm64", false, []string{"manylinux_2_17_aarch64.manylinux2014_aarch64", "musllinux_1_2_aarch64"}},
		{"darwin", "amd64", false, []string{"macosx_10_9_x86_64"}},
		{"darwin", "arm64", false, []string{"macosx_11_0_arm64"}},
		{"windows", "amd64", false, []string{"win_amd64"}},
		{"windows", "arm64", false, []string{"win_arm64"}},
		{"freebsd", "amd64", true, nil},
		{"linux", "386", true, nil},
	}

	for _, tt := range tests {
		t.Run(tt.goos+"/"+tt.goarch, func(t *testing.T) {
			p, err := platforms.Lookup(tt.goos, tt.goarch)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Lookup(%q, %q) error = %v, wantErr %v", tt.goos, tt.goarch, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(p.WheelTags) != len(tt.wantTags) {
				t.Fatalf("WheelTags = %v, want %v", p.WheelTags, tt.wantTags)
			}
			for i, tag := range tt.wantTags {
				if p.WheelTags[i] != tag {
					t.Errorf("WheelTags[%d] = %q, want %q", i, p.WheelTags[i], tag)
				}
			}
		})
	}
}

func TestFilter(t *testing.T) {
	t.Run("empty returns all", func(t *testing.T) {
		got, err := platforms.Filter("")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != len(platforms.All) {
			t.Errorf("len = %d, want %d", len(got), len(platforms.All))
		}
	})

	t.Run("single platform", func(t *testing.T) {
		got, err := platforms.Filter("linux/amd64")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1", len(got))
		}
		if got[0].GOOS != "linux" || got[0].GOARCH != "amd64" {
			t.Errorf("got %s/%s, want linux/amd64", got[0].GOOS, got[0].GOARCH)
		}
	})

	t.Run("multiple platforms", func(t *testing.T) {
		got, err := platforms.Filter("linux/amd64,darwin/arm64")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
	})

	t.Run("spaces trimmed", func(t *testing.T) {
		got, err := platforms.Filter(" linux/amd64 , darwin/arm64 ")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("len = %d, want 2", len(got))
		}
	})

	t.Run("invalid platform", func(t *testing.T) {
		_, err := platforms.Filter("freebsd/amd64")
		if err == nil {
			t.Error("expected error for unsupported platform")
		}
	})

	t.Run("missing slash", func(t *testing.T) {
		_, err := platforms.Filter("linuxamd64")
		if err == nil {
			t.Error("expected error for missing slash")
		}
	})
}

func TestPlatformMethods(t *testing.T) {
	t.Run("Windows", func(t *testing.T) {
		p, _ := platforms.Lookup("windows", "amd64")
		if !p.Windows() {
			t.Error("Windows() should be true for windows/amd64")
		}
		if p.BinaryExt() != ".exe" {
			t.Errorf("BinaryExt() = %q, want .exe", p.BinaryExt())
		}
	})

	t.Run("Linux not Windows", func(t *testing.T) {
		p, _ := platforms.Lookup("linux", "amd64")
		if p.Windows() {
			t.Error("Windows() should be false for linux/amd64")
		}
		if p.BinaryExt() != "" {
			t.Errorf("BinaryExt() = %q, want empty", p.BinaryExt())
		}
	})

	goReleaserCases := []struct {
		goos, goarch string
		wantOS       string
		wantArch     string
		wantExt      string
	}{
		{"linux", "amd64", "Linux", "x86_64", "tar.gz"},
		{"linux", "arm64", "Linux", "arm64", "tar.gz"},
		{"darwin", "amd64", "Darwin", "x86_64", "tar.gz"},
		{"darwin", "arm64", "Darwin", "arm64", "tar.gz"},
		{"windows", "amd64", "Windows", "x86_64", "zip"},
		{"windows", "arm64", "Windows", "arm64", "zip"},
	}

	for _, tc := range goReleaserCases {
		t.Run(tc.goos+"/"+tc.goarch+" GoReleaser names", func(t *testing.T) {
			p, _ := platforms.Lookup(tc.goos, tc.goarch)
			if p.GoReleaserOS() != tc.wantOS {
				t.Errorf("GoReleaserOS() = %q, want %q", p.GoReleaserOS(), tc.wantOS)
			}
			if p.GoReleaserArch() != tc.wantArch {
				t.Errorf("GoReleaserArch() = %q, want %q", p.GoReleaserArch(), tc.wantArch)
			}
			if p.ArchiveExt != tc.wantExt {
				t.Errorf("ArchiveExt = %q, want %q", p.ArchiveExt, tc.wantExt)
			}
		})
	}
}
