package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/muninndb"
	"github.com/sipeed/picoclaw/pkg/tools"
)

// MuninnDBMemoryStore stores and retrieves memories from a MuninnDB backend.
type MuninnDBMemoryStore struct {
	client   *muninndb.Client
	vault    string
	fallback *FileMemoryStore
}

// NewMuninnDBMemoryStore creates a MuninnDB-backed memory store.
func NewMuninnDBMemoryStore(cfg *config.MuninnDBConfig, workspace string) (*MuninnDBMemoryStore, error) {
	if cfg == nil {
		return nil, fmt.Errorf("muninndb config is required")
	}

	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("muninndb endpoint is required")
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

	store := &MuninnDBMemoryStore{
		client: muninndb.NewClientWithHTTPClient(httpClient, endpoint, vault, strings.TrimSpace(cfg.APIKey)),
		vault:  vault,
	}
	if cfg.FallbackToFile {
		store.fallback = NewFileMemoryStore(workspace)
	}

	logger.InfoCF("agent", "Initialized MuninnDB memory store", map[string]any{
		"vault":            vault,
		"endpoint":         endpoint,
		"fallback_to_file": cfg.FallbackToFile,
	})

	return store, nil
}

// Recall retrieves relevant memories from MuninnDB.
func (ms *MuninnDBMemoryStore) Recall(ctx context.Context, query string, limit int) (*tools.MemoryQueryResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		limit = 5
	}

	resp, err := ms.client.Activate(ctx, muninndb.ActivateRequest{
		Query: strings.TrimSpace(query),
		Limit: limit,
		Mode:  "semantic",
	})
	if err != nil {
		if ms.fallback != nil {
			logger.WarnCF("agent", "MuninnDB recall failed, falling back to file memory", map[string]any{
				"vault": vaultOrUnknown(ms.vault),
				"error": err.Error(),
			})
			return ms.fallback.Recall(ctx, query, limit)
		}
		return nil, fmt.Errorf("muninndb recall: %w", err)
	}

	entries := make([]string, 0, len(resp.Engrams))
	for _, engram := range resp.Engrams {
		content := formatEngramForMemory(engram)
		if content == "" {
			continue
		}
		entries = append(entries, content)
		if len(entries) >= limit {
			break
		}
	}

	return &tools.MemoryQueryResult{Entries: entries}, nil
}

// Memorize persists memory content to MuninnDB.
func (ms *MuninnDBMemoryStore) Memorize(ctx context.Context, content string, opts tools.MemoryWriteOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}

	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	engramID, err := newEngramID()
	if err != nil {
		return fmt.Errorf("generate engram id: %w", err)
	}

	engram := muninndb.Engram{
		ID:        engramID,
		Content:   content,
		CreatedAt: time.Now().UTC(),
		Tags:      memoryTags(opts),
	}

	if err := ms.client.WriteEngram(ctx, engram); err != nil {
		if ms.fallback != nil {
			logger.WarnCF("agent", "MuninnDB memorize failed, falling back to file memory", map[string]any{
				"vault": vaultOrUnknown(ms.vault),
				"error": err.Error(),
			})
			return ms.fallback.Memorize(ctx, content, opts)
		}
		return fmt.Errorf("muninndb memorize: %w", err)
	}

	return nil
}

// GetMemoryContext returns formatted memory context for prompts.
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

// Close releases memory store resources.
func (ms *MuninnDBMemoryStore) Close() error {
	if ms.fallback != nil {
		return ms.fallback.Close()
	}
	return nil
}

func formatEngramForMemory(engram muninndb.Engram) string {
	content := strings.TrimSpace(engram.Content)
	if content == "" {
		content = strings.TrimSpace(engram.Summary)
	}
	if content == "" {
		return ""
	}

	parts := []string{content}
	if len(engram.Tags) > 0 {
		parts = append(parts, "Tags: "+strings.Join(engram.Tags, ", "))
	}
	if engram.Concept != "" {
		parts = append(parts, "Concept: "+engram.Concept)
	}
	if len(engram.KeyPoints) > 0 {
		parts = append(parts, "Key Points: "+strings.Join(engram.KeyPoints, "; "))
	}
	return strings.Join(parts, "\n")
}

func newEngramID() ([16]byte, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return [16]byte{}, err
	}
	var out [16]byte
	copy(out[:], id[:])
	return out, nil
}

func memoryTags(opts tools.MemoryWriteOptions) []string {
	if opts.LongTerm {
		return []string{"long-term"}
	}
	return []string{"daily-note"}
}

func vaultOrUnknown(vault string) string {
	vault = strings.TrimSpace(vault)
	if vault == "" {
		return "unknown"
	}
	return vault
}

var _ MemoryProvider = (*MuninnDBMemoryStore)(nil)

var _ = errors.Is
