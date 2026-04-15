// White-box tests for unexported helpers in the pypidiff package.
// This file is intentionally in package pypidiff (not pypidiff_test) so that
// unexported functions such as diffMetadata, equalKeywords, and formatURLLines
// can be tested directly without an export shim.
package pypidiff

import (
	"strings"
	"testing"

	pypiclient "github.com/StevenACoffman/gowheels/internal/pypi"
)

// --- diffMetadata ---

func TestDiffMetadata(t *testing.T) {
	t.Parallel()

	base := &localMetadata{
		summary:        "a great tool",
		licenseExpr:    "MIT",
		keywords:       []string{"go", "cli"},
		projectURLs:    map[string]string{"Repository": "https://github.com/a/b"},
		requiresPython: ">=3.9",
	}

	baseRemote := &pypiclient.PackageInfo{
		Summary:           "a great tool",
		LicenseExpression: "MIT",
		Keywords:          "go cli",
		ProjectURLs:       map[string]string{"Repository": "https://github.com/a/b"},
		RequiresPython:    ">=3.9",
	}

	tests := []struct {
		name       string
		local      *localMetadata
		remote     *pypiclient.PackageInfo
		wantFields []string // names of fields that should appear in diffs
	}{
		{
			name:       "no differences",
			local:      base,
			remote:     baseRemote,
			wantFields: nil,
		},
		{
			name:  "summary differs",
			local: base,
			remote: &pypiclient.PackageInfo{
				Summary:           "different summary",
				LicenseExpression: "MIT",
				Keywords:          "go cli",
				ProjectURLs:       map[string]string{"Repository": "https://github.com/a/b"},
				RequiresPython:    ">=3.9",
			},
			wantFields: []string{"Summary"},
		},
		{
			name:  "license differs",
			local: base,
			remote: &pypiclient.PackageInfo{
				Summary:           "a great tool",
				LicenseExpression: "Apache-2.0",
				Keywords:          "go cli",
				ProjectURLs:       map[string]string{"Repository": "https://github.com/a/b"},
				RequiresPython:    ">=3.9",
			},
			wantFields: []string{"License-Expression"},
		},
		{
			name: "local license empty, remote non-empty",
			local: &localMetadata{
				summary:        "a great tool",
				keywords:       []string{"go", "cli"},
				projectURLs:    map[string]string{"Repository": "https://github.com/a/b"},
				requiresPython: ">=3.9",
			},
			remote: &pypiclient.PackageInfo{
				Summary:           "a great tool",
				LicenseExpression: "MIT",
				Keywords:          "go cli",
				ProjectURLs:       map[string]string{"Repository": "https://github.com/a/b"},
				RequiresPython:    ">=3.9",
			},
			wantFields: []string{"License-Expression"},
		},
		{
			name:  "keywords differ",
			local: base,
			remote: &pypiclient.PackageInfo{
				Summary:           "a great tool",
				LicenseExpression: "MIT",
				Keywords:          "go cli tool",
				ProjectURLs:       map[string]string{"Repository": "https://github.com/a/b"},
				RequiresPython:    ">=3.9",
			},
			wantFields: []string{"Keywords"},
		},
		{
			name: "keyword whitespace normalised — no diff",
			local: &localMetadata{
				summary:        "a great tool",
				licenseExpr:    "MIT",
				keywords:       []string{"go", "cli"},
				projectURLs:    map[string]string{"Repository": "https://github.com/a/b"},
				requiresPython: ">=3.9",
			},
			remote: &pypiclient.PackageInfo{
				Summary:           "a great tool",
				LicenseExpression: "MIT",
				Keywords:          "go  cli", // extra space; normalised to "go cli"
				ProjectURLs:       map[string]string{"Repository": "https://github.com/a/b"},
				RequiresPython:    ">=3.9",
			},
			wantFields: nil,
		},
		{
			name:  "requires-python differs",
			local: base,
			remote: &pypiclient.PackageInfo{
				Summary:           "a great tool",
				LicenseExpression: "MIT",
				Keywords:          "go cli",
				ProjectURLs:       map[string]string{"Repository": "https://github.com/a/b"},
				RequiresPython:    ">=3.8",
			},
			wantFields: []string{"Requires-Python"},
		},
		{
			name:  "project-urls differ",
			local: base,
			remote: &pypiclient.PackageInfo{
				Summary:           "a great tool",
				LicenseExpression: "MIT",
				Keywords:          "go cli",
				ProjectURLs:       map[string]string{"Repository": "https://github.com/a/c"},
				RequiresPython:    ">=3.9",
			},
			wantFields: []string{"Project-URLs"},
		},
		{
			name:  "project-url key case-insensitive match — no diff",
			local: base, // has "Repository" key
			remote: &pypiclient.PackageInfo{
				Summary:           "a great tool",
				LicenseExpression: "MIT",
				Keywords:          "go cli",
				ProjectURLs:       map[string]string{"repository": "https://github.com/a/b"},
				RequiresPython:    ">=3.9",
			},
			wantFields: nil,
		},
		{
			name:  "multiple fields differ",
			local: base,
			remote: &pypiclient.PackageInfo{
				Summary:           "different",
				LicenseExpression: "Apache-2.0",
				Keywords:          "go cli",
				ProjectURLs:       map[string]string{"Repository": "https://github.com/a/b"},
				RequiresPython:    ">=3.9",
			},
			wantFields: []string{"Summary", "License-Expression"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			diffs := diffMetadata(tt.local, tt.remote)

			gotFields := make([]string, len(diffs))
			for i, d := range diffs {
				gotFields[i] = d.field
			}

			if len(diffs) != len(tt.wantFields) {
				t.Errorf("diffMetadata() = %v, want fields %v", gotFields, tt.wantFields)
				return
			}
			for i, want := range tt.wantFields {
				if gotFields[i] != want {
					t.Errorf("diff[%d].field = %q, want %q", i, gotFields[i], want)
				}
			}
		})
	}
}

