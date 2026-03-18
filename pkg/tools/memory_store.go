package tools

import (
	"context"
	"fmt"
	"strings"
)

// MemoryStoreTool stores content into the configured memory provider.
type MemoryStoreTool struct {
	memory MemoryAccess
}

func NewMemoryStoreTool(memory MemoryAccess) *MemoryStoreTool {
	return &MemoryStoreTool{memory: memory}
}

func (t *MemoryStoreTool) Name() string {
	return "memory_store"
}

func (t *MemoryStoreTool) Description() string {
	return "Store important information into the agent's long-term memory or daily notes"
}

func (t *MemoryStoreTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{
				"type":        "string",
				"description": "The memory content to store",
			},
			"long_term": map[string]any{
				"type":        "boolean",
				"description": "Whether to store this as long-term memory instead of a daily note",
				"default":     true,
			},
			"tags": map[string]any{
				"type":        "array",
				"description": "Optional tags describing the memory",
				"items": map[string]any{
					"type": "string",
				},
			},
		},
		"required": []string{"content"},
	}
}

func (t *MemoryStoreTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	if t.memory == nil {
		return ErrorResult("memory provider is not configured")
	}

	content, ok := args["content"].(string)
	content = strings.TrimSpace(content)
	if !ok || content == "" {
		return ErrorResult("content is required and must be a non-empty string")
	}

	longTerm := true
	if v, ok := args["long_term"].(bool); ok {
		longTerm = v
	}

	content = mergeMemoryTags(content, args["tags"])
	if err := t.memory.Memorize(ctx, content, MemoryWriteOptions{LongTerm: longTerm}); err != nil {
		return ErrorResult(fmt.Sprintf("memory store failed: %v", err)).WithError(err)
	}

	target := "daily notes"
	if longTerm {
		target = "long-term memory"
	}
	return SilentResult(fmt.Sprintf("Stored memory in %s", target))
}

func mergeMemoryTags(content string, rawTags any) string {
	tags, ok := rawTags.([]any)
	if !ok || len(tags) == 0 {
		return content
	}

	values := make([]string, 0, len(tags))
	for _, raw := range tags {
		tag, ok := raw.(string)
		if !ok {
			continue
		}
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		values = append(values, tag)
	}
	if len(values) == 0 {
		return content
	}

	return fmt.Sprintf("%s\n\nTags: %s", content, strings.Join(values, ", "))
}
