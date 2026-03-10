package muninndb

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

func (c *Client) Activate(ctx context.Context, req ActivateRequest) (*ActivateResponse, error) {
	if req.Limit == 0 {
		req.Limit = 10
	}
	if req.Mode == "" {
		req.Mode = "semantic"
	}

	var resp ActivateResponse
	if err := c.doJSON(ctx, http.MethodPost, "/api/v1/vault/"+c.vault+"/activate", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) WriteEngram(ctx context.Context, engram Engram) error {
	return c.doJSON(ctx, http.MethodPost, "/api/v1/vault/"+c.vault+"/engrams", engram, nil)
}

func (c *Client) doJSON(ctx context.Context, method, path string, payload any, out any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := c.endpoint + path
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if err := sleepWithContext(ctx, time.Duration(attempt)*100*time.Millisecond); err != nil {
				return fmt.Errorf("%w: %v", ErrTemporary, err)
			}
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")

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
