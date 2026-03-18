# Tool System Architecture

## 1. Identity

- **What it is:** Extensible tool execution framework enabling LLMs to interact with filesystem, shell, web, and external services.
- **Purpose:** Provides a unified interface for tool registration, execution, and result handling with support for synchronous/asynchronous operations and MCP integration.

## 2. Core Components

- `pkg/tools/base.go` (`Tool`, `AsyncExecutor`, `AsyncCallback`): Defines the fundamental tool interface and async execution pattern.
- `pkg/tools/registry.go` (`ToolRegistry`, `Register`, `ExecuteWithContext`): Thread-safe tool registry with sorted iteration for KV cache stability.
- `pkg/tools/result.go` (`ToolResult`, `NewToolResult`, `SilentResult`, `AsyncResult`, `ErrorResult`, `UserResult`): Structured result types with dual-output support (ForLLM/ForUser) and current-turn consumption control.
- `pkg/tools/toolloop.go` (`RunToolLoop`, `ToolLoopConfig`, `ToolLoopResult`): Core LLM + tool call iteration loop with parallel execution.
- `pkg/tools/types.go` (`ToolDefinition`, `ToolCall`, `Message`): Data structures for LLM communication following OpenAI function calling format.
- `pkg/tools/mcp_tool.go` (`MCPTool`, `MCPManager`): MCP protocol adapter wrapping external tools as native PicoClaw tools.
- `pkg/mcp/manager.go` (`Manager`, `ServerConnection`, `ConnectServer`, `CallTool`): MCP server connection management with stdio/SSE transport support.

## 3. Execution Flow (LLM Retrieval Map)

- **1. Registration:** Tools registered via `ToolRegistry.Register()` at agent initialization (`pkg/agent/instance.go:45-80`).
- **2. Schema Conversion:** `ToolRegistry.ToProviderDefs()` converts tools to provider-compatible JSON Schema (`pkg/tools/registry.go:147-177`).
- **3. LLM Call:** Agent loop calls LLM with tool definitions via `providers.LLMProvider.Chat()` (`pkg/tools/toolloop.go:67`).
- **4. Tool Call Parsing:** Response tool calls normalized via `providers.NormalizeToolCall()` (`pkg/tools/toolloop.go:90`).
- **5. Parallel Execution:** Most tool calls execute in parallel via `ToolRegistry.ExecuteWithContext()` (`pkg/tools/toolloop.go:134-156`), but side-effect tools such as `message`, `wecom_card`, and `send_file` are forced into sequential execution by the agent loop to preserve ordering and channel reply semantics.
- **6. Context Injection:** `WithToolRoutingContext()` injects channel/chatID into context for tool routing (`pkg/tools/registry.go:77`).
- **7. Result Handling:** Tool results appended as tool role messages; tools may additionally mark the current conversation turn as consumed so the agent stops before generating redundant assistant narration (`pkg/agent/loop.go`, `pkg/tools/result.go`).
- **8. MCP Bridge:** External MCP tools wrapped by `MCPTool.Execute()` calling `MCPManager.CallTool()` (`pkg/tools/mcp_tool.go`). The wrapper now preserves both `Content` text and JSON-marshaled `StructuredContent`, so tools that only return structured payloads still give the LLM usable output.

## 4. Design Rationale

### Tool Interface Simplicity
The `Tool` interface requires only 4 methods (`Name`, `Description`, `Parameters`, `Execute`), making it trivial to implement new tools. The optional `AsyncExecutor` interface extends this for background operations without blocking the agent loop.

### Context-Based Routing
Request-scoped context (`WithToolRoutingContext`) provides channel/chatID to tools without mutable state on tool instances, enabling safe concurrent execution of tool calls across multiple conversations.

### Dual-Output Results
`ToolResult` separates `ForLLM` (context for reasoning) from `ForUser` (direct display), allowing tools to provide detailed information to the LLM while presenting concise output to users.

`ToolResult.ConsumesCurrentTurn` adds explicit control-flow semantics for side-effect tools that already delivered the user-facing answer. This avoids relying on prompt wording or post-hoc text suppression.

### Sandbox Abstraction
File tools use `fileSystem` interface with three implementations: `hostFs` (unrestricted), `sandboxFs` (using `os.Root`), and `whitelistFs` (regex-based path whitelist), enabling flexible security models.

### Shell Safety
`ExecTool` implements 30+ deny patterns blocking dangerous commands (rm -rf, format, shutdown, etc.) with support for custom allow/deny patterns and workspace path restriction.
