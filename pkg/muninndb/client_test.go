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
			body:       `{"query_id":"test","activations":[{"id":"123","content":"recent decision","tags":["project"]}],"total_found":1}`,
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
				if r.URL.Path != "/api/activate" {
					t.Fatalf("path = %s, want /api/activate", r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer secret" {
					t.Fatalf("authorization = %q", got)
				}
				var req ActivateRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				if req.Vault != "test-vault" {
					t.Fatalf("vault = %q, want test-vault", req.Vault)
				}
				if len(req.Context) != 1 || req.Context[0] != "recent project decisions" {
					t.Fatalf("context = %v", req.Context)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := NewClientWithHTTPClient(server.Client(), server.URL, "test-vault", "secret")
			resp, err := client.Activate(context.Background(), "recent project decisions", 10)

			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("Activate() error = %v", err)
				}
				if resp == nil || len(resp.Activations) != 1 {
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
		if r.URL.Path != "/api/engrams" {
			t.Fatalf("path = %s, want /api/engrams", r.URL.Path)
		}
		var got WriteRequest
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if got.Content != "hello memory" {
			t.Fatalf("content = %q", got.Content)
		}
		if got.Vault != "test-vault" {
			t.Fatalf("vault = %q, want test-vault", got.Vault)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"abc123","created_at":1234567890}`))
	}))
	defer server.Close()

	client := NewClientWithHTTPClient(server.Client(), server.URL, "test-vault", "secret")
	resp, err := client.WriteEngram(context.Background(), "hello memory", []string{"note"}, "test concept")
	if err != nil {
		t.Fatalf("WriteEngram() error = %v", err)
	}
	if resp == nil || resp.ID != "abc123" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestClientContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"query_id":"test","activations":[],"total_found":0}`))
	}))
	defer server.Close()

	client := NewClientWithHTTPClient(server.Client(), server.URL, "test-vault", "secret")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := client.Activate(ctx, "test query", 10)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if !errors.Is(err, ErrTemporary) {
		t.Fatalf("error = %v, want wrapped ErrTemporary", err)
	}
}

func TestClientHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/health" {
			t.Fatalf("path = %s, want /api/health", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-vault", "secret")
	if err := client.Health(context.Background()); err != nil {
		t.Fatalf("Health() error = %v", err)
	}
}
