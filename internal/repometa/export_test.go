package repometa

import "net/http"

// SetHTTPClient replaces the package-level HTTP client for testing and returns
// a function that restores the original.
func SetHTTPClient(c *http.Client) (restore func()) {
	orig := httpClient
	httpClient = c
	return func() { httpClient = orig }
}

// SetAPIBase overrides the GitHub API base URL for testing and returns a
// function that restores the original.
func SetAPIBase(base string) (restore func()) {
	orig := apiBase
	apiBase = base
	return func() { apiBase = orig }
}
