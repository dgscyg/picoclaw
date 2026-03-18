package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/muninndb"
	"github.com/sipeed/picoclaw/pkg/tools"
)

// MuninnDBMemoryStore is retained only as a compatibility wrapper for direct Muninn REST access.
// The main Muninn mode now relies on official MCP integration instead of this type.
type MuninnDBMemoryStore struct {
	client   *muninndb.Client
	vault    string
	fallback *FileMemoryStore
}

func NewMuninnDBMemoryStore(cfg *config.MuninnDBConfig, workspace string) (*MuninnDBMemoryStore, error) {
	if cfg == nil {
		return nil, fmt.Errorf("muninndb config is required")
	}
	endpoint := strings.TrimSpace(cfg.MCPEndpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("muninndb mcp endpoint is required")
	}
	vault := strings.TrimSpace(cfg.Vault)
	if vault == "" {
		vault = config.DefaultMemoryVault
	}
	httpClient := &http.Client{Timeout: 30 * time.Second}
	if timeout := strings.TrimSpace(cfg.Timeout); timeout != "" {
		d, err := time.ParseDuration(timeout)
		if err != nil {
			return nil, fmt.Errorf("parse muninndb timeout: %w", err)
		}
		httpClient.Timeout = d
	}
	store := &MuninnDBMemoryStore{client: muninndb.NewClientWithHTTPClient(httpClient, endpoint, vault, strings.TrimSpace(cfg.APIKey)), vault: vault, fallback: NewFileMemoryStore(workspace)}
	logger.InfoCF("agent", "Initialized compatibility MuninnDB memory store", map[string]any{"vault": vault, "mcp_endpoint": endpoint})
	return store, nil
}

func (ms *MuninnDBMemoryStore) Recall(ctx context.Context, query string, limit int) (*tools.MemoryQueryResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		limit = 5
	}
	resp, err := ms.client.Activate(ctx, strings.TrimSpace(query), limit)
	if err != nil {
		if ms.fallback != nil {
			logger.WarnCF("agent", "MuninnDB recall failed, falling back to file memory", map[string]any{"vault": ms.vault, "error": err.Error()})
			return ms.fallback.Recall(ctx, query, limit)
		}
		return nil, fmt.Errorf("muninndb recall: %w", err)
	}
	entries := make([]string, 0, len(resp.Activations))
	for _, item := range resp.Activations {
		content := formatActivationForMemory(item)
		if content != "" {
			entries = append(entries, content)
		}
	}
	return &tools.MemoryQueryResult{Entries: entries}, nil
}

func (ms *MuninnDBMemoryStore) Memorize(ctx context.Context, content string, opts tools.MemoryWriteOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	tags := memoryTags(opts)
	concept := "Daily Note"
	if opts.LongTerm {
		concept = "Long-term Memory"
	}
	_, err := ms.client.WriteEngram(ctx, content, tags, concept)
	if err != nil {
		if ms.fallback != nil {
			logger.WarnCF("agent", "MuninnDB memorize failed, falling back to file memory", map[string]any{"vault": ms.vault, "error": err.Error()})
			return ms.fallback.Memorize(ctx, content, opts)
		}
		return fmt.Errorf("muninndb memorize: %w", err)
	}
	return nil
}

func (ms *MuninnDBMemoryStore) GetMemoryContext(ctx context.Context) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := ms.Recall(ctx, "", 5)
	if err != nil {
		if ms.fallback != nil {
			return ms.fallback.GetMemoryContext(ctx)
		}
		return "", err
	}
	if result == nil || len(result.Entries) == 0 {
		return "", nil
	}
	var sb strings.Builder
	sb.WriteString("## Long-term Memory\n\n")
	for i, entry := range result.Entries {
		if i > 0 {
			sb.WriteString("\n\n---\n\n")
		}
		sb.WriteString(entry)
	}
	return sb.String(), nil
}

func (ms *MuninnDBMemoryStore) Close() error {
	if ms.fallback != nil {
		return ms.fallback.Close()
	}
	return nil
}

func formatActivationForMemory(item muninndb.ActivationItem) string {
	content := strings.TrimSpace(item.Content)
	if content == "" {
		return ""
	}
	parts := []string{content}
	if item.Concept != "" {
		parts = append(parts, "Concept: "+item.Concept)
	}
	if item.Score > 0 {
		parts = append(parts, fmt.Sprintf("Relevance: %.2f", item.Score))
	}
	return strings.Join(parts, "\n")
}

func memoryTags(opts tools.MemoryWriteOptions) []string {
	if opts.LongTerm {
		return []string{"long-term"}
	}
	return []string{"daily-note"}
}

var _ MemoryProvider = (*MuninnDBMemoryStore)(nil)
var _ = errors.Is
