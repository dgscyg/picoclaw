package tools

import (
	"context"
	"fmt"
	"strings"
)

// MemoryAccess abstracts memory operations needed by memory tools.
type MemoryAccess interface {
	Recall(ctx context.Context, query string, limit int) (*MemoryQueryResult, error)
	Memorize(ctx context.Context, content string, opts MemoryWriteOptions) error
}

// MemoryQueryResult represents retrieved memory content.
type MemoryQueryResult struct {
	Entries []string
}

// MemoryWriteOptions controls how new memory is persisted.
type MemoryWriteOptions struct {
	LongTerm bool
}

// MemoryRecallTool recalls relevant memories from the configured memory provider.
type MemoryRecallTool struct {
	memory MemoryAccess
}

func NewMemoryRecallTool(memory MemoryAccess) *MemoryRecallTool {
	return &MemoryRecallTool{memory: memory}
}

func (t *MemoryRecallTool) Name() string {
	return "memory_recall"
}

func (t *MemoryRecallTool) Description() string {
	return "Recall relevant memories from the agent's long-term memory. Use this before acting on remembered identifiers such as contact chat_id mappings, user aliases, device IDs, or other stored routing facts."
}

func (t *MemoryRecallTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The search query to find relevant memories. For example, use a person's name to recall a stored chat_id/contact mapping before sending them a message.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Maximum number of memories to return",
				"default":     5,
				"minimum":     1.0,
				"maximum":     20.0,
			},
		},
		"required": []string{"query"},
	}
}

func (t *MemoryRecallTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	if t.memory == nil {
		return ErrorResult("memory provider is not configured")
	}

	query, ok := args["query"].(string)
	query = strings.TrimSpace(query)
	if !ok || query == "" {
		return ErrorResult("query is required and must be a non-empty string")
	}

	limit := 5
	if l, ok := args["limit"].(float64); ok {
		if li := int(l); li >= 1 && li <= 20 {
			limit = li
		}
	}

	result, err := t.memory.Recall(ctx, query, limit)
	if err != nil {
		return ErrorResult(fmt.Sprintf("memory recall failed: %v", err)).WithError(err)
	}

	return SilentResult(formatMemoryEntries("Recalled memories", query, result))
}

func formatMemoryEntries(title, query string, result *MemoryQueryResult) string {
	if result == nil || len(result.Entries) == 0 {
		return fmt.Sprintf("%s for %q: no matching memories found", title, query)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s for %q (%d results):\n\n", title, query, len(result.Entries)))
	for i, entry := range result.Entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("%d. %s", i+1, entry))
		if i < len(result.Entries)-1 {
			sb.WriteString("\n\n---\n\n")
		}
	}
	return sb.String()
}
