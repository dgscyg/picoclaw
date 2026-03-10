package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"net/http"
	"net/http/httptest"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/muninndb"
	"github.com/sipeed/picoclaw/pkg/tools"
)

func TestFileMemoryStoreImplementsMemoryProviderContract(t *testing.T) {
	workspace := t.TempDir()
	store := NewFileMemoryStore(workspace)

	var provider MemoryProvider = store

	if err := provider.Memorize(context.Background(), "remember this", tools.MemoryWriteOptions{}); err != nil {
		t.Fatalf("Memorize() error = %v", err)
	}

	result, err := provider.Recall(context.Background(), "remember", 5)
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if result == nil || len(result.Entries) == 0 {
		t.Fatalf("Recall() returned no entries: %#v", result)
	}

	memoryContext, err := provider.GetMemoryContext(context.Background())
	if err != nil {
		t.Fatalf("GetMemoryContext() error = %v", err)
	}
	if !strings.Contains(memoryContext, "remember this") {
		t.Fatalf("GetMemoryContext() = %q, want daily note content", memoryContext)
	}

	if err := provider.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestFileMemoryStoreMemorizeModes(t *testing.T) {
	workspace := t.TempDir()
	store := NewFileMemoryStore(workspace)
	ctx := context.Background()

	if err := store.Memorize(ctx, "stable fact", tools.MemoryWriteOptions{LongTerm: true}); err != nil {
		t.Fatalf("Memorize(long-term) error = %v", err)
	}
	if err := store.Memorize(ctx, "today note", tools.MemoryWriteOptions{}); err != nil {
		t.Fatalf("Memorize(daily) error = %v", err)
	}

	memoryFile := filepath.Join(workspace, "memory", "MEMORY.md")
	data, err := os.ReadFile(memoryFile)
	if err != nil {
		t.Fatalf("ReadFile(MEMORY.md) error = %v", err)
	}
	if got := string(data); got != "stable fact" {
		t.Fatalf("MEMORY.md = %q, want %q", got, "stable fact")
	}

	todayFile := store.getTodayFile()
	todayData, err := os.ReadFile(todayFile)
	if err != nil {
		t.Fatalf("ReadFile(today) error = %v", err)
	}
	if !strings.Contains(string(todayData), "today note") {
		t.Fatalf("today file = %q, want daily note content", string(todayData))
	}
}

func TestMuninnDBMemoryStoreFallbackAndContext(t *testing.T) {
	workspace := t.TempDir()
	fallback := NewFileMemoryStore(workspace)
	if err := fallback.Memorize(context.Background(), "fallback memory", tools.MemoryWriteOptions{}); err != nil {
		t.Fatalf("fallback.Memorize() error = %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"temporary"}`))
	}))
	defer server.Close()

	store := &MuninnDBMemoryStore{
		client:   muninndb.NewClientWithHTTPClient(server.Client(), server.URL, "test-vault", "secret"),
		vault:    "test-vault",
		fallback: fallback,
	}

	result, err := store.Recall(context.Background(), "fallback", 3)
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if result == nil || len(result.Entries) == 0 || !strings.Contains(result.Entries[0], "fallback memory") {
		t.Fatalf("Recall() = %#v, want fallback result", result)
	}

	memoryContext, err := store.GetMemoryContext(context.Background())
	if err != nil {
		t.Fatalf("GetMemoryContext() error = %v", err)
	}
	if !strings.Contains(memoryContext, "fallback memory") {
		t.Fatalf("GetMemoryContext() = %q, want fallback content", memoryContext)
	}
}

func TestMuninnDBMemoryStoreConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *config.MuninnDBConfig
		wantErr string
	}{
		{
			name:    "nil config",
			cfg:     nil,
			wantErr: "muninndb config is required",
		},
		{
			name:    "missing endpoint",
			cfg:     &config.MuninnDBConfig{},
			wantErr: "muninndb endpoint is required",
		},
		{
			name: "invalid timeout",
			cfg: &config.MuninnDBConfig{
				Endpoint: "http://127.0.0.1:8080",
				Timeout:  "invalid",
			},
			wantErr: "parse muninndb timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := NewMuninnDBMemoryStore(tt.cfg, t.TempDir())
			if err == nil {
				t.Fatalf("NewMuninnDBMemoryStore() error = nil, store = %#v", store)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestNewMemoryProviderFallsBackToFile(t *testing.T) {
	workspace := t.TempDir()
	cfg := &config.Config{}
	cfg.Memory.Provider = config.MemoryProviderMuninnDB
	cfg.Memory.MuninnDB = &config.MuninnDBConfig{}

	provider := newMemoryProvider(cfg, workspace)

	if _, ok := provider.(*FileMemoryStore); !ok {
		t.Fatalf("provider type = %T, want *FileMemoryStore", provider)
	}
}

