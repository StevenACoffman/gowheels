package repometa_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/StevenACoffman/gowheels/internal/repometa"
)

// serveJSON starts a test server that serves the given payload as JSON.
func serveJSON(t *testing.T, payload map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(payload)
	}))
}

func serveRepo(t *testing.T, spdxID string) *httptest.Server {
	t.Helper()
	return serveJSON(t, map[string]any{
		"description":    "A test tool",
		"homepage":       "https://example.com",
		"html_url":       "https://github.com/owner/repo",
		"topics":         []string{"cli", "go"},
		"default_branch": "main",
		"license":        map[string]string{"spdx_id": spdxID},
		"owner":          map[string]string{"login": "owner"},
	})
}

func TestFetch_Fields(t *testing.T) {
	ts := serveRepo(t, "MIT")
	defer ts.Close()

	restoreClient := repometa.SetHTTPClient(ts.Client())
	defer restoreClient()
	restoreBase := repometa.SetAPIBase(ts.URL)
	defer restoreBase()

	repo, err := repometa.Fetch(context.Background(), "owner/repo", "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if repo.Description != "A test tool" {
		t.Errorf("Description = %q, want %q", repo.Description, "A test tool")
	}
	if repo.LicenseSPDX != "MIT" {
		t.Errorf("LicenseSPDX = %q, want %q", repo.LicenseSPDX, "MIT")
	}
	if repo.HTMLURL != "https://github.com/owner/repo" {
		t.Errorf("HTMLURL = %q, want %q", repo.HTMLURL, "https://github.com/owner/repo")
	}
	if len(repo.Topics) != 2 || repo.Topics[0] != "cli" {
		t.Errorf("Topics = %v, want [cli go]", repo.Topics)
	}
	if repo.Homepage != "https://example.com" {
		t.Errorf("Homepage = %q, want %q", repo.Homepage, "https://example.com")
	}
	if repo.DefaultBranch != "main" {
		t.Errorf("DefaultBranch = %q, want %q", repo.DefaultBranch, "main")
	}
	if repo.OwnerLogin != "owner" {
		t.Errorf("OwnerLogin = %q, want %q", repo.OwnerLogin, "owner")
	}
}

// TestFetch_NilLicense verifies that a null license field (GitHub cannot
// detect a license at all) results in an empty LicenseSPDX rather than a crash.
func TestFetch_NilLicense(t *testing.T) {
	ts := serveJSON(t, map[string]any{
		"description":    "A test tool",
		"html_url":       "https://github.com/owner/repo",
		"topics":         []string{},
		"default_branch": "main",
		"license":        nil, // GitHub returns null when no license is detected
		"owner":          map[string]string{"login": "owner"},
	})
	defer ts.Close()

	restoreClient := repometa.SetHTTPClient(ts.Client())
	defer restoreClient()
	restoreBase := repometa.SetAPIBase(ts.URL)
	defer restoreBase()

	repo, err := repometa.Fetch(context.Background(), "owner/repo", "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if repo.LicenseSPDX != "" {
		t.Errorf("LicenseSPDX = %q, want empty string for null license", repo.LicenseSPDX)
	}
}

// TestFetch_NonOK verifies that a non-200 HTTP response returns an error.
func TestFetch_NonOK(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
	}))
	defer ts.Close()

	restoreClient := repometa.SetHTTPClient(ts.Client())
	defer restoreClient()
	restoreBase := repometa.SetAPIBase(ts.URL)
	defer restoreBase()

	_, err := repometa.Fetch(context.Background(), "owner/repo", "")
	if err == nil {
		t.Fatal("Fetch: expected error for 403 response, got nil")
	}
}

// TestFetch_MalformedJSON verifies that a malformed response body returns an error.
func TestFetch_MalformedJSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not-valid-json{`))
	}))
	defer ts.Close()

	restoreClient := repometa.SetHTTPClient(ts.Client())
	defer restoreClient()
	restoreBase := repometa.SetAPIBase(ts.URL)
	defer restoreBase()

	_, err := repometa.Fetch(context.Background(), "owner/repo", "")
	if err == nil {
		t.Fatal("Fetch: expected error for malformed JSON, got nil")
	}
}

func TestFetch_SentinelLicense(t *testing.T) {
	for _, tt := range []struct {
		spdx string
		want string
	}{
		{"MIT", "MIT"},
		{"Apache-2.0", "Apache-2.0"},
		{"NOASSERTION", ""},
		{"noassertion", ""},
		{"other", ""},
		{"OTHER", ""},
		{"", ""},
	} {
		t.Run("spdx="+tt.spdx, func(t *testing.T) {
			ts := serveRepo(t, tt.spdx)
			defer ts.Close()

			restoreClient := repometa.SetHTTPClient(ts.Client())
			defer restoreClient()
			restoreBase := repometa.SetAPIBase(ts.URL)
			defer restoreBase()

			repo, err := repometa.Fetch(context.Background(), "owner/repo", "")
			if err != nil {
				t.Fatalf("Fetch: %v", err)
			}
			if repo.LicenseSPDX != tt.want {
				t.Errorf("LicenseSPDX = %q, want %q", repo.LicenseSPDX, tt.want)
			}
		})
	}
}
