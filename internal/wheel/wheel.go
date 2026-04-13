// Package wheel builds PEP 427 .whl files from resolved Go binaries.
package wheel

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	_ "embed"
	"errors"
	"fmt"
	"hash/crc32"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/StevenACoffman/gowheels/internal/source"
)

//go:embed shim.py
var shimTemplate string

// Config parameterises a wheel build.
type Config struct {
	// Package identity
	RawName     string // binary name as supplied (e.g. "my-tool")
	PackageName string // Python package name; defaults to NormalizeName(RawName)
	EntryPoint  string // console_scripts key; defaults to RawName

	// Metadata
	Version     string // PEP 440, already normalised
	Summary     string
	URL         string
	LicenseExpr string // SPDX expression, e.g. "MIT"
	LicensePath string // local license file; empty → no license bundled
	ReadmePath  string // explicit readme; empty → auto-detect; "-" → none

	// Output
	OutputDir string // defaults to "dist"
}

// BuiltWheel represents a .whl file that was created on disk and whose bytes
// are held in memory for the optional upload step.
type BuiltWheel struct {
	Filename string
	Data     []byte
}

var normalizeRe = regexp.MustCompile(`[-._]+`)

// spdxPermitted matches the complete set of characters allowed in an SPDX
// expression: letters, digits, hyphens, dots, +, spaces, and parentheses.
var spdxPermitted = regexp.MustCompile(`^[a-zA-Z0-9\-\.+() ]+$`)

// spdxLowerOp detects lowercase boolean operators (and/or/with) as whole words,
// which are invalid in SPDX — they must be uppercase (AND, OR, WITH).
var spdxLowerOp = regexp.MustCompile(`\b(and|or|with)\b`)

// ValidateLicenseExpression reports whether expr is a well-formed SPDX
// expression for use in the Metadata-Version 2.4 License-Expression field.
//
// It validates the character set and operator casing. It does not verify that
// individual identifiers exist in the SPDX license list — pass a clearly
// incorrect value like "Apache 2.0" (instead of "Apache-2.0") and this will
// not catch the wrong identifier, but it will catch malformed expressions such
// as those containing commas, slashes, underscores, or lowercase operators.
func ValidateLicenseExpression(expr string) error {
	if expr == "" {
		return errors.New("License-Expression must not be empty")
	}
	if expr != strings.TrimSpace(expr) {
		return errors.New("License-Expression must not have leading or trailing whitespace")
	}
	if !spdxPermitted.MatchString(expr) {
		return fmt.Errorf(
			"License-Expression %q contains characters not permitted in SPDX expressions "+
				"(allowed: letters, digits, -, ., +, spaces, parentheses)",
			expr,
		)
	}
	if loc := spdxLowerOp.FindStringIndex(expr); loc != nil {
		op := expr[loc[0]:loc[1]]
		return fmt.Errorf(
			"License-Expression %q: SPDX operator %q must be uppercase (%s)",
			expr, op, strings.ToUpper(op),
		)
	}
	return nil
}

// NormalizeName applies PEP 625 / PEP 427 name normalisation: lowercase and
// run of [-._]+ replaced by a single underscore.
func NormalizeName(name string) string {
	return strings.ToLower(normalizeRe.ReplaceAllString(name, "_"))
}

