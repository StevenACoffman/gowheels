package pypi

import (
	"fmt"
	"slices"
	"strings"

	"github.com/StevenACoffman/gowheels/internal/wheel"
)

// osClassifierPrefix is the trove classifier prefix for operating-system
// entries. These are excluded from RenderAsMetadata because they depend on
// which binary platforms were uploaded, which callers diffing against local
// metadata cannot know in advance.
const osClassifierPrefix = "Operating System ::"

// RenderAsMetadata converts a PackageInfo into the RFC 822 METADATA text
// format that "gowheels pypi" writes into wheels. It is used by the
// "pypidiff" command and by the pre-upload check in "pypi --upload" to
// produce a comparable text for unified diff.
//
// Field emission order matches buildMetadata in internal/wheel so that
// equivalent content produces no diff lines due to reordering.
//
// Three normalizations are applied to make remote metadata comparable to
// locally-generated metadata:
//   - OS-specific classifiers are excluded (platform-dependent; not known without binaries).
//   - Project-URLs are sorted alphabetically (wheel.BuildMetadataText does the same).
//   - Description-Content-Type is stripped of MIME parameters such as
//     "; charset=UTF-8" that PyPI may have stored from an earlier upload.
func RenderAsMetadata(info *PackageInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Metadata-Version: 2.4\n")
	fmt.Fprintf(&b, "Name: %s\n", wheel.NormalizeName(info.Name))
	fmt.Fprintf(&b, "Version: %s\n", info.Version)
	if info.Summary != "" {
		fmt.Fprintf(&b, "Summary: %s\n", info.Summary)
	}
	if info.Keywords != "" {
		fmt.Fprintf(&b, "Keywords: %s\n", info.Keywords)
	}
	for _, c := range filterOSClassifiers(info.Classifiers) {
		fmt.Fprintf(&b, "Classifier: %s\n", c)
	}
	// Sort Project-URL keys alphabetically to match the sort applied by
	// wheel.BuildMetadataText on the local side.
	urlKeys := make([]string, 0, len(info.ProjectURLs))
	for k := range info.ProjectURLs {
		urlKeys = append(urlKeys, k)
	}
	slices.Sort(urlKeys)
	for _, k := range urlKeys {
		fmt.Fprintf(&b, "Project-URL: %s, %s\n", k, info.ProjectURLs[k])
	}
	if info.LicenseExpression != "" {
		fmt.Fprintf(&b, "License-Expression: %s\n", info.LicenseExpression)
	}
	if info.RequiresPython != "" {
		fmt.Fprintf(&b, "Requires-Python: %s\n", info.RequiresPython)
	}
	if strings.TrimSpace(info.Description) != "" {
		ct := bareMIMEType(info.DescriptionContentType)
		if ct == "" {
			ct = "text/plain"
		}
		fmt.Fprintf(&b, "Description-Content-Type: %s\n", ct)
		fmt.Fprintf(&b, "\n%s", info.Description)
	}
	return b.String()
}

// filterOSClassifiers returns a sorted copy of c with all "Operating System ::"
// entries removed.
func filterOSClassifiers(c []string) []string {
	out := make([]string, 0, len(c))
	for _, cl := range c {
		if !strings.HasPrefix(cl, osClassifierPrefix) {
			out = append(out, cl)
		}
	}
	slices.Sort(out)
	return out
}

// bareMIMEType returns the bare MIME type from a Content-Type value, stripping
// any parameters (e.g. "text/markdown; charset=UTF-8" → "text/markdown").
func bareMIMEType(ct string) string {
	if idx := strings.IndexByte(ct, ';'); idx >= 0 {
		return strings.TrimSpace(ct[:idx])
	}
	return ct
}