// --- equalKeywords ---

func TestEqualKeywords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		a, b string
		want bool
	}{
		{"go cli", "go cli", true},
		{"go  cli", "go cli", true},  // extra internal space normalised
		{"go cli", "go  cli", true},  // symmetric
		{" go cli ", "go cli", true}, // leading/trailing space normalised
		{"go cli", "cli go", false},  // order matters
		{"go cli", "go", false},      // different token count
		{"", "", true},
		{"go", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			t.Parallel()
			if got := equalKeywords(tt.a, tt.b); got != tt.want {
				t.Errorf("equalKeywords(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

// --- equalURLMaps ---

func TestEqualURLMaps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b map[string]string
		want bool
	}{
		{
			name: "identical",
			a:    map[string]string{"Repository": "https://github.com/a/b"},
			b:    map[string]string{"Repository": "https://github.com/a/b"},
			want: true,
		},
		{
			name: "key case differs — equal",
			a:    map[string]string{"Repository": "https://github.com/a/b"},
			b:    map[string]string{"repository": "https://github.com/a/b"},
			want: true,
		},
		{
			name: "value differs",
			a:    map[string]string{"Repository": "https://github.com/a/b"},
			b:    map[string]string{"Repository": "https://github.com/a/c"},
			want: false,
		},
		{
			name: "different key count",
			a:    map[string]string{"Repository": "https://github.com/a/b"},
			b: map[string]string{
				"Repository": "https://github.com/a/b",
				"Homepage":   "https://example.com",
			},
			want: false,
		},
		{
			name: "both empty",
			a:    map[string]string{},
			b:    map[string]string{},
			want: true,
		},
		{
			name: "both nil",
			a:    nil,
			b:    nil,
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := equalURLMaps(tt.a, tt.b); got != tt.want {
				t.Errorf("equalURLMaps() = %v, want %v", got, tt.want)
			}
		})
	}
}

// --- formatURLLines ---

func TestFormatURLLines(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		urls map[string]string
		want string
	}{
		{
			name: "empty map",
			urls: map[string]string{},
			want: "(none)",
		},
		{
			name: "nil map",
			urls: nil,
			want: "(none)",
		},
		{
			name: "single entry",
			urls: map[string]string{"Repository": "https://github.com/a/b"},
			want: "Repository: https://github.com/a/b",
		},
		{
			name: "multiple entries sorted alphabetically",
			urls: map[string]string{
				"Repository":  "https://github.com/a/b",
				"Bug Tracker": "https://github.com/a/b/issues",
				"Changelog":   "https://github.com/a/b/releases",
			},
			want: "Bug Tracker: https://github.com/a/b/issues\n" +
				"Changelog: https://github.com/a/b/releases\n" +
				"Repository: https://github.com/a/b",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatURLLines(tt.urls)
			if got != tt.want {
				t.Errorf("formatURLLines() =\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

// --- buildProjectURLs ---

func TestBuildProjectURLs(t *testing.T) {
	t.Parallel()

	t.Run("no repo URL", func(t *testing.T) {
		t.Parallel()
		got := buildProjectURLs("", nil)
		if len(got) != 0 {
			t.Errorf("expected empty map, got %v", got)
		}
	})

	t.Run("github URL adds bug tracker and changelog", func(t *testing.T) {
		t.Parallel()
		got := buildProjectURLs("https://github.com/owner/repo", nil)
		if got["Repository"] != "https://github.com/owner/repo" {
			t.Errorf("Repository = %q", got["Repository"])
		}
		if got["Bug Tracker"] != "https://github.com/owner/repo/issues" {
			t.Errorf("Bug Tracker = %q", got["Bug Tracker"])
		}
		if got["Changelog"] != "https://github.com/owner/repo/releases" {
			t.Errorf("Changelog = %q", got["Changelog"])
		}
		if len(got) != 3 {
			t.Errorf("expected 3 entries, got %d: %v", len(got), got)
		}
	})

	t.Run("non-hosting URL omits bug tracker and changelog", func(t *testing.T) {
		t.Parallel()
		got := buildProjectURLs("https://example.com/project", nil)
		if _, ok := got["Bug Tracker"]; ok {
			t.Errorf("Bug Tracker should be absent for non-hosting URL")
		}
		if _, ok := got["Changelog"]; ok {
			t.Errorf("Changelog should be absent for non-hosting URL")
		}
	})
}

// --- buildClassifiers ---

func TestBuildClassifiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		version string
		wantLen int
		wantDev string
	}{
		{"1.0.0", 3, "Development Status :: 5 - Production/Stable"},
		{"1.0.0b1", 3, "Development Status :: 4 - Beta"},
		{"1.0.0a1", 3, "Development Status :: 3 - Alpha"},
		{"1.0.0.dev1", 3, "Development Status :: 2 - Pre-Alpha"},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			t.Parallel()
			got := buildClassifiers(tt.version)
			if len(got) != tt.wantLen {
				t.Fatalf(
					"buildClassifiers(%q) len = %d, want %d: %v",
					tt.version,
					len(got),
					tt.wantLen,
					got,
				)
			}
			if got[0] != tt.wantDev {
				t.Errorf("got[0] = %q, want %q", got[0], tt.wantDev)
			}
			if got[1] != "Environment :: Console" {
				t.Errorf("got[1] = %q, want %q", got[1], "Environment :: Console")
			}
			if got[2] != "Programming Language :: Python :: 3" {
				t.Errorf("got[2] = %q, want %q", got[2], "Programming Language :: Python :: 3")
			}
		})
	}
}

// --- filterOSClassifiers ---

func TestFilterOSClassifiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input []string
		want  []string
	}{
		{
			name:  "nil input",
			input: nil,
			want:  []string{},
		},
		{
			name:  "no OS classifiers",
			input: []string{"Environment :: Console", "Programming Language :: Python :: 3"},
			want:  []string{"Environment :: Console", "Programming Language :: Python :: 3"},
		},
		{
			name: "OS classifiers removed and result sorted",
			input: []string{
				"Development Status :: 5 - Production/Stable",
				"Environment :: Console",
				"Operating System :: MacOS",
				"Operating System :: POSIX :: Linux",
				"Programming Language :: Python :: 3",
			},
			want: []string{
				"Development Status :: 5 - Production/Stable",
				"Environment :: Console",
				"Programming Language :: Python :: 3",
			},
		},
		{
			name:  "all OS classifiers removed returns empty",
			input: []string{"Operating System :: MacOS", "Operating System :: POSIX :: Linux"},
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := filterOSClassifiers(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("filterOSClassifiers() = %v, want %v", got, tt.want)
			}
			for i, w := range tt.want {
				if got[i] != w {
					t.Errorf("filterOSClassifiers()[%d] = %q, want %q", i, got[i], w)
				}
			}
		})
	}
}

