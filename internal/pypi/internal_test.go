// Package pypi white-box tests for unexported helpers.
// This file is intentionally in package pypi (not pypi_test) so that it can
// access unexported symbols such as parseWheelFilename.
package pypi

import "testing"

func TestParseWheelFilename(t *testing.T) {
	tests := []struct {
		filename    string
		wantName    string
		wantVersion string
		wantErr     bool
	}{
		{
			filename:    "mytool-1.2.3-py3-none-linux_x86_64.whl",
			wantName:    "mytool",
			wantVersion: "1.2.3",
		},
		{
			filename:    "my_tool-0.1.0-py3-none-win_amd64.whl",
			wantName:    "my_tool",
			wantVersion: "0.1.0",
		},
		{
			filename:    "mytool-1.2.3b1-py3-none-macosx_11_0_arm64.whl",
			wantName:    "mytool",
			wantVersion: "1.2.3b1",
		},
		{
			filename:    "mytool-1.2.3-py3-none-manylinux_2_17_x86_64.manylinux2014_x86_64.whl",
			wantName:    "mytool",
			wantVersion: "1.2.3",
		},
		// No dash → error
		{filename: "nodash.whl", wantErr: true},
		// Empty → error
		{filename: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			name, ver, err := parseWheelFilename(tt.filename)
			if (err != nil) != tt.wantErr {
				t.Fatalf(
					"parseWheelFilename(%q) error = %v, wantErr %v",
					tt.filename,
					err,
					tt.wantErr,
				)
			}
			if tt.wantErr {
				return
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
			if ver != tt.wantVersion {
				t.Errorf("version = %q, want %q", ver, tt.wantVersion)
			}
		})
	}
}
