// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package agent

import (
	"context"

	"github.com/sipeed/picoclaw/pkg/tools"
)

// MemoryResult is kept as an alias for backward compatibility.
type MemoryResult = tools.MemoryQueryResult

// MemorizeOptions is kept as an alias for backward compatibility.
type MemorizeOptions = tools.MemoryWriteOptions

// MemoryProvider abstracts persistent memory backends.
type MemoryProvider interface {
	// Recall retrieves relevant memories for the given query.
	Recall(ctx context.Context, query string, limit int) (*tools.MemoryQueryResult, error)

	// Memorize stores new memory content.
	Memorize(ctx context.Context, content string, opts tools.MemoryWriteOptions) error

	// GetMemoryContext returns formatted memory context for prompts.
	GetMemoryContext(ctx context.Context) (string, error)

	// Close releases backend resources.
	Close() error
}

// NewMemoryStore creates the default file-based memory store.
// Kept for backward compatibility.
func NewMemoryStore(workspace string) *FileMemoryStore {
	return NewFileMemoryStore(workspace)
}
