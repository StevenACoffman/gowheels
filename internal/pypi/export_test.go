package pypi

import "net/http"

// SetHTTPClient replaces the package-level HTTP client for the duration of a
// test and returns a restore function. Intended for use by upload_test.go
// (package pypi_test) which cannot access unexported package state directly.
func SetHTTPClient(c *http.Client) (restore func()) {
	orig := httpClient
	httpClient = c
	return func() { httpClient = orig }
}
