// Package massdriver adapts the Massdriver V2 GraphQL API to domain.Platform.
package massdriver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/massdriver-cloud/rocketsled/internal/domain"
)

const graphqlPath = "/api/v2"

// Client talks to one Massdriver organization.
type Client struct {
	endpoint   string
	orgID      string
	authHeader string
	http       *http.Client
}

var _ domain.Platform = (*Client)(nil)

// New builds a Client. apiKey may be a service-account or personal access token
// (md_/mds_ prefix, sent as Bearer) or a legacy API key (sent as Basic org:key).
func New(baseURL, orgID, apiKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(orgID+":"+apiKey))
	if strings.HasPrefix(apiKey, "md_") || strings.HasPrefix(apiKey, "mds_") {
		auth = "Bearer " + apiKey
	}
	return &Client{
		endpoint:   strings.TrimRight(baseURL, "/") + graphqlPath,
		orgID:      orgID,
		authHeader: auth,
		http:       httpClient,
	}
}

type gqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type gqlError struct {
	Message    string         `json:"message"`
	Code       string         `json:"code"`
	Path       []any          `json:"path"`
	Extensions map[string]any `json:"extensions"`
}

func (e gqlError) code() string {
	if e.Code != "" {
		return e.Code
	}
	if c, ok := e.Extensions["code"].(string); ok {
		return c
	}
	return ""
}

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []gqlError      `json:"errors"`
}

// APIError is a non-validation failure returned by the API.
type APIError struct {
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("massdriver: %s (%s)", e.Message, e.Code)
	}
	return "massdriver: " + e.Message
}

// Is maps API codes onto domain sentinels.
func (e *APIError) Is(target error) bool {
	switch target {
	case domain.ErrNotFound:
		return e.Code == "NOT_FOUND"
	case domain.ErrForbidden:
		return e.Code == "FORBIDDEN" || e.Code == "UNAUTHENTICATED"
	}
	return false
}

// ValidationError is a mutation payload with successful=false.
type ValidationError struct {
	Mutation string
	Messages []validationMessage
}

type validationMessage struct {
	Field   string `json:"field"`
	Message string `json:"message"`
	Code    string `json:"code"`
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Messages))
	for _, m := range e.Messages {
		if m.Field != "" {
			parts = append(parts, m.Field+": "+m.Message)
			continue
		}
		parts = append(parts, m.Message)
	}
	if len(parts) == 0 {
		return "massdriver: " + e.Mutation + " failed"
	}
	return "massdriver: " + e.Mutation + " failed: " + strings.Join(parts, "; ")
}

// do executes a GraphQL operation and decodes data into out.
func (c *Client) do(ctx context.Context, query string, vars map[string]any, out any) error {
	if vars == nil {
		vars = map[string]any{}
	}
	vars["organizationId"] = c.orgID
	body, err := json.Marshal(gqlRequest{Query: query, Variables: vars})
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "rocketsled")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("massdriver request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &APIError{Code: "UNAUTHENTICATED", Message: "credentials rejected (HTTP " + resp.Status + ")"}
	}
	if resp.StatusCode >= 500 {
		return &APIError{Message: "server error (HTTP " + resp.Status + ")"}
	}
	var gr gqlResponse
	if err := json.Unmarshal(raw, &gr); err != nil {
		return fmt.Errorf("decode response (HTTP %s): %w", resp.Status, err)
	}
	if len(gr.Errors) > 0 {
		first := gr.Errors[0]
		return &APIError{Code: first.code(), Message: first.Message}
	}
	if out != nil {
		if err := json.Unmarshal(gr.Data, out); err != nil {
			return fmt.Errorf("decode data: %w", err)
		}
	}
	return nil
}

type payload[T any] struct {
	Successful bool                `json:"successful"`
	Messages   []validationMessage `json:"messages"`
	Result     *T                  `json:"result"`
}

func (p payload[T]) check(mutation string) error {
	if p.Successful {
		return nil
	}
	return &ValidationError{Mutation: mutation, Messages: p.Messages}
}

var errNilResult = errors.New("massdriver: mutation succeeded without a result")
