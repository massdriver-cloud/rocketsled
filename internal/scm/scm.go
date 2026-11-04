// Package scm holds the shared plumbing for sign-in providers. Each provider
// lives in its own subpackage and implements domain.SCM.
package scm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HTTPError is a non-2xx provider response.
type HTTPError struct {
	Status int
	Body   string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body)
}

// DefaultHTTPClient bounds every provider call.
func DefaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

// GetJSON performs an authenticated GET and decodes a 2xx body into out.
func GetJSON(ctx context.Context, client *http.Client, url, token string, out any, headers map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "rocketsled")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet := string(raw)
		if len(snippet) > 300 {
			snippet = snippet[:300]
		}
		return &HTTPError{Status: resp.StatusCode, Body: snippet}
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode body: %w", err)
	}
	return nil
}
