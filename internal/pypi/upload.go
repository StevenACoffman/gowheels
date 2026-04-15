package pypi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"golang.org/x/crypto/blake2b"

	"github.com/StevenACoffman/gowheels/internal/wheel"
)

const defaultPyPIURL = "https://upload.pypi.org/legacy/"

// Upload sends a built wheel to PyPI using the legacy multipart upload API.
// token is the value returned by MintToken (GOWHEELS_PYPI_TOKEN or OIDC-minted).
// pypiURL defaults to the public PyPI endpoint when empty.
//
// Digests follow what twine 5.x sends for Metadata-Version 2.4:
//   - sha2_digest       — SHA-256 hex (the field PyPI stores as the primary hash)
//   - blake2_256_digest — BLAKE2b-256 hex (required alongside sha2 for modern uploads)
//
// MD5 is omitted: PyPI deprecated it and modern warehouse validation ignores it.
func Upload(ctx context.Context, w wheel.BuiltWheel, token, pypiURL string) error {
	if pypiURL == "" {
		pypiURL = defaultPyPIURL
	}

	sha2Sum := sha256.Sum256(w.Data)
	blake2Sum, err := blake2b256(w.Data)
	if err != nil {
		return fmt.Errorf("computing blake2b-256 digest: %w", err)
	}

	pkgName, _, err := parseWheelFilename(w.Filename)
	if err != nil {
		return err
	}

	body, contentType, err := buildUploadForm(w, sha2Sum[:], blake2Sum)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pypiURL, body)
	if err != nil {
		return fmt.Errorf("building upload request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.SetBasicAuth("__token__", token)

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending upload request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		return nil
	}
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	msg := uploadError(resp.StatusCode, pkgName)
	if detail := strings.TrimSpace(string(respBody)); detail != "" {
		return fmt.Errorf("%s\nPyPI response: %s", msg, detail)
	}
	return errors.New(msg)
}

// buildUploadForm constructs the multipart body for a PyPI legacy upload.
// It returns the body buffer, content-type header value, and any error.
func buildUploadForm(w wheel.BuiltWheel, sha2, blake2 []byte) (*bytes.Buffer, string, error) {
	pkgName, version, err := parseWheelFilename(w.Filename)
	if err != nil {
		return nil, "", err
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
		// sha2_digest is the canonical field name PyPI stores for SHA-256.
		// blake2_256_digest is required for Metadata-Version 2.4 uploads.
		{"sha2_digest", hex.EncodeToString(sha2)},
		{"blake2_256_digest", hex.EncodeToString(blake2)},
	}
	for _, f := range fields {
		if err := mw.WriteField(f[0], f[1]); err != nil {
			return nil, "", fmt.Errorf("building upload form: %w", err)
		}
	}

	fw, err := mw.CreateFormFile("content", w.Filename)
	if err != nil {
		return nil, "", fmt.Errorf("creating file form field: %w", err)
	}
	if _, err := io.Copy(fw, bytes.NewReader(w.Data)); err != nil {
		return nil, "", fmt.Errorf("writing wheel data: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, "", fmt.Errorf("closing multipart writer: %w", err)
	}

	return &body, mw.FormDataContentType(), nil
}

// blake2b256 returns the BLAKE2b-256 digest of data.
func blake2b256(data []byte) ([]byte, error) {
	h, err := blake2b.New256(nil)
	if err != nil {
		return nil, fmt.Errorf("creating blake2b-256 hash: %w", err)
	}
	h.Write(data)
	return h.Sum(nil), nil
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

func uploadError(status int, pkgName string) string {
	switch status {
	case http.StatusUnauthorized:
		return "not authenticated: check that GOWHEELS_PYPI_TOKEN is set to a valid PyPI API token\n" +
			"  (tokens start with 'pypi-'; create one at https://pypi.org/manage/account/token/)"
	case http.StatusForbidden:
		return fmt.Sprintf(
			"permission denied uploading %q to PyPI\n"+
				"  1. name taken?  check https://pypi.org/project/%s/ — if owned by someone else, choose a different name\n"+
				"  2. token scope? project-scoped tokens only work for their specific project;\n"+
				"                  use an account-wide token or create one scoped to %q\n"+
				"  3. first upload? the project must not already exist under a different owner",
			pkgName, pkgName, pkgName,
		)
	case http.StatusConflict:
		return fmt.Sprintf(
			"version already exists: %q has already been published at this version\n"+
				"  PyPI does not allow re-uploading the same version, even to fix missing metadata\n"+
				"  (summary, description/README, keywords cannot be updated in place)\n"+
				"  options: delete the release and re-upload, or bump the version\n"+
				"  manage releases at https://pypi.org/manage/project/%s/releases/",
			pkgName,
			pkgName,
		)
	case http.StatusUnprocessableEntity:
		return "invalid wheel: PyPI rejected the upload — the wheel filename, metadata, or a digest field failed validation\n" +
			"  see 'PyPI response' above for the specific rejection reason"
	case http.StatusBadRequest:
		return "invalid package: PyPI rejected the upload — check metadata fields and version format\n" +
			"  run with --debug to see full request details"
	default:
		return fmt.Sprintf("unexpected status %d from PyPI", status)
	}
}
