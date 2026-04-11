package wheel_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/StevenACoffman/gowheels/internal/platforms"
	"github.com/StevenACoffman/gowheels/internal/source"
	"github.com/StevenACoffman/gowheels/internal/wheel"
)

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"mytool", "mytool"},
		{"my-tool", "my_tool"},
		{"my_tool", "my_tool"},
		{"my.tool", "my_tool"},
		{"My-Tool", "my_tool"},
		{"my---tool", "my_tool"},
		{"my..tool", "my_tool"},
		{"my-.tool", "my_tool"},
	}
	for _, tt := range tests {
		got := wheel.NormalizeName(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildAll(t *testing.T) {
	outDir := t.TempDir()

	plat, _ := platforms.Lookup("linux", "amd64")
	binaries := []source.Binary{
		{
			Platform: plat,
			Data:     []byte("fake-binary-data"),
			Filename: "mytool",
		},
	}

	cfg := wheel.Config{
		RawName:     "mytool",
		Version:     "1.2.3",
		LicenseExpr: "MIT",
		ReadmePath:  "-", // disable auto-detect
		OutputDir:   outDir,
	}

	wheels, err := wheel.BuildAll(cfg, binaries)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}

	// linux/amd64 → 2 wheel tags → 2 wheels
	if len(wheels) != 2 {
		t.Fatalf("got %d wheels, want 2 (manylinux + musllinux)", len(wheels))
	}

	for _, w := range wheels {
		t.Run(w.Filename, func(t *testing.T) {
			// File must exist on disk
			path := filepath.Join(outDir, w.Filename)
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("wheel file not on disk: %v", err)
			}

			// Filename uses py3 (not py30)
			if !strings.Contains(w.Filename, "-py3-none-") {
				t.Errorf("filename %q does not contain -py3-none-", w.Filename)
			}

			checkWheelContents(t, w)
		})
	}
}

func TestBuildAll_CustomPackageAndEntryPoint(t *testing.T) {
	outDir := t.TempDir()

	plat, _ := platforms.Lookup("darwin", "arm64")
	binaries := []source.Binary{
		{Platform: plat, Data: []byte("bin"), Filename: "my-tool"},
	}

	cfg := wheel.Config{
		RawName:     "my-tool",
		PackageName: "my_tool_pkg",
		EntryPoint:  "mytool",
		Version:     "0.1.0",
		ReadmePath:  "-",
		OutputDir:   outDir,
	}

	wheels, err := wheel.BuildAll(cfg, binaries)
	if err != nil {
		t.Fatalf("BuildAll: %v", err)
	}
	if len(wheels) != 1 {
		t.Fatalf("got %d wheels, want 1", len(wheels))
	}

	w := wheels[0]
	if !strings.HasPrefix(w.Filename, "my_tool_pkg-") {
		t.Errorf("filename %q should start with my_tool_pkg-", w.Filename)
	}

	zr, _ := zip.NewReader(bytes.NewReader(w.Data), int64(len(w.Data)))
	entry := readZipFile(zr, "my_tool_pkg-0.1.0.dist-info/entry_points.txt")
	if !strings.Contains(entry, "mytool = my_tool_pkg:main") {
		t.Errorf("entry_points.txt = %q, want mytool = my_tool_pkg:main", entry)
	}
}

