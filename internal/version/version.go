// Package version normalizes version strings to PEP 440.
package version

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// semverPreRe matches a semver pre-release like "1.2.3-alpha.2" or "1.2.3-beta".
var semverPreRe = regexp.MustCompile(`^(\d+\.\d+\.\d+)-(alpha|beta|rc|dev)(?:\.(\d+))?$`)

// pep440Re validates a PEP 440 version string with at least three numeric components.
var pep440Re = regexp.MustCompile(`^\d+\.\d+\.\d+(\.\d+)*((a|b|rc)\d+)?(\.post\d+)?(\.dev\d+)?$`)

var pep440Labels = map[string]string{
	"alpha": "a",
	"beta":  "b",
	"rc":    "rc",
	"dev":   ".dev",
}

// Normalize converts a version string in either semver or PEP 440 format to a
// PEP 440-compatible string. A leading "v" prefix is stripped first.
//
// Accepted forms:
//   - Semver pre-release:   v1.2.3-alpha.1  →  1.2.3a1
//   - Semver pre-release:   v1.2.3-beta     →  1.2.3b0
//   - PEP 440 direct:       v1.2.3          →  1.2.3
//   - PEP 440 pre-release:  1.2.3a1, 1.2.3.dev0, etc.
func Normalize(v string) (string, error) {
	v = strings.TrimPrefix(v, "v")

	// Semver pre-release path: detected by hyphen after n.n.n
	if m := semverPreRe.FindStringSubmatch(v); m != nil {
		base := m[1]
		label := m[2]
		num := m[3]
		if num == "" {
			num = "0"
		}
		pep, ok := pep440Labels[label]
		if !ok {
			return "", fmt.Errorf(
				"unknown semver pre-release label %q: accepted labels are alpha, beta, rc, dev",
				label,
			)
		}
		return base + pep + num, nil
	}

	// PEP 440 direct path
	if !pep440Re.MatchString(v) {
		return "", fmt.Errorf(
			"version %q is not valid semver pre-release or PEP 440\n"+
				"  semver examples: v1.2.3, v1.2.3-alpha.1, v1.2.3-beta.2\n"+
				"  PEP 440 examples: 1.2.3, 1.2.3a1, 1.2.3b2, 1.2.3rc1, 1.2.3.dev0",
			v,
		)
	}
	return v, nil
}

// Resolve returns a normalized PEP 440 version from an explicit string or,
// when explicit is empty, from the current git tag via
// `git describe --tags --exact-match`.
func Resolve(ctx context.Context, explicit string) (string, error) {
	if explicit != "" {
		return Normalize(explicit)
	}
	out, err := exec.CommandContext(ctx, "git", "describe", "--tags", "--exact-match").Output()
	if err != nil {
		return "", fmt.Errorf(
			"--version not provided and no exact git tag found\n"+
				"  (git describe: %w)\n"+
				"  pass --version explicitly to continue",
			err,
		)
	}
	tag := strings.TrimSpace(string(out))
	if tag == "" {
		return "", errors.New("--version not provided and git describe returned empty output")
	}
	return Normalize(tag)
}
