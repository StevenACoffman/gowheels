package pypi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/StevenACoffman/gowheels/internal/wheel"
)

// TestUploadFormFields verifies that the multipart form sent to PyPI uses the
// correct digest field names expected by twine 5.x / Metadata-Version 2.4:
//   - sha2_digest (SHA-256 hex) — present
//   - blake2_256_digest (BLAKE2b-256 hex) — present
//   - md5_digest — absent (deprecated by PyPI)
//   - sha256_digest — absent (old alias, replaced by sha2_digest)
func TestUploadFormFields(t *testing.T) {
	var capturedForm map[string][]string
	var capturedFileField string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, "bad multipart", http.StatusBadRequest)
			return
		}
		capturedForm = r.MultipartForm.Value
		// Capture the file part name.
		for key := range r.MultipartForm.File {
			capturedFileField = key
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Temporarily replace the package-level httpClient with one that hits our
	// test server (no actual network call).
	orig := httpClient
	httpClient = ts.Client()
	defer func() { httpClient = orig }()

	w := wheel.BuiltWheel{
		Filename: "mytool-1.0.0-py3-none-manylinux_2_17_x86_64.whl",
		Data:     []byte("fake wheel data"),
	}

	if err := Upload(context.Background(), w, "test-token", ts.URL+"/"); err != nil {
		t.Fatalf("Upload: %v", err)
	}

	present := []string{"sha2_digest", "blake2_256_digest"}
	for _, field := range present {
		if vals := capturedForm[field]; len(vals) == 0 || vals[0] == "" {
			t.Errorf("form field %q is absent or empty; want a hex digest", field)
		}
	}

	absent := []string{"md5_digest", "sha256_digest"}
	for _, field := range absent {
		if _, ok := capturedForm[field]; ok {
			t.Errorf("form field %q is present but must be absent (deprecated)", field)
		}
	}

	if capturedFileField != "content" {
		t.Errorf("file form field = %q, want %q", capturedFileField, "content")
	}

	// Verify sha2_digest is a 64-char hex string (SHA-256).
	if sha2 := capturedForm["sha2_digest"]; len(sha2) > 0 {
		if len(sha2[0]) != 64 {
			t.Errorf("sha2_digest length = %d, want 64 hex chars", len(sha2[0]))
		}
	}

	// Verify blake2_256_digest is a 64-char hex string (BLAKE2b-256).
	if b2 := capturedForm["blake2_256_digest"]; len(b2) > 0 {
		if len(b2[0]) != 64 {
			t.Errorf("blake2_256_digest length = %d, want 64 hex chars", len(b2[0]))
		}
	}
}

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
				t.Fatalf("parseWheelFilename(%q) error = %v, wantErr %v", tt.filename, err, tt.wantErr)
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