// checkWheelContents validates the structural invariants of a built wheel.
func checkWheelContents(t *testing.T, w wheel.BuiltWheel) {
	t.Helper()

	zr, err := zip.NewReader(bytes.NewReader(w.Data), int64(len(w.Data)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}

	// All entries must use Store (method=0) with flags=0.
	for _, f := range zr.File {
		if f.Method != zip.Store {
			t.Errorf("entry %q uses method %d, want Store (0)", f.Name, f.Method)
		}
		if f.Flags != 0 {
			t.Errorf("entry %q has flags %#x, want 0 (no data descriptor)", f.Name, f.Flags)
		}
	}

	// Required files must be present.
	names := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		names = append(names, f.Name)
	}

	required := []string{"__init__.py", "__main__.py", "METADATA", "WHEEL", "entry_points.txt", "RECORD"}
	for _, req := range required {
		found := slices.ContainsFunc(names, func(n string) bool { return strings.HasSuffix(n, req) })
		if !found {
			t.Errorf("wheel is missing required entry ending in %q", req)
		}
	}

	// Binary must be in bin/ subdirectory.
	hasBin := slices.ContainsFunc(names, func(n string) bool { return strings.Contains(n, "/bin/") })
	if !hasBin {
		t.Errorf("wheel has no entry in bin/ subdirectory")
	}

	// Binary entry must have mode 0o755.
	for _, f := range zr.File {
		if strings.Contains(f.Name, "/bin/") {
			mode := f.Mode()
			if mode&0o111 == 0 {
				t.Errorf("binary %q has mode %04o, want executable bit set", f.Name, mode)
			}
		}
	}

	// WHEEL file must contain correct fields.
	wheelContent := readZipFile(zr, "WHEEL")
	for _, want := range []string{
		"Wheel-Version: 1.0",
		"Generator: gowheels",
		"Root-Is-Purelib: false",
		"Tag: py3-none-",
	} {
		if !strings.Contains(wheelContent, want) {
			t.Errorf("WHEEL file missing %q\nContent:\n%s", want, wheelContent)
		}
	}

	// METADATA must use Metadata-Version 2.4.
	metadata := readZipFile(zr, "METADATA")
	if !strings.Contains(metadata, "Metadata-Version: 2.4") {
		t.Errorf("METADATA missing Metadata-Version: 2.4\nContent:\n%s", metadata)
	}

	// RECORD must be last entry and have empty hash/size.
	last := zr.File[len(zr.File)-1]
	if !strings.HasSuffix(last.Name, "/RECORD") {
		t.Errorf("last entry is %q, want RECORD", last.Name)
	}
	record := readZipFile(zr, "RECORD")
	if !strings.HasSuffix(strings.TrimRight(record, "\n"), ",,") {
		t.Errorf("RECORD last line should end with ,, (empty hash/size)\nContent:\n%s", record)
	}

	// RECORD entries must be in alphabetical order (except the trailing RECORD line).
	checkRecordOrder(t, record)

	// RECORD hashes must be correct.
	checkRecordHashes(t, zr, record)

	// __init__.py must not contain the __BIN_NAME__ sentinel.
	init_ := readZipFile(zr, "__init__.py")
	if strings.Contains(init_, "__BIN_NAME__") {
		t.Errorf("__init__.py still contains unreplaced __BIN_NAME__ sentinel")
	}
}

func checkRecordOrder(t *testing.T, record string) {
	t.Helper()
	lines := strings.Split(strings.TrimRight(record, "\n"), "\n")
	// Last line is RECORD itself (empty hash); skip it.
	if len(lines) < 2 {
		return
	}
	dataLines := lines[:len(lines)-1]
	for i := 1; i < len(dataLines); i++ {
		prev := strings.SplitN(dataLines[i-1], ",", 2)[0]
		curr := strings.SplitN(dataLines[i], ",", 2)[0]
		if curr < prev {
			t.Errorf("RECORD not in alphabetical order: %q before %q", prev, curr)
		}
	}
}

func checkRecordHashes(t *testing.T, zr *zip.Reader, record string) {
	t.Helper()
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "/RECORD") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("opening %s: %v", f.Name, err)
		}
		var buf bytes.Buffer
		buf.ReadFrom(rc)
		rc.Close()

		h := sha256.Sum256(buf.Bytes())
		want := "sha256=" + base64.RawURLEncoding.EncodeToString(h[:])

		found := false
		for _, line := range strings.Split(record, "\n") {
			if strings.HasPrefix(line, f.Name+",") {
				found = true
				if !strings.Contains(line, want) {
					t.Errorf("RECORD hash for %q mismatch\n  got  %s\n  want entry containing %s", f.Name, line, want)
				}
				break
			}
		}
		if !found {
			t.Errorf("RECORD has no entry for %q", f.Name)
		}
	}
}

func readZipFile(zr *zip.Reader, suffix string) string {
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, suffix) {
			rc, err := f.Open()
			if err != nil {
				return ""
			}
			defer rc.Close()
			var buf bytes.Buffer
			buf.ReadFrom(rc)
			return buf.String()
		}
	}
	return ""
}
