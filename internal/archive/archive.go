// Package archive extracts binaries from tar.gz and zip archives.
package archive

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"path/filepath"
)

// ExtractBinary extracts the file named binaryName from a tar.gz or zip
// archive. archiveExt must be "tar.gz" or "zip".
func ExtractBinary(data []byte, archiveExt, binaryName string) ([]byte, error) {
	switch archiveExt {
	case "tar.gz":
		return extractFromTarGz(data, binaryName)
	case "zip":
		return extractFromZip(data, binaryName)
	default:
		return nil, fmt.Errorf("unsupported archive extension: %q", archiveExt)
	}
}

func extractFromTarGz(data []byte, name string) ([]byte, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		if filepath.Base(hdr.Name) == name {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("binary %q not found in tar.gz archive", name)
}

func extractFromZip(data []byte, name string) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("zip: %w", err)
	}
	for _, f := range r.File {
		if filepath.Base(f.Name) == name {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("zip entry %s: %w", f.Name, err)
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("binary %q not found in zip archive", name)
}
