// Package pypi handles PyPI authentication and wheel upload.
package pypi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

var httpClient = &http.Client{Timeout: 30 * time.Second}

const pypiMintTokenURL = "https://upload.pypi.org/_/oidc/mint-token"

// MintToken returns a PyPI upload token using the following priority:
//
//  1. pypiToken argument — returned immediately if non-empty (set via --pypi-token flag).
//     Explicit user credential always takes precedence over ambient OIDC.
//  2. GitHub Actions OIDC — if ACTIONS_ID_TOKEN_REQUEST_URL and
//     ACTIONS_ID_TOKEN_REQUEST_TOKEN are set, requests an OIDC JWT with
//     audience=pypi and exchanges it for a short-lived upload token at
//     PyPI's /_/oidc/mint-token endpoint.
//  3. Neither present — structured error with remediation instructions.
func MintToken(ctx context.Context, pypiToken string) (string, error) {
	if pypiToken != "" {
		return pypiToken, nil
	}

	requestURL := os.Getenv("ACTIONS_ID_TOKEN_REQUEST_URL")
	requestToken := os.Getenv("ACTIONS_ID_TOKEN_REQUEST_TOKEN")

	if requestURL == "" || requestToken == "" {
		return "", fmt.Errorf(
			"gowheels: no PyPI credentials found\n" +
				"  option 1: set PYPI_TOKEN to a PyPI API token\n" +
				"  option 2: add 'id-token: write' to your workflow permissions and\n" +
				"            configure a trusted publisher at https://pypi.org/manage/account/publishing/",
		)
	}

	oidcToken, err := requestOIDCToken(ctx, requestURL, requestToken)
	if err != nil {
		return "", fmt.Errorf("requesting OIDC token: %w", err)
	}

	uploadToken, err := exchangeForUploadToken(ctx, oidcToken)
	if err != nil {
		return "", fmt.Errorf("minting PyPI upload token: %w", err)
	}

	return uploadToken, nil
}

func requestOIDCToken(ctx context.Context, requestURL, requestToken string) (string, error) {
	u, err := url.Parse(requestURL)
	if err != nil {
		return "", fmt.Errorf("invalid OIDC request URL: %w", err)
	}
	q := u.Query()
	q.Set("audience", "pypi")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+requestToken)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("OIDC endpoint returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Value string `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Value == "" {
		return "", fmt.Errorf("OIDC token response was empty")
	}
	return result.Value, nil
}

func exchangeForUploadToken(ctx context.Context, oidcToken string) (string, error) {
	payload, err := json.Marshal(struct {
		Token string `json:"token"`
	}{Token: oidcToken})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pypiMintTokenURL, bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		var apiErr struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(raw, &apiErr) == nil && apiErr.Message != "" {
			if resp.StatusCode == http.StatusNotFound {
				return "", fmt.Errorf(
					"no trusted publisher found for this workflow: %s\n"+
						"register one at https://pypi.org/manage/account/publishing/",
					apiErr.Message,
				)
			}
			return "", fmt.Errorf("PyPI mint-token returned %d: %s", resp.StatusCode, apiErr.Message)
		}
		return "", fmt.Errorf("PyPI mint-token returned %d: %s", resp.StatusCode, string(raw))
	}

	var result struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if result.Token == "" {
		return "", fmt.Errorf("PyPI returned empty upload token")
	}
	return result.Token, nil
}
