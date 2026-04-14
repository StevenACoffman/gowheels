package pypi_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/StevenACoffman/gowheels/internal/pypi"
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
		//nolint:gosec // G120: test server; memory exhaustion not a concern in unit tests
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, "bad multipart", http.StatusBadRequest)
			return
		}
		capturedForm = r.MultipartForm.Value
		for key := range r.MultipartForm.File {
			capturedFileField = key
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	restore := pypi.SetHTTPClient(ts.Client())
	defer restore()

	w := wheel.BuiltWheel{
		Filename: "mytool-1.0.0-py3-none-manylinux_2_17_x86_64.whl",
		Data:     []byte("fake wheel data"),
	}

	if err := pypi.Upload(context.Background(), w, "test-token", ts.URL+"/"); err != nil {
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
