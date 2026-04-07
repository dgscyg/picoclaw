# Tool System Architecture

## 1. Identity

- **What it is:** Extensible tool execution framework enabling LLMs to interact with filesystem, shell, web, and external services.
- **Purpose:** Provides a unified interface for tool registration, execution, and result handling with support for synchronous/asynchronous operations and MCP integration.

## 2. Core Components

- `pkg/tools/base.go` (`Tool`, `AsyncExecutor`, `AsyncCallback`): Defines the fundamental tool interface and async execution pattern.
- `pkg/tools/registry.go` (`ToolRegistry`, `Register`, `ExecuteWithContext`): Thread-safe tool registry with sorted iteration for KV cache stability.
- `pkg/tools/result.go` (`ToolResult`, `NewToolResult`, `SilentResult`, `AsyncResult`, `ErrorResult`, `UserResult`): Structured result types with dual-output support (ForLLM/ForUser), current-turn consumption control, and optional ephemeral message history handoff for SubTurn/evaluator flows.
- `pkg/tools/toolloop.go` (`RunToolLoop`, `ToolLoopConfig`, `ToolLoopResult`): Core LLM + tool call iteration loop with parallel execution.
- `pkg/tools/types.go` (`ToolDefinition`, `ToolCall`, `Message`): Data structures for LLM communication following OpenAI function calling format.
- `pkg/tools/mcp_tool.go` (`MCPTool`, `MCPManager`): MCP protocol adapter wrapping external tools as native PicoClaw tools.
- `pkg/mcp/manager.go` (`Manager`, `ServerConnection`, `ConnectServer`, `CallTool`): MCP server connection management with stdio/SSE transport support.

## 3. Execution Flow (LLM Retrieval Map)

- **1. Registration:** Tools registered via `ToolRegistry.Register()` at agent initialization (`pkg/agent/instance.go:45-80`).
- **2. Schema Conversion:** `ToolRegistry.ToProviderDefs()` converts tools to provider-compatible JSON Schema (`pkg/tools/registry.go:147-177`).
- **3. LLM Call:** Agent loop calls LLM with tool definitions via `providers.LLMProvider.Chat()` (`pkg/tools/toolloop.go:67`).
- **4. Tool Call Parsing:** Response tool calls normalized via `providers.NormalizeToolCall()` (`pkg/tools/toolloop.go:90`).
- **5. Execution Order:** The generic `pkg/tools/toolloop.go` helper can execute tool calls in parallel, but the merged main-agent path in `pkg/agent/loop.go:2586-2975` executes provider-returned tool calls in order so hook decisions, `reply_to` reuse, and current-turn suppression stay deterministic.
- **6. Context Injection:** `ToolRegistry.ExecuteWithContext()` injects channel/chatID/`reply_to` routing context via `WithToolRoutingContext(...)`, while the agent loop adds per-round `WithToolRoundID(...)` and current-user-message context before each call.
- **7. Result Handling:** Tool results append tool-role messages to session history; `ToolResult.ConsumesCurrentTurn` suppresses redundant assistant narration after direct delivery, and `ToolResult.Messages` carries ephemeral SubTurn/evaluator history when a tool needs to return stateful worker context instead of only plain text.
- **8. Hidden Tool Discovery:** When MCP discovery is enabled, hidden tools stay out of the default tool list until `tool_search_tool_bm25` or `tool_search_tool_regex` finds them and `PromoteTools(...)` temporarily unlocks them (`pkg/tools/search_tool.go`, `pkg/tools/registry.go`).
- **9. MCP Bridge:** External MCP tools wrapped by `MCPTool.Execute()` calling `MCPManager.CallTool()` (`pkg/tools/mcp_tool.go`). The wrapper now preserves both `Content` text and JSON-marshaled `StructuredContent`, so tools that only return structured payloads still give the LLM usable output.

## 4. Design Rationale

### Tool Interface Simplicity
The `Tool` interface requires only 4 methods (`Name`, `Description`, `Parameters`, `Execute`), making it trivial to implement new tools. The optional `AsyncExecutor` interface extends this for background operations without blocking the agent loop.

### Context-Based Routing
Request-scoped context (`WithToolRoutingContext`) provides channel/chatID to tools without mutable state on tool instances, enabling safe concurrent execution of tool calls across multiple conversations.

### Dual-Output Results
`ToolResult` separates `ForLLM` (context for reasoning) from `ForUser` (direct display), allowing tools to provide detailed information to the LLM while presenting concise output to users.

`ToolResult.ConsumesCurrentTurn` adds explicit control-flow semantics for side-effect tools that already delivered the user-facing answer. This avoids relying on prompt wording or post-hoc text suppression.

`ToolResult.Messages` is intentionally not serialized to JSON. It exists only for in-process evaluator/SubTurn flows that need to preserve ephemeral worker session history across iterations without polluting normal user-facing tool output.

### Discovery Recall Quality
Hidden-tool discovery is only as good as its search corpus. PicoClaw builds BM25 search text from tool name plus description and now also appends a normalized form that splits `snake_case`, kebab-case, and punctuation-heavy identifiers into plain terms. This matters for MCP tools because their exposed names are often sanitized identifiers such as `mcp_server_control_ac_device`; without normalization, natural-language queries can miss already-registered tools even when the tool name obviously contains the right action words.

### Sandbox Abstraction
File tools use `fileSystem` interface with three implementations: `hostFs` (unrestricted), `sandboxFs` (using `os.Root`), and `whitelistFs` (regex-based path whitelist), enabling flexible security models.

### Shell Safety
`ExecTool` implements 30+ deny patterns blocking dangerous commands (rm -rf, format, shutdown, etc.) with support for custom allow/deny patterns and workspace path restriction.
