package muninndb

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientActivate(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantErr    error
		wantCalls  int
	}{
		{
			name:       "success",
			statusCode: http.StatusOK,
			body:       `{"engrams":[{"content":"recent decision","tags":["project"]}]}`,
			wantCalls:  1,
		},
		{
			name:       "retries temporary error",
			statusCode: http.StatusInternalServerError,
			body:       `{"error":"temporary"}`,
			wantErr:    ErrTemporary,
			wantCalls:  3,
		},
		{
			name:       "returns request error on bad request",
			statusCode: http.StatusBadRequest,
			body:       `{"error":"bad request"}`,
			wantErr:    ErrRequest,
			wantCalls:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost {
					t.Fatalf("method = %s, want POST", r.Method)
				}
				if r.URL.Path != "/api/v1/vault/test-vault/activate" {
					t.Fatalf("path = %s", r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer secret" {
					t.Fatalf("authorization = %q", got)
				}
				var req ActivateRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				if req.Query != "recent project decisions" {
					t.Fatalf("query = %q", req.Query)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := NewClientWithHTTPClient(server.Client(), server.URL, "test-vault", "secret")
			resp, err := client.Activate(context.Background(), ActivateRequest{Query: "recent project decisions"})

			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Activate() error = %v", err)
				}
				if resp == nil || len(resp.Engrams) != 1 {
					t.Fatalf("unexpected response: %+v", resp)
				}
			} else {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("error = %v, want %v", err, tt.wantErr)
				}
			}

			if calls != tt.wantCalls {
				t.Fatalf("calls = %d, want %d", calls, tt.wantCalls)
			}
		})
	}
}

func TestClientWriteEngram(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/vault/test-vault/engrams" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var got Engram
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if got.Content != "hello memory" {
			t.Fatalf("content = %q", got.Content)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := NewClientWithHTTPClient(server.Client(), server.URL, "test-vault", "secret")
	err := client.WriteEngram(context.Background(), Engram{Content: "hello memory", Tags: []string{"note"}})
	if err != nil {
		t.Fatalf("WriteEngram() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestClientContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"engrams":[]}`))
	}))
	defer server.Close()

	client := NewClientWithHTTPClient(server.Client(), server.URL, "test-vault", "secret")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Activate(ctx, ActivateRequest{Query: "recent project decisions"})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if !errors.Is(err, ErrTemporary) {
		t.Fatalf("error = %v, want wrapped ErrTemporary", err)
	}
}