// --- formatClassifiers ---

func TestFormatClassifiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		classifiers []string
		want        string
	}{
		{
			name:        "nil returns (none)",
			classifiers: nil,
			want:        "(none)",
		},
		{
			name:        "empty returns (none)",
			classifiers: []string{},
			want:        "(none)",
		},
		{
			name:        "single entry",
			classifiers: []string{"Environment :: Console"},
			want:        "Environment :: Console",
		},
		{
			name: "multiple entries joined by newlines",
			classifiers: []string{
				"Development Status :: 5 - Production/Stable",
				"Environment :: Console",
				"Programming Language :: Python :: 3",
			},
			want: "Development Status :: 5 - Production/Stable\n" +
				"Environment :: Console\n" +
				"Programming Language :: Python :: 3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatClassifiers(tt.classifiers)
			if got != tt.want {
				t.Errorf("formatClassifiers() =\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}

// --- diffMetadata classifier cases ---

func TestDiffMetadataClassifiers(t *testing.T) {
	t.Parallel()

	stableClassifiers := buildClassifiers("1.0.0")
	remoteStable := []string{
		"Development Status :: 5 - Production/Stable",
		"Environment :: Console",
		"Operating System :: MacOS",
		"Operating System :: POSIX :: Linux",
		"Programming Language :: Python :: 3",
	}

	tests := []struct {
		name       string
		local      *localMetadata
		remote     *pypiclient.PackageInfo
		wantFields []string
	}{
		{
			name: "matching classifiers — no diff (OS entries filtered from remote)",
			local: &localMetadata{
				classifiers: stableClassifiers,
			},
			remote: &pypiclient.PackageInfo{
				Classifiers: remoteStable,
			},
			wantFields: nil,
		},
		{
			name: "dev status mismatch — diff reported",
			local: &localMetadata{
				classifiers: buildClassifiers("1.0.0b1"), // Beta
			},
			remote: &pypiclient.PackageInfo{
				Classifiers: remoteStable, // Production/Stable
			},
			wantFields: []string{"Classifiers"},
		},
		{
			name: "extra remote classifier — diff reported",
			local: &localMetadata{
				classifiers: stableClassifiers,
			},
			remote: &pypiclient.PackageInfo{
				Classifiers: append(remoteStable, "Topic :: Utilities"),
			},
			wantFields: []string{"Classifiers"},
		},
		{
			name: "both empty — no diff",
			local: &localMetadata{
				classifiers: nil,
			},
			remote:     &pypiclient.PackageInfo{},
			wantFields: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			diffs := diffMetadata(tt.local, tt.remote)

			gotFields := make([]string, len(diffs))
			for i, d := range diffs {
				gotFields[i] = d.field
			}

			if len(diffs) != len(tt.wantFields) {
				t.Errorf("diffMetadata() = %v, want fields %v", gotFields, tt.wantFields)
				return
			}
			for i, want := range tt.wantFields {
				if gotFields[i] != want {
					t.Errorf("diff[%d].field = %q, want %q", i, gotFields[i], want)
				}
			}
		})
	}
}

// --- printDiffValue ---

func TestPrintDiffValue(t *testing.T) {
	t.Parallel()

	t.Run("empty value shows (empty)", func(t *testing.T) {
		t.Parallel()
		var sb strings.Builder
		printDiffValue(&sb, "local", "")
		if !strings.Contains(sb.String(), "(empty)") {
			t.Errorf("expected (empty), got %q", sb.String())
		}
	})

	t.Run("single line value", func(t *testing.T) {
		t.Parallel()
		var sb strings.Builder
		printDiffValue(&sb, "local", "MIT")
		got := sb.String()
		if !strings.Contains(got, "local:") || !strings.Contains(got, "MIT") {
			t.Errorf("unexpected output: %q", got)
		}
	})

	t.Run("multi-line value indents continuations", func(t *testing.T) {
		t.Parallel()
		var sb strings.Builder
		printDiffValue(&sb, "remote", "Bug Tracker: https://x\nChangelog: https://y")
		lines := strings.Split(strings.TrimRight(sb.String(), "\n"), "\n")
		if len(lines) != 2 {
			t.Fatalf("expected 2 lines, got %d: %v", len(lines), lines)
		}
		// First line has the "remote:" prefix; second is indented to same column.
		prefix0 := strings.IndexByte(lines[0], 'B') // first char of value
		prefix1 := strings.IndexByte(lines[1], 'C') // first char of continuation
		if prefix0 != prefix1 {
			t.Errorf(
				"value columns differ: line0 col %d, line1 col %d\n%s",
				prefix0,
				prefix1,
				sb.String(),
			)
		}
	})

	t.Run("local and remote labels align", func(t *testing.T) {
		t.Parallel()
		var sb strings.Builder
		printDiffValue(&sb, "local", "x")
		printDiffValue(&sb, "remote", "y")
		lines := strings.Split(strings.TrimRight(sb.String(), "\n"), "\n")
		// Both values should start at the same column.
		col0 := strings.Index(lines[0], "x")
		col1 := strings.Index(lines[1], "y")
		if col0 != col1 {
			t.Errorf(
				"value columns differ: local col %d, remote col %d\n%s",
				col0,
				col1,
				sb.String(),
			)
		}
	})
}

// --- formatDescriptionSummary ---

func TestFormatDescriptionSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		content     string
		contentType string
		want        string
	}{
		{
			name:        "empty content returns (none)",
			content:     "",
			contentType: "",
			want:        "(none)",
		},
		{
			name:        "whitespace-only content returns (none)",
			content:     "   \n  ",
			contentType: "text/markdown",
			want:        "(none)",
		},
		{
			name:        "present with content-type",
			content:     "# Hello\n\nWorld",
			contentType: "text/markdown",
			want:        "text/markdown (14 bytes)",
		},
		{
			name:        "present without content-type defaults to text/plain",
			content:     "hello",
			contentType: "",
			want:        "text/plain (5 bytes)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatDescriptionSummary(tt.content, tt.contentType)
			if got != tt.want {
				t.Errorf("formatDescriptionSummary() = %q, want %q", got, tt.want)
			}
		})
	}
}

