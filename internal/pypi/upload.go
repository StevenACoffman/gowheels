package pypi

import (
	"bytes"
	"context"
	"crypto/md5"  //nolint:gosec // MD5 is required by the PyPI legacy upload API
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/StevenACoffman/gowheels/internal/wheel"
)

const defaultPyPIURL = "https://upload.pypi.org/legacy/"

// Upload sends a built wheel to PyPI using the legacy multipart upload API.
// token is the value returned by MintToken (GOWHEELS_PYPI_TOKEN or OIDC-minted).
// pypiURL defaults to the public PyPI endpoint when empty.
func Upload(ctx context.Context, w wheel.BuiltWheel, token, pypiURL string) error {
	if pypiURL == "" {
		pypiURL = defaultPyPIURL
	}

	sha256Sum := sha256.Sum256(w.Data)
	md5Sum := md5.Sum(w.Data) //nolint:gosec

	pkgName, version, err := parseWheelFilename(w.Filename)
	if err != nil {
		return err
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)

	fields := [][2]string{
		{":action", "file_upload"},
		{"metadata_version", "2.4"},
		{"name", pkgName},
		{"version", version},
		{"filetype", "bdist_wheel"},
		{"pyversion", "py3"},
		{"protocol_version", "1"},
		{"requires_python", ">=3.9"},
		// Send both digest field names for maximum compatibility:
		// sha256_digest is the modern spelling; md5_digest for legacy servers.
		{"sha256_digest", hex.EncodeToString(sha256Sum[:])},
		{"md5_digest", hex.EncodeToString(md5Sum[:])},
	}
	for _, f := range fields {
		if err := mw.WriteField(f[0], f[1]); err != nil {
			return fmt.Errorf("building upload form: %w", err)
		}
	}

	fw, err := mw.CreateFormFile("content", w.Filename)
	if err != nil {
		return err
	}
	if _, err := io.Copy(fw, bytes.NewReader(w.Data)); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pypiURL, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.SetBasicAuth("__token__", token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		return nil
	}
	return errors.New(uploadError(resp.StatusCode))
}

// parseWheelFilename extracts package name and version from a wheel filename
// of the form {name}-{version}-py3-none-{tag}.whl.
func parseWheelFilename(filename string) (name, version string, err error) {
	base := strings.TrimSuffix(filename, ".whl")
	parts := strings.SplitN(base, "-", 3)
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid wheel filename: %q", filename)
	}
	return parts[0], parts[1], nil
}

func uploadError(status int) string {
	switch status {
	case http.StatusUnauthorized:
		return "not authenticated: ensure your API token is valid"
	case http.StatusForbidden:
		return "permission denied: the package name may be taken or your token lacks write access"
	case http.StatusConflict:
		return "version already exists: this version has already been published"
	case http.StatusBadRequest:
		return "invalid package: check your metadata and version format"
	default:
		return fmt.Sprintf("unexpected status %d from PyPI", status)
	}
}
