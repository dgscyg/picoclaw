package agent

import (
	"context"

	"github.com/sipeed/picoclaw/pkg/tools"
)

// NoopMemoryStore disables local memory context and persistence in MCP-only Muninn mode.
type NoopMemoryStore struct{}

func NewNoopMemoryStore() *NoopMemoryStore {
	return &NoopMemoryStore{}
}

func (ms *NoopMemoryStore) Recall(ctx context.Context, query string, limit int) (*tools.MemoryQueryResult, error) {
	return &tools.MemoryQueryResult{Entries: nil}, nil
}

func (ms *NoopMemoryStore) Memorize(ctx context.Context, content string, opts tools.MemoryWriteOptions) error {
	return nil
}

func (ms *NoopMemoryStore) GetMemoryContext(ctx context.Context) (string, error) {
	return "", nil
}

func (ms *NoopMemoryStore) Close() error {
	return nil
}

var _ MemoryProvider = (*NoopMemoryStore)(nil)
