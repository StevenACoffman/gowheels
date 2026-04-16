// Package pypi white-box tests for unexported helpers.
// This file is intentionally in package pypi (not pypi_test) so that it can
// access unexported symbols such as parseWheelFilename.
package pypi

import (
	"reflect"
	"testing"
)

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

func TestMetadataFormFields(t *testing.T) {
	tests := []struct {
		name     string
		meta     string
		wantKeys []string // form field keys expected (in any order subset)
		wantPair [][2]string
	}{
		{
			name: "empty",
			meta: "",
		},
		{
			name: "typical metadata",
			meta: "Metadata-Version: 2.4\nName: mytool\nVersion: 1.0.0\n" +
				"Summary: My awesome tool\n" +
				"Keywords: go golang cli\n" +
				"Classifier: Development Status :: 5 - Production/Stable\n" +
				"Classifier: Environment :: Console\n" +
				"Project-URL: Repository, https://github.com/owner/mytool\n" +
				"Project-URL: Bug Tracker, https://github.com/owner/mytool/issues\n" +
				"License-Expression: MIT\n" +
				"Requires-Python: >=3.9\n" +
				"Description-Content-Type: text/markdown\n\n" +
				"# mytool\n\nSome readme content.\n",
			wantPair: [][2]string{
				{"summary", "My awesome tool"},
				{"keywords", "go golang cli"},
				{"classifiers", "Development Status :: 5 - Production/Stable"},
				{"classifiers", "Environment :: Console"},
				{"project_urls", "Repository, https://github.com/owner/mytool"},
				{"project_urls", "Bug Tracker, https://github.com/owner/mytool/issues"},
				{"license_expression", "MIT"},
				{"requires_python", ">=3.9"},
				{"description_content_type", "text/markdown"},
				{"description", "# mytool\n\nSome readme content.\n"},
			},
		},
		{
			name: "no body",
			meta: "Metadata-Version: 2.4\nName: mytool\nVersion: 1.0.0\n" +
				"Summary: No readme here\n" +
				"Requires-Python: >=3.9\n",
			wantPair: [][2]string{
				{"summary", "No readme here"},
				{"requires_python", ">=3.9"},
			},
		},
		{
			name:     "body whitespace-only is skipped",
			meta:     "Metadata-Version: 2.4\nName: mytool\nVersion: 1.0.0\n\n   \n",
			wantPair: nil,
		},
		{
			name: "colon in summary value is preserved",
			meta: "Metadata-Version: 2.4\nName: mytool\nVersion: 1.0.0\n" +
				"Summary: My tool: does stuff\n",
			wantPair: [][2]string{
				{"summary", "My tool: does stuff"},
			},
		},
		{
			name: "unknown headers are skipped",
			meta: "Metadata-Version: 2.4\nName: mytool\nVersion: 1.0.0\n" +
				"Author: Alice\n" +
				"Requires-Python: >=3.9\n",
			wantPair: [][2]string{
				{"requires_python", ">=3.9"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := metadataFormFields(tt.meta)
			// Treat nil and empty slice as equivalent — pre-allocation leaves
			// a non-nil empty slice when no headers match.
			if len(got) != len(tt.wantPair) || !reflect.DeepEqual(got, tt.wantPair) {
				if len(got) != 0 || len(tt.wantPair) != 0 {
					t.Errorf("metadataFormFields mismatch\ngot:  %v\nwant: %v", got, tt.wantPair)
				}
			}
		})
	}
}
