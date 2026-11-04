// Package scm holds the shared plumbing for source-control providers. Each
// provider lives in its own subpackage and implements domain.SCM.
package scm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

// ErrNotPush marks webhook deliveries that are not push events.
var ErrNotPush = errors.New("not a push event")

// DefaultHTTPClient bounds every provider call.
func DefaultHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

// DoJSON performs an authenticated JSON request and decodes a 2xx body into out.
func DoJSON(ctx context.Context, client *http.Client, method, url, token string, body, out any, headers map[string]string) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode body: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "rocketsled")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
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
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode body: %w", err)
	}
	return nil
}

// ReadBody reads a webhook payload with a sane cap.
func ReadBody(r *http.Request) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r.Body, 4<<20))
}
