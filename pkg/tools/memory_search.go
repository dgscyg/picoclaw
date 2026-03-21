package tools

import (
	"context"
	"fmt"
	"strings"
)

// MemorySearchTool performs semantic-style memory search via the configured provider.
type MemorySearchTool struct {
	memory MemoryAccess
}

func NewMemorySearchTool(memory MemoryAccess) *MemorySearchTool {
	return &MemorySearchTool{memory: memory}
}

func (t *MemorySearchTool) Name() string {
	return "memory_search"
}

func (t *MemorySearchTool) Description() string {
	return "Search the agent's memory for semantically relevant information. Use this to find stored contact mappings such as chat_id, aliases, or device identifiers before calling other tools."
}

func (t *MemorySearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The semantic search query for relevant memories. For example, search a person's name to find a remembered chat_id/contact mapping before sending them a message.",
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

func (t *MemorySearchTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
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
		return ErrorResult(fmt.Sprintf("memory search failed: %v", err)).WithError(err)
	}

	if result == nil || len(result.Entries) == 0 {
		return SilentResult(fmt.Sprintf("Memory search for %q returned no results", query))
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Memory search results for %q:\n\n", query))
	for i, entry := range result.Entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("Result %d:\n%s", i+1, entry))
		if i < len(result.Entries)-1 {
			sb.WriteString("\n\n")
		}
	}

	return SilentResult(sb.String())
}
