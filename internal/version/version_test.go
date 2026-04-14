package version_test

import (
	"testing"

	"github.com/StevenACoffman/gowheels/internal/version"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		// v prefix stripped
		{"v1.2.3", "1.2.3", false},
		{"1.2.3", "1.2.3", false},

		// Semver pre-release → PEP 440
		{"v1.2.3-alpha.1", "1.2.3a1", false},
		{"v1.2.3-alpha.0", "1.2.3a0", false},
		{"v1.2.3-beta.2", "1.2.3b2", false},
		{"v1.2.3-rc.3", "1.2.3rc3", false},
		{"v1.2.3-dev.0", "1.2.3.dev0", false},

		// Semver pre-release with no number → default 0
		{"v1.2.3-alpha", "1.2.3a0", false},
		{"v1.2.3-beta", "1.2.3b0", false},
		{"v1.2.3-rc", "1.2.3rc0", false},
		{"v1.2.3-dev", "1.2.3.dev0", false},

		// PEP 440 forms pass through unchanged (after v-strip)
		{"1.2.3a1", "1.2.3a1", false},
		{"1.2.3b2", "1.2.3b2", false},
		{"1.2.3rc1", "1.2.3rc1", false},
		{"1.2.3.post1", "1.2.3.post1", false},
		{"1.2.3.dev0", "1.2.3.dev0", false},
		{"v1.2.3a1", "1.2.3a1", false},

		// Multi-component base versions
		{"v1.0.0", "1.0.0", false},
		{"v0.1.0", "0.1.0", false},
		{"v10.20.30", "10.20.30", false},

		// Unknown semver pre-release label → error
		{"v1.2.3-gamma.1", "", true},
		{"v1.2.3-snapshot.0", "", true},

		// Completely invalid
		{"not-a-version", "", true},
		{
			"v1.2",
			"",
			true,
		}, // only two components; not matched by semver pre-re or PEP 440 (needs 3 parts for pure semver)
		{"1.2.3-", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := version.Normalize(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Normalize(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("Normalize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