// BuildAll builds one wheel per WheelTag across all binaries. A Linux binary
// with two wheel tags (manylinux + musllinux) produces two wheels from the
// same bytes without re-reading the source.
func BuildAll(cfg Config, binaries []source.Binary) ([]BuiltWheel, error) {
	// Validate Metadata-Version 2.4 fields before doing any filesystem work.
	if cfg.LicenseExpr != "" {
		if err := ValidateLicenseExpression(cfg.LicenseExpr); err != nil {
			return nil, fmt.Errorf("--license-expr: %w", err)
		}
	}
	if strings.ContainsAny(cfg.Summary, "\r\n") {
		return nil, errors.New("--summary: must be a single line (Metadata-Version 2.4 §2.1.5)")
	}

	if cfg.OutputDir == "" {
		cfg.OutputDir = "dist"
	}
	if err := os.MkdirAll(cfg.OutputDir, 0o750); err != nil {
		return nil, fmt.Errorf("creating output dir: %w", err)
	}

	normName := NormalizeName(cfg.RawName)
	if cfg.PackageName != "" {
		normName = NormalizeName(cfg.PackageName)
	}
	entryPoint := cfg.RawName
	if cfg.EntryPoint != "" {
		entryPoint = cfg.EntryPoint
	}

	// Read optional files once, shared across all wheels.
	readmeContent, readmeContentType := resolveReadme(cfg.ReadmePath)

	var licenseData []byte
	if cfg.LicensePath != "" {
		data, err := os.ReadFile(cfg.LicensePath)
		if err != nil {
			return nil, fmt.Errorf("reading license file: %w", err)
		}
		licenseData = data
	}

	metadata := buildMetadata(normName, cfg.Version, cfg.Summary, cfg.URL,
		cfg.LicenseExpr, readmeContent, readmeContentType, licenseData != nil)

	var built []BuiltWheel
	for _, bin := range binaries {
		for _, tag := range bin.Platform.WheelTags {
			w, err := buildWheel(wheelParams{
				normName:    normName,
				rawName:     cfg.RawName,
				entryPoint:  entryPoint,
				version:     cfg.Version,
				tag:         tag,
				metadata:    metadata,
				licenseData: licenseData,
				binary:      bin,
				outputDir:   cfg.OutputDir,
			})
			if err != nil {
				return nil, fmt.Errorf("wheel %s/%s tag %s: %w",
					bin.Platform.GOOS, bin.Platform.GOARCH, tag, err)
			}
			built = append(built, w)
		}
	}
	return built, nil
}

type wheelParams struct {
	normName    string
	rawName     string
	entryPoint  string
	version     string
	tag         string
	metadata    string
	licenseData []byte
	binary      source.Binary
	outputDir   string
}

func buildWheel(p wheelParams) (BuiltWheel, error) {
	distInfo := fmt.Sprintf("%s-%s.dist-info", p.normName, p.version)

	// Render shim: replace __BIN_NAME__ sentinel with the actual binary name.
	shim := strings.ReplaceAll(shimTemplate, "__BIN_NAME__", p.rawName)

	files := map[string][]byte{
		p.normName + "/__init__.py":              []byte(shim),
		p.normName + "/__main__.py":              []byte("from . import main; main()\n"),
		p.normName + "/bin/" + p.binary.Filename: p.binary.Data,
		distInfo + "/METADATA":                   []byte(p.metadata),
		distInfo + "/WHEEL":                      []byte(buildWheelMeta(p.tag)),
		distInfo + "/entry_points.txt":            []byte(fmt.Sprintf("[console_scripts]\n%s = %s:main\n", p.entryPoint, p.normName)),
	}
	if p.licenseData != nil {
		files[distInfo+"/licenses/LICENSE.txt"] = p.licenseData
	}

	return buildZip(files, p.normName, p.version, p.tag, p.outputDir)
}

