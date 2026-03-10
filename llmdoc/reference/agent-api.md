# Agent API Reference

Key data structures and interfaces for the Agent Core module.

## Core Structures

### AgentLoop

Central message processing orchestrator.

```go
type AgentLoop struct {
    bus            *bus.MessageBus      // Inbound/outbound message routing
    cfg            *config.Config       // Application configuration
    registry       *AgentRegistry       // Multi-agent instance registry
    state          *state.Manager       // Workspace state persistence
    running        atomic.Bool          // Loop control flag
    summarizing    sync.Map             // Session summarization guards
    fallback       *providers.FallbackChain // Provider failover chain
    channelManager *channels.Manager    // Channel implementations
    mediaStore     media.MediaStore     // Media lifecycle management
    transcriber    voice.Transcriber    // Audio transcription
    cmdRegistry    *commands.Registry   // Slash command handlers
}
```

Source: `pkg/agent/loop.go:39-51`

### AgentInstance

Fully configured agent with isolated workspace.

```go
type AgentInstance struct {
    ID                        string
    Name                      string
    Model                     string
    Fallbacks                 []string
    Workspace                 string
    MaxIterations             int
    MaxTokens                 int
    Temperature               float64
    ThinkingLevel             ThinkingLevel
    ContextWindow             int
    SummarizeMessageThreshold int
    SummarizeTokenPercent     int
    Provider                  providers.LLMProvider
    Sessions                  *session.SessionManager
    ContextBuilder            *ContextBuilder
    Tools                     *tools.ToolRegistry
    Subagents                 *config.SubagentsConfig
    SkillsFilter              []string
    Candidates                []providers.FallbackCandidate
    Router                    *routing.Router         // Optional light/heavy routing
    LightCandidates           []providers.FallbackCandidate
}
```

Source: `pkg/agent/instance.go:18-48`

### ContextBuilder

System prompt constructor with mtime-based caching.

```go
type ContextBuilder struct {
    workspace          string
    skillsLoader       *skills.SkillsLoader
    memory             *MemoryStore
    systemPromptMutex  sync.RWMutex
    cachedSystemPrompt string
    cachedAt           time.Time           // Max mtime at cache build
    existedAtCache     map[string]bool     // File existence snapshot
    skillFilesAtCache  map[string]time.Time // Skill file mtimes
}
```

Source: `pkg/agent/context.go:20-42`

### MemoryStore

Persistent memory with long-term and daily note storage.

```go
type MemoryStore struct {
    workspace  string
    memoryDir  string      // memory/
    memoryFile string      // memory/MEMORY.md
}
```

Source: `pkg/agent/memory.go:22-26`

### ThinkingLevel

Provider thinking parameter configuration.

```go
type ThinkingLevel string

const (
    ThinkingOff      ThinkingLevel = "off"
    ThinkingLow      ThinkingLevel = "low"
    ThinkingMedium   ThinkingLevel = "medium"
    ThinkingHigh     ThinkingLevel = "high"
    ThinkingXHigh    ThinkingLevel = "xhigh"
    ThinkingAdaptive ThinkingLevel = "adaptive"
)
```

Source: `pkg/agent/thinking.go:10-19`

## Key Interfaces

### Tool Interface

Implemented by all agent tools.

```go
type Tool interface {
    Name() string
    Description() string
    Parameters() map[string]any
    Execute(ctx context.Context, args map[string]any) *ToolResult
}
```

Source: `pkg/tools/types.go`

### AsyncExecutor Interface

Optional interface for async tool execution.

```go
type AsyncExecutor interface {
    ExecuteAsync(ctx context.Context, args map[string]any, callback AsyncCallback) *ToolResult
}
```

Source: `pkg/tools/types.go`

## Configuration References

### Agent Defaults (config.json)

```json
{
  "agents": {
    "defaults": {
      "workspace": "~/.picoclaw/workspace",
      "model": "claude-sonnet-4-20250514",
      "max_tool_iterations": 20,
      "max_tokens": 8192,
      "temperature": 0.7,
      "summarize_message_threshold": 20,
      "summarize_token_percent": 75,
      "thinking_level": "off",
      "routing": {
        "enabled": false,
        "light_model": "",
        "threshold": 3
      }
    }
  }
}
```

### Tool Registration Pattern

```go
// pkg/agent/instance.go:72-94
if cfg.Tools.IsToolEnabled("read_file") {
    toolsRegistry.Register(tools.NewReadFileTool(workspace, readRestrict, allowReadPaths))
}
```

## Source of Truth

- **Main Loop:** `pkg/agent/loop.go` - Message processing and tool execution
- **Context Building:** `pkg/agent/context.go` - System prompt assembly with caching
- **Agent Instance:** `pkg/agent/instance.go` - Agent configuration and initialization
- **Agent Registry:** `pkg/agent/registry.go` - Multi-agent management
- **Memory System:** `pkg/agent/memory.go` - Persistent memory storage
- **Tool Registry:** `pkg/tools/registry.go` - Tool registration and execution
