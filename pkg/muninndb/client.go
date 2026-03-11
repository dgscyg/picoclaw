package muninndb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultTimeout = 30 * time.Second

// Client is a REST client for MuninnDB.
type Client struct {
	httpClient *http.Client
	endpoint   string
	vault      string
	apiKey     string
}

// NewClient creates a MuninnDB client with sane defaults.
func NewClient(endpoint, vault, apiKey string) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		endpoint:   strings.TrimRight(endpoint, "/"),
		vault:      vault,
		apiKey:     apiKey,
	}
}

// NewClientWithHTTPClient creates a client with a custom HTTP client.
func NewClientWithHTTPClient(httpClient *http.Client, endpoint, vault, apiKey string) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{
		httpClient: httpClient,
		endpoint:   strings.TrimRight(endpoint, "/"),
		vault:      vault,
		apiKey:     apiKey,
	}
}

// Activate performs a semantic memory activation query.
// The query string is converted to Context for MuninnDB API.
func (c *Client) Activate(ctx context.Context, query string, limit int) (*ActivateResponse, error) {
	if limit == 0 {
		limit = 10
	}

	req := ActivateRequest{
		Vault:      c.vault,
		Context:    []string{query},
		MaxResults: limit,
	}

	var resp ActivateResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/activate", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// WriteEngram writes a new memory engram to MuninnDB.
func (c *Client) WriteEngram(ctx context.Context, content string, tags []string, concept string) (*WriteResponse, error) {
	req := WriteRequest{
		Vault:   c.vault,
		Content: content,
		Tags:    tags,
		Concept: concept,
	}

	var resp WriteResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/engrams", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Link creates or updates a semantic relationship between two engrams.
func (c *Client) Link(ctx context.Context, sourceID, targetID, relation string, weight float64) (*LinkResponse, error) {
	req := LinkRequest{
		Vault:    c.vault,
		SourceID: sourceID,
		TargetID: targetID,
		Relation: relation,
		Weight:   weight,
	}

	var resp LinkResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/link", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Traverse explores related engrams starting from a seed engram or query.
func (c *Client) Traverse(ctx context.Context, req TraverseRequest) (*TraverseResponse, error) {
	req.Vault = c.vault

	var resp TraverseResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/traverse", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Explain returns a relevance explanation for an engram or query.
func (c *Client) Explain(ctx context.Context, req ExplainRequest) (*ExplainResponse, error) {
	req.Vault = c.vault

	var resp ExplainResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/explain", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Contradictions returns contradiction inspection results for the current vault.
func (c *Client) Contradictions(ctx context.Context) (*ContradictionsResponse, error) {
	path := "/api/contradictions"
	if c.vault != "" {
		path += "?vault=" + url.QueryEscape(c.vault)
	}

	var resp ContradictionsResponse
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// Health checks if MuninnDB is healthy.
func (c *Client) Health(ctx context.Context) error {
	return c.doJSON(ctx, http.MethodGet, "/api/health", nil, nil)
}

func (c *Client) doJSON(ctx context.Context, method, path string, payload any, out any) error {
	var body []byte
	var err error
	if payload != nil {
		body, err = json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
	}

	url := c.endpoint + path
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if err := sleepWithContext(ctx, time.Duration(attempt)*100*time.Millisecond); err != nil {
				return fmt.Errorf("%w: %v", ErrTemporary, err)
			}
		}

		var reqBody io.Reader
		if body != nil {
			reqBody = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}

		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("%w: %v", ErrTemporary, err)
			continue
		}

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			lastErr = fmt.Errorf("read response: %w", readErr)
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			apiErr := &APIError{StatusCode: resp.StatusCode, Body: string(respBody)}
			if apiErr.Temporary() && attempt < 2 {
				lastErr = fmt.Errorf("%w: %w", ErrTemporary, apiErr)
				continue
			}
			if apiErr.Temporary() {
				return fmt.Errorf("%w: %w", ErrTemporary, apiErr)
			}
			return fmt.Errorf("%w: %w", ErrRequest, apiErr)
		}

		if out == nil || len(respBody) == 0 {
			return nil
		}
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("%w: request failed", ErrRequest)
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
