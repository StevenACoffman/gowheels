package pypi

import (
	"strings"
	"testing"
)

func TestRenderAsMetadata(t *testing.T) {
	t.Parallel()

	t.Run("minimal package", func(t *testing.T) {
		t.Parallel()
		info := &PackageInfo{Name: "mytool", Version: "1.0.0"}
		got := RenderAsMetadata(info)
		mustContain(t, got, "Metadata-Version: 2.4")
		mustContain(t, got, "Name: mytool")
		mustContain(t, got, "Version: 1.0.0")
		mustNotContain(t, got, "Summary:")
		mustNotContain(t, got, "Keywords:")
		mustNotContain(t, got, "Description-Content-Type:")
	})

	t.Run("name is normalised", func(t *testing.T) {
		t.Parallel()
		got := RenderAsMetadata(&PackageInfo{Name: "My-Tool", Version: "1.0.0"})
		mustContain(t, got, "Name: my_tool")
	})

	t.Run("OS classifiers are stripped", func(t *testing.T) {
		t.Parallel()
		info := &PackageInfo{
			Name:    "tool",
			Version: "1.0.0",
			Classifiers: []string{
				"Environment :: Console",
				"Operating System :: POSIX :: Linux",
				"Operating System :: MacOS",
				"Programming Language :: Python :: 3",
			},
		}
		got := RenderAsMetadata(info)
		mustNotContain(t, got, "Operating System")
		mustContain(t, got, "Classifier: Environment :: Console")
		mustContain(t, got, "Classifier: Programming Language :: Python :: 3")
	})

	t.Run("project URLs are sorted alphabetically", func(t *testing.T) {
		t.Parallel()
		info := &PackageInfo{
			Name:    "tool",
			Version: "1.0.0",
			ProjectURLs: map[string]string{
				"Repository":  "https://github.com/a/b",
				"Bug Tracker": "https://github.com/a/b/issues",
				"Changelog":   "https://github.com/a/b/releases",
			},
		}
		got := RenderAsMetadata(info)
		bugIdx := strings.Index(got, "Bug Tracker")
		changeIdx := strings.Index(got, "Changelog")
		repoIdx := strings.Index(got, "Repository")
		if !(bugIdx < changeIdx && changeIdx < repoIdx) {
			t.Errorf("Project-URLs not in alphabetical order:\n%s", got)
		}
	})

	t.Run("README body emitted after blank separator line", func(t *testing.T) {
		t.Parallel()
		info := &PackageInfo{
			Name:                   "tool",
			Version:                "1.0.0",
			Description:            "# Hello\n\nWorld",
			DescriptionContentType: "text/markdown",
		}
		got := RenderAsMetadata(info)
		mustContain(t, got, "Description-Content-Type: text/markdown")
		mustContain(t, got, "\n\n# Hello")
	})

	t.Run("MIME parameters stripped from content type", func(t *testing.T) {
		t.Parallel()
		info := &PackageInfo{
			Name:                   "tool",
			Version:                "1.0.0",
			Description:            "hello",
			DescriptionContentType: "text/markdown; charset=UTF-8",
		}
		got := RenderAsMetadata(info)
		mustContain(t, got, "Description-Content-Type: text/markdown\n")
		mustNotContain(t, got, "charset")
	})

	t.Run("whitespace-only description is omitted", func(t *testing.T) {
		t.Parallel()
		info := &PackageInfo{Name: "tool", Version: "1.0.0", Description: "   \n  "}
		got := RenderAsMetadata(info)
		mustNotContain(t, got, "Description-Content-Type:")
	})

	t.Run("absent description defaults content type to text/plain", func(t *testing.T) {
		t.Parallel()
		info := &PackageInfo{
			Name:        "tool",
			Version:     "1.0.0",
			Description: "hello",
			// DescriptionContentType intentionally empty
		}
		got := RenderAsMetadata(info)
		mustContain(t, got, "Description-Content-Type: text/plain")
	})
}

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
					t.Errorf("[%d] = %q, want %q", i, got[i], w)
				}
			}
		})
	}
}

func TestBareMIMEType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"text/markdown", "text/markdown"},
		{"text/markdown; charset=UTF-8", "text/markdown"},
		{"text/markdown;charset=UTF-8", "text/markdown"},
		{"text/x-rst; charset=utf-8", "text/x-rst"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			if got := bareMIMEType(tt.input); got != tt.want {
				t.Errorf("bareMIMEType(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func mustContain(t *testing.T, text, substr string) {
	t.Helper()
	if !strings.Contains(text, substr) {
		t.Errorf("expected output to contain %q\ngot:\n%s", substr, text)
	}
}

func mustNotContain(t *testing.T, text, substr string) {
	t.Helper()
	if strings.Contains(text, substr) {
		t.Errorf("expected output NOT to contain %q\ngot:\n%s", substr, text)
	}
}