// buildZip constructs the wheel zip archive.
//
// Design decisions:
//   - zip.Store (no compression) via CreateRaw with pre-computed CRC32 and
//     both 32/64-bit size fields, Flags=0 — suppresses data-descriptor bits
//     that PyPI's legacy upload endpoint and some uv versions reject.
//   - RECORD entries written in alphabetical order (slices.Sorted(maps.Keys))
//     for deterministic archives.
//   - RECORD entry itself appended last with empty hash/size (,,).
func buildZip(files map[string][]byte, normName, version, tag, outputDir string) (BuiltWheel, error) {
	distInfo := fmt.Sprintf("%s-%s.dist-info", normName, version)
	recordPath := distInfo + "/RECORD"

	// Sort keys once; used for both RECORD generation and zip entry ordering.
	sortedPaths := slices.Sorted(maps.Keys(files))

	// Build RECORD (alphabetical order over all non-RECORD entries).
	var record strings.Builder
	for _, path := range sortedPaths {
		fmt.Fprintf(&record, "%s,%s,%d\n", path, sha256Digest(files[path]), len(files[path]))
	}
	fmt.Fprintf(&record, "%s,,\n", recordPath)
	files[recordPath] = []byte(record.String())

	whlName := fmt.Sprintf("%s-%s-py3-none-%s.whl", normName, version, tag)

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	writeEntry := func(path string, data []byte) error {
		header := &zip.FileHeader{
			Name:               path,
			Method:             zip.Store,
			CRC32:              crc32.ChecksumIEEE(data),
			CompressedSize64:   uint64(len(data)),
			UncompressedSize64: uint64(len(data)),
			// Flags=0 (default): no data-descriptor bit; sizes/CRC in local header.
		}
		if strings.Contains(path, "/bin/") {
			header.SetMode(0o755)
		} else {
			header.SetMode(0o644)
		}
		w, err := zw.CreateRaw(header)
		if err != nil {
			return fmt.Errorf("zip entry %s: %w", path, err)
		}
		if _, err := w.Write(data); err != nil {
			return fmt.Errorf("writing zip entry %s: %w", path, err)
		}
		return nil
	}

	// Write all entries except RECORD in alphabetical order, then RECORD last.
	for _, path := range sortedPaths {
		if path == recordPath {
			continue
		}
		if err := writeEntry(path, files[path]); err != nil {
			return BuiltWheel{}, err
		}
	}
	if err := writeEntry(recordPath, files[recordPath]); err != nil {
		return BuiltWheel{}, err
	}

	if err := zw.Close(); err != nil {
		return BuiltWheel{}, fmt.Errorf("finalising wheel: %w", err)
	}

	whlPath := filepath.Join(outputDir, whlName)
	if err := os.WriteFile(whlPath, buf.Bytes(), 0o644); err != nil {
		return BuiltWheel{}, fmt.Errorf("writing wheel file: %w", err)
	}

	return BuiltWheel{Filename: whlName, Data: buf.Bytes()}, nil
}

func buildMetadata(name, version, summary, url, licenseExpr, readme, readmeContentType string, hasLicense bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Metadata-Version: 2.4\n")
	fmt.Fprintf(&b, "Name: %s\n", name)
	fmt.Fprintf(&b, "Version: %s\n", version)
	if summary != "" {
		fmt.Fprintf(&b, "Summary: %s\n", summary)
	}
	if url != "" {
		fmt.Fprintf(&b, "Project-URL: Repository, %s\n", url)
	}
	if licenseExpr != "" {
		fmt.Fprintf(&b, "License-Expression: %s\n", licenseExpr)
	}
	if hasLicense {
		fmt.Fprintf(&b, "License-File: licenses/LICENSE.txt\n")
	}
	fmt.Fprintf(&b, "Requires-Python: >=3.9\n")
	if readme != "" {
		fmt.Fprintf(&b, "Description-Content-Type: %s\n", readmeContentType)
		fmt.Fprintf(&b, "\n%s", readme)
	}
	return b.String()
}

func buildWheelMeta(tag string) string {
	return fmt.Sprintf("Wheel-Version: 1.0\nGenerator: gowheels\nRoot-Is-Purelib: false\nTag: py3-none-%s\n", tag)
}

func sha256Digest(data []byte) string {
	h := sha256.Sum256(data)
	return "sha256=" + base64.RawURLEncoding.EncodeToString(h[:])
}

// resolveReadme reads the readme at path (auto-detects common names when
// path is empty; skips when path is "-").
func resolveReadme(path string) (content, contentType string) {
	if path == "-" {
		return "", ""
	}
	if path == "" {
		for _, name := range []string{"README.md", "README.rst", "README.txt", "README"} {
			if data, err := os.ReadFile(name); err == nil {
				return string(data), detectContentType(name)
			}
		}
		return "", ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	return string(data), detectContentType(path)
}

func detectContentType(path string) string {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".md"), strings.HasSuffix(lower, ".markdown"):
		return "text/markdown"
	case strings.HasSuffix(lower, ".rst"):
		return "text/x-rst"
	default:
		return "text/plain"
	}
}
