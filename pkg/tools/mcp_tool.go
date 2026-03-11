package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type MCPManager interface {
	CallTool(ctx context.Context, serverName, toolName string, arguments map[string]any) (*mcp.CallToolResult, error)
}

type MCPTool struct {
	manager     MCPManager
	serverName  string
	tool        *mcp.Tool
	defaultArgs map[string]any
	forcedArgs  map[string]any
}

func NewMCPTool(manager MCPManager, serverName string, tool *mcp.Tool) *MCPTool {
	return &MCPTool{manager: manager, serverName: serverName, tool: tool}
}

func NewMCPToolWithDefaults(manager MCPManager, serverName string, tool *mcp.Tool, defaultArgs map[string]any) *MCPTool {
	return &MCPTool{manager: manager, serverName: serverName, tool: tool, defaultArgs: defaultArgs}
}

func NewMCPToolWithArgPolicy(manager MCPManager, serverName string, tool *mcp.Tool, defaultArgs, forcedArgs map[string]any) *MCPTool {
	return &MCPTool{manager: manager, serverName: serverName, tool: tool, defaultArgs: defaultArgs, forcedArgs: forcedArgs}
}

func sanitizeIdentifierComponent(s string) string {
	const maxLen = 64
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	prevUnderscore := false
	for _, r := range s {
		isAllowed := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-'
		if !isAllowed {
			if !prevUnderscore {
				b.WriteRune('_')
				prevUnderscore = true
			}
			continue
		}
		if r == '_' {
			if prevUnderscore {
				continue
			}
			prevUnderscore = true
		} else {
			prevUnderscore = false
		}
		b.WriteRune(r)
	}
	result := strings.Trim(b.String(), "_")
	if result == "" {
		result = "unnamed"
	}
	if len(result) > maxLen {
		result = result[:maxLen]
	}
	return result
}

func (t *MCPTool) Name() string {
	sanitizedServer := sanitizeIdentifierComponent(t.serverName)
	sanitizedTool := sanitizeIdentifierComponent(t.tool.Name)
	full := fmt.Sprintf("mcp_%s_%s", sanitizedServer, sanitizedTool)
	lossless := strings.ToLower(t.serverName) == sanitizedServer && strings.ToLower(t.tool.Name) == sanitizedTool
	const maxTotal = 64
	if lossless && len(full) <= maxTotal {
		return full
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(t.serverName + "\x00" + t.tool.Name))
	suffix := fmt.Sprintf("%08x", h.Sum32())
	base := full
	if len(base) > maxTotal-9 {
		base = strings.TrimRight(full[:maxTotal-9], "_")
	}
	return base + "_" + suffix
}

func (t *MCPTool) Description() string {
	desc := t.tool.Description
	if desc == "" {
		desc = fmt.Sprintf("MCP tool from %s server", t.serverName)
	}
	return fmt.Sprintf("[MCP:%s] %s", t.serverName, desc)
}

func (t *MCPTool) Parameters() map[string]any {
	schema := t.tool.InputSchema
	if schema == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}, "required": []string{}}
	}
	if schemaMap, ok := schema.(map[string]any); ok {
		return schemaMap
	}
	var jsonData []byte
	if rawMsg, ok := schema.(json.RawMessage); ok {
		jsonData = rawMsg
	} else if bytes, ok := schema.([]byte); ok {
		jsonData = bytes
	}
	if jsonData != nil {
		var result map[string]any
		if err := json.Unmarshal(jsonData, &result); err == nil {
			return result
		}
		return map[string]any{"type": "object", "properties": map[string]any{}, "required": []string{}}
	}
	var err error
	jsonData, err = json.Marshal(schema)
	if err != nil {
		return map[string]any{"type": "object", "properties": map[string]any{}, "required": []string{}}
	}
	var result map[string]any
	if err := json.Unmarshal(jsonData, &result); err != nil {
		return map[string]any{"type": "object", "properties": map[string]any{}, "required": []string{}}
	}
	return result
}

func (t *MCPTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	finalArgs := mergeMCPArgs(t.defaultArgs, t.forcedArgs, args)
	result, err := t.manager.CallTool(ctx, t.serverName, t.tool.Name, finalArgs)
	if err != nil {
		return ErrorResult(fmt.Sprintf("MCP tool execution failed: %v", err)).WithError(err)
	}
	if result == nil {
		nilErr := fmt.Errorf("MCP tool returned nil result without error")
		return ErrorResult("MCP tool execution failed: nil result").WithError(nilErr)
	}
	if result.IsError {
		errMsg := extractContentText(result.Content)
		return ErrorResult(fmt.Sprintf("MCP tool returned error: %s", errMsg)).WithError(fmt.Errorf("MCP tool error: %s", errMsg))
	}
	output := extractContentText(result.Content)
	return &ToolResult{ForLLM: output, IsError: false}
}

func mergeMCPArgs(defaults, forced, provided map[string]any) map[string]any {
	merged := map[string]any{}
	for k, v := range defaults {
		merged[k] = v
	}
	for k, v := range provided {
		merged[k] = v
	}
	for k, v := range forced {
		merged[k] = v
	}
	return merged
}

func extractContentText(content []mcp.Content) string {
	var parts []string
	for _, c := range content {
		switch v := c.(type) {
		case *mcp.TextContent:
			parts = append(parts, v.Text)
		case *mcp.ImageContent:
			parts = append(parts, fmt.Sprintf("[Image: %s]", v.MIMEType))
		default:
			parts = append(parts, fmt.Sprintf("[Content: %T]", v))
		}
	}
	return strings.Join(parts, "\n")
}
