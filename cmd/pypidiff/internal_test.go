// White-box tests for unexported helpers in the pypidiff command.
// Rendering, diff, MIME, and classifier helpers are tested in their respective
// packages (internal/pypi, internal/wheel); this file covers only the helpers
// that are specific to the pypidiff command itself.
package pypidiff

import "testing"

// --- buildExtraURLPairs ---

func TestBuildExtraURLPairs(t *testing.T) {
	t.Parallel()

	t.Run("no URL and no meta returns empty", func(t *testing.T) {
		t.Parallel()
		if got := buildExtraURLPairs("", nil); len(got) != 0 {
			t.Errorf("expected empty, got %v", got)
		}
	})

	t.Run("github URL adds bug tracker and changelog", func(t *testing.T) {
		t.Parallel()
		got := buildExtraURLPairs("https://github.com/owner/repo", nil)
		wantPairs := map[string]string{
			"Bug Tracker": "https://github.com/owner/repo/issues",
			"Changelog":   "https://github.com/owner/repo/releases",
		}
		if len(got) != len(wantPairs) {
			t.Fatalf("expected %d pairs, got %d: %v", len(wantPairs), len(got), got)
		}
		for _, p := range got {
			wantURL, ok := wantPairs[p[0]]
			if !ok {
				t.Errorf("unexpected key %q", p[0])
				continue
			}
			if p[1] != wantURL {
				t.Errorf("pair %q = %q, want %q", p[0], p[1], wantURL)
			}
		}
	})

	t.Run("non-hosting URL omits bug tracker and changelog", func(t *testing.T) {
		t.Parallel()
		got := buildExtraURLPairs("https://example.com/project", nil)
		for _, p := range got {
			if p[0] == "Bug Tracker" || p[0] == "Changelog" {
				t.Errorf("unexpected pair %q for non-hosting URL", p[0])
			}
		}
	})
}
