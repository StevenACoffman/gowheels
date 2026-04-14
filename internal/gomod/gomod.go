// Package gomod extracts the repository URL from a go.mod module path.
package gomod

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RepoURL returns the HTTPS repository URL inferred from the go.mod module
// path found in modDir (use "." for the current directory). Returns "" when
// the module path does not begin with a known hosting domain (github.com,
// gitlab.com, codeberg.org) or when go.mod cannot be read.
func RepoURL(modDir string) string {
	if modDir == "" {
		modDir = "."
	}
	path, err := modulePath(filepath.Join(modDir, "go.mod"))
	if err != nil {
		return ""
	}
	return pathToURL(path)
}

func modulePath(filename string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", fmt.Errorf("opening go.mod: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			// Strip inline comments: `module foo/bar // comment`
			if i := strings.Index(rest, "//"); i != -1 {
				rest = rest[:i]
			}
			return strings.TrimSpace(rest), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("reading go.mod: %w", err)
	}
	return "", fmt.Errorf("no module directive in %s", filename)
}

// pathToURL converts a Go module path to a browseable HTTPS URL.
// It strips major-version suffixes (/v2, /v3, …) and only handles
// hosting domains where the URL structure is owner/repo.
func pathToURL(modPath string) string {
	// Strip major version suffix: e.g. github.com/foo/bar/v3 → github.com/foo/bar
	if i := strings.LastIndex(modPath, "/"); i != -1 {
		if isMajorVersionSuffix(modPath[i+1:]) {
			modPath = modPath[:i]
		}
	}

	for _, host := range []string{"github.com/", "gitlab.com/", "codeberg.org/"} {
		rest, ok := strings.CutPrefix(modPath, host)
		if !ok {
			continue
		}
		// Take only owner/repo — ignore deeper sub-paths.
		parts := strings.SplitN(rest, "/", 3)
		if len(parts) >= 2 && parts[0] != "" && parts[1] != "" {
			return "https://" + host + parts[0] + "/" + parts[1]
		}
	}
	return ""
}

// isMajorVersionSuffix returns true for "v2", "v3", …, "v99".
func isMajorVersionSuffix(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	for _, c := range s[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
