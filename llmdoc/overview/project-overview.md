# PicoClaw Project Overview

## 1. Identity

- **What it is:** PicoClaw is an ultra-lightweight personal AI Assistant written in Go, designed to run on $10 hardware with <10MB RAM.
- **Purpose:** Provides a self-hosted AI assistant with multi-platform chat integration, LLM provider abstraction, tool execution, and scheduled task management.

## 2. High-Level Description

PicoClaw is inspired by [nanobot](https://github.com/HKUDS/nanobot) and refactored from the ground up through AI self-bootstrapping. It serves as a personal AI assistant that bridges messaging platforms (Telegram, Discord, Slack, WeCom, Feishu, QQ, WhatsApp, etc.) with multiple LLM providers (OpenAI, Anthropic, Zhipu, DeepSeek, Gemini, etc.).

The system architecture centers on a Message Bus pattern that decouples channel implementations from the agent core. Key design principles include:

- **Provider Factory with Fallback Chain:** Automatic failover between LLM providers with error classification and exponential backoff cooldown.
- **Tool Registry with Context Injection:** Extensible tool system supporting filesystem operations, shell commands, web search, MCP integration, and subagent spawning.
- **Multi-Agent Routing:** Registry-based agent management with workspace isolation and subagent permission controls.

Core capabilities include session management with automatic summarization, persistent memory (long-term + daily notes), cron-based scheduled tasks, periodic heartbeat processing, and OAuth-based authentication for multiple providers.

## 3. Tech Stack

- **Language:** Go 1.25+
- **CLI Framework:** Cobra
- **Key Libraries:**
  - `telego` (Telegram), `discordgo` (Discord), `slack-go` (Slack)
  - `anthropic-sdk-go`, `openai-go` (LLM providers)
  - `gronx` (cron expressions)
- **Storage:** JSON files with atomic writes, SQLite for scheduled tasks
- **Supported Platforms:** Linux (amd64, arm64, armv7, mipsle, riscv64, loong64), macOS, Windows, FreeBSD

## 4. Architecture Highlights

### Core Components

| Package | Description |
|---------|-------------|
| `pkg/agent/` | Agent loop, context builder, instance/registry, memory system |
| `pkg/channels/` | Channel implementations (15+ platforms) with registry pattern |
| `pkg/providers/` | LLM provider abstraction with fallback and cooldown mechanisms |
| `pkg/tools/` | Tool registry, execution loop, built-in tools |
| `pkg/bus/` | Message bus for channel-agent decoupling |
| `pkg/session/` | Session management with persistence |
| `pkg/mcp/` | Model Context Protocol integration |

### Key Files

- `pkg/agent/loop.go` (`AgentLoop`, `processWithFallback`): Central message processing with fallback support.
- `pkg/channels/manager.go` (`Manager`): Channel lifecycle and message routing.
- `pkg/providers/factory.go` (`resolveProviderSelection`): Provider factory supporting 15+ protocols.
- `pkg/tools/toolloop.go` (`RunToolLoop`): Core LLM + tool iteration loop.
- `pkg/bus/bus.go` (`MessageBus`): Buffered channel-based message routing.
- `cmd/picoclaw/main.go` (`NewPicoclawCommand`): CLI entry point with Cobra framework.

### Data Flow

1. **Inbound:** Channel receives message -> `BaseChannel.HandleMessage` -> `MessageBus.PublishInbound` -> Agent consumes.
2. **Processing:** `AgentLoop.processMessage` -> `ContextBuilder.BuildMessages` -> LLM call with fallback -> Tool execution loop.
3. **Outbound:** Agent publishes to bus -> `Manager.dispatchOutbound` -> Channel worker with rate limiting -> Platform send.

## 5. Related Documents

- `/llmdoc/agent/channel_system_investigation_report.md` - Channel architecture details
- `/llmdoc/agent/provider_system_investigation_report.md` - Provider abstraction details
- `/llmdoc/agent/tool_system_investigation.md` - Tool system architecture
- `/llmdoc/agent/agent_core_investigation_report.md` - Agent loop and context management
- `/llmdoc/agent/session-state-bus-investigation-report.md` - Session and bus systems