// --- detectReadmeContentType ---

func TestDetectReadmeContentType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want string
	}{
		{"README.md", "text/markdown"},
		{"README.markdown", "text/markdown"},
		{"README.MD", "text/markdown"}, // case-insensitive
		{"README.rst", "text/x-rst"},
		{"README.RST", "text/x-rst"},
		{"README.txt", "text/plain"},
		{"README", "text/plain"},
		{"docs/index.md", "text/markdown"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			got := detectReadmeContentType(tt.path)
			if got != tt.want {
				t.Errorf("detectReadmeContentType(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// --- diffMetadata description cases ---

func TestDiffMetadataDescription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		local      *localMetadata
		remote     *pypiclient.PackageInfo
		wantFields []string
	}{
		{
			name:       "both absent — no diff",
			local:      &localMetadata{},
			remote:     &pypiclient.PackageInfo{},
			wantFields: nil,
		},
		{
			name: "both present, same content-type and size — no diff",
			local: &localMetadata{
				readme:            "# Hello",
				readmeContentType: "text/markdown",
			},
			remote: &pypiclient.PackageInfo{
				Description:            "# Hello",
				DescriptionContentType: "text/markdown",
			},
			wantFields: nil,
		},
		{
			name:  "local has README, remote absent — diff reported",
			local: &localMetadata{readme: "# Hello", readmeContentType: "text/markdown"},
			remote: &pypiclient.PackageInfo{
				Description:            "",
				DescriptionContentType: "",
			},
			wantFields: []string{"Description"},
		},
		{
			name:  "remote has README, local absent — diff reported",
			local: &localMetadata{},
			remote: &pypiclient.PackageInfo{
				Description:            "# Hello",
				DescriptionContentType: "text/markdown",
			},
			wantFields: []string{"Description"},
		},
		{
			name: "same content, different content-type — diff reported",
			local: &localMetadata{
				readme:            "hello",
				readmeContentType: "text/markdown",
			},
			remote: &pypiclient.PackageInfo{
				Description:            "hello",
				DescriptionContentType: "text/x-rst",
			},
			wantFields: []string{"Description"},
		},
		{
			name: "same content-type, different length — diff reported",
			local: &localMetadata{
				readme:            "short",
				readmeContentType: "text/markdown",
			},
			remote: &pypiclient.PackageInfo{
				Description:            "a much longer description",
				DescriptionContentType: "text/markdown",
			},
			wantFields: []string{"Description"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			diffs := diffMetadata(tt.local, tt.remote)

			gotFields := make([]string, len(diffs))
			for i, d := range diffs {
				gotFields[i] = d.field
			}

			if len(diffs) != len(tt.wantFields) {
				t.Errorf("diffMetadata() = %v, want fields %v", gotFields, tt.wantFields)
				return
			}
			for i, want := range tt.wantFields {
				if gotFields[i] != want {
					t.Errorf("diff[%d].field = %q, want %q", i, gotFields[i], want)
				}
			}
		})
	}
}
