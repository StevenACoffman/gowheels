package pypi

import (
	"strings"
	"testing"
)

func TestSplitLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"a\nb\nc\n", []string{"a", "b", "c"}},
		{"a\nb\nc", []string{"a", "b", "c"}},
		{"single", []string{"single"}},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			got := splitLines(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("splitLines(%q) = %v, want %v", tt.input, got, tt.want)
			}
			for i, w := range tt.want {
				if got[i] != w {
					t.Errorf("[%d] = %q, want %q", i, got[i], w)
				}
			}
		})
	}
}

func TestComputeEdits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		a, b    []string
		wantOps string
	}{
		{
			name:    "identical",
			a:       []string{"a", "b", "c"},
			b:       []string{"a", "b", "c"},
			wantOps: "===",
		},
		{
			name:    "pure insertion",
			a:       []string{},
			b:       []string{"x", "y"},
			wantOps: "++",
		},
		{
			name:    "pure deletion",
			a:       []string{"x", "y"},
			b:       []string{},
			wantOps: "--",
		},
		{
			name:    "substitution",
			a:       []string{"a"},
			b:       []string{"b"},
			wantOps: "-+", // deletion before insertion: standard diff output
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			edits := computeEdits(tt.a, tt.b)
			var ops strings.Builder
			for _, e := range edits {
				ops.WriteByte(e.op)
			}
			if got := ops.String(); got != tt.wantOps {
				t.Errorf("computeEdits ops = %q, want %q", got, tt.wantOps)
			}
		})
	}

	t.Run("line numbers are 1-based and correct", func(t *testing.T) {
		t.Parallel()
		// a=["x","y","z"], b=["x","z"]: LCS is ["x","z"], "y" is deleted.
		edits := computeEdits([]string{"x", "y", "z"}, []string{"x", "z"})
		for _, e := range edits {
			switch e.line {
			case "x":
				if e.op != '=' || e.aIdx != 1 || e.bIdx != 1 {
					t.Errorf("line 'x': op=%c aIdx=%d bIdx=%d", e.op, e.aIdx, e.bIdx)
				}
			case "y":
				if e.op != '-' || e.aIdx != 2 || e.bIdx != 0 {
					t.Errorf("line 'y': op=%c aIdx=%d bIdx=%d", e.op, e.aIdx, e.bIdx)
				}
			case "z":
				if e.op != '=' || e.aIdx != 3 || e.bIdx != 2 {
					t.Errorf("line 'z': op=%c aIdx=%d bIdx=%d", e.op, e.aIdx, e.bIdx)
				}
			}
		}
	})
}

func TestBuildHunks(t *testing.T) {
	t.Parallel()

	t.Run("pure insertion at start produces @@ -0,0 header", func(t *testing.T) {
		t.Parallel()
		hunks := buildHunks([]string{}, []string{"x", "y"}, DiffContext)
		if len(hunks) != 1 {
			t.Fatalf("expected 1 hunk, got %d", len(hunks))
		}
		if !strings.HasPrefix(hunks[0], "@@ -0,0 +1,2 @@") {
			t.Errorf("expected @@ -0,0 +1,2 @@, got %q", hunks[0])
		}
	})

	t.Run("pure deletion produces @@ +0,0 header", func(t *testing.T) {
		t.Parallel()
		hunks := buildHunks([]string{"x", "y"}, []string{}, DiffContext)
		if len(hunks) != 1 {
			t.Fatalf("expected 1 hunk, got %d", len(hunks))
		}
		if !strings.HasPrefix(hunks[0], "@@ -1,2 +0,0 @@") {
			t.Errorf("expected @@ -1,2 +0,0 @@, got %q", hunks[0])
		}
	})
}

func TestUnifiedDiff(t *testing.T) {
	t.Parallel()

	t.Run("identical texts return empty string", func(t *testing.T) {
		t.Parallel()
		if got := UnifiedDiff("a", "b", "x\ny\n", "x\ny\n"); got != "" {
			t.Errorf("expected empty diff for identical texts, got %q", got)
		}
	})

	t.Run("diff has --- and +++ header lines", func(t *testing.T) {
		t.Parallel()
		got := UnifiedDiff("local", "remote", "a\n", "b\n")
		mustContain(t, got, "--- local")
		mustContain(t, got, "+++ remote")
	})

	t.Run("substitution renders deletion before insertion", func(t *testing.T) {
		t.Parallel()
		got := UnifiedDiff("a", "b", "same\nold\nsame\n", "same\nnew\nsame\n")
		delIdx := strings.Index(got, "-old")
		addIdx := strings.Index(got, "+new")
		if delIdx == -1 || addIdx == -1 {
			t.Fatalf("missing -old or +new in diff:\n%s", got)
		}
		if delIdx > addIdx {
			t.Errorf("-old appears after +new; want deletion before insertion:\n%s", got)
		}
	})

	t.Run("context lines appear with space prefix", func(t *testing.T) {
		t.Parallel()
		got := UnifiedDiff("a", "b",
			"ctx1\nctx2\nctx3\nold\nctx4\nctx5\nctx6\n",
			"ctx1\nctx2\nctx3\nnew\nctx4\nctx5\nctx6\n",
		)
		mustContain(t, got, " ctx1")
		mustContain(t, got, " ctx6")
	})

	t.Run("hunk header @@ is present", func(t *testing.T) {
		t.Parallel()
		mustContain(t, UnifiedDiff("a", "b", "a\n", "b\n"), "@@")
	})

	t.Run("insertion-only diff", func(t *testing.T) {
		t.Parallel()
		got := UnifiedDiff("a", "b", "x\n", "x\ny\n")
		mustContain(t, got, "+y")
		mustNotContain(t, got, "-y")
	})

	t.Run("deletion-only diff", func(t *testing.T) {
		t.Parallel()
		got := UnifiedDiff("a", "b", "x\ny\n", "x\n")
		mustContain(t, got, "-y")
		mustNotContain(t, got, "+y")
	})
}

func TestTruncateReadmeBody(t *testing.T) {
	t.Parallel()

	t.Run("no long description unchanged", func(t *testing.T) {
		t.Parallel()
		text := "Metadata-Version: 2.4\nName: tool\n"
		if got := TruncateReadmeBody(text); got != text {
			t.Errorf("expected unchanged, got %q", got)
		}
	})

	t.Run("body at limit is not truncated", func(t *testing.T) {
		t.Parallel()
		text := "Metadata-Version: 2.4\nName: tool\n\n" + strings.Repeat("x", DiffReadmeLimit)
		if got := TruncateReadmeBody(text); got != text {
			t.Errorf("expected unchanged for body at limit")
		}
	})

	t.Run("long body is truncated with marker", func(t *testing.T) {
		t.Parallel()
		body := strings.Repeat("x", DiffReadmeLimit+100)
		text := "Metadata-Version: 2.4\nName: tool\n\n" + body
		got := TruncateReadmeBody(text)
		if len(got) >= len(text) {
			t.Errorf("expected truncation, got same or longer output")
		}
		mustContain(t, got, "bytes omitted from diff")
		mustContain(t, got, "Metadata-Version: 2.4")
		mustContain(t, got, strings.Repeat("x", DiffReadmeLimit))
	})

	t.Run("header section is not confused for body separator", func(t *testing.T) {
		// Ensure a multi-header METADATA text with no body is returned unchanged,
		// confirming that the search anchors on the first blank line only.
		t.Parallel()
		text := "Metadata-Version: 2.4\nName: tool\nSummary: a tool\n"
		if got := TruncateReadmeBody(text); got != text {
			t.Errorf("expected unchanged for header-only text, got %q", got)
		}
	})
}
