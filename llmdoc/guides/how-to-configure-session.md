# How to Configure Session Behavior

This guide covers configuring session persistence, history compression, and model routing in PicoClaw.

## 1. Session Storage Configuration

Session files are stored in `<workspace>/sessions/` by default. Each session is persisted as a JSON file named `<sanitized_key>.json`.

Session keys use format `channel:chatID` (e.g., `telegram:123456`), sanitized to `telegram_123456.json` for Windows compatibility.

## 2. Configure History Compression Thresholds

Edit `config.yaml` under `agents.defaults`:

```yaml
agents:
  defaults:
    summarize_message_threshold: 20    # Trigger summarization after N messages
    summarize_token_percent: 75       # Trigger summarization at N% of context window
```

**Default Values:**
- `summarize_message_threshold`: 20 messages
- `summarize_token_percent`: 75% of context window

**Behavior:**
- When history exceeds message threshold OR token estimate exceeds token percentage, background summarization triggers.
- Summarization keeps last 4 messages and generates a summary of older content via LLM.

Reference: `pkg/config/defaults.go:36-37`, `pkg/agent/loop.go:1313-1328`.

## 3. Configure Model Routing

Enable light model routing for simple queries:

```yaml
agents:
  defaults:
    routing:
      light_model: "gpt-4o-mini"    # Model name from model_list
      threshold: 0.35               # Complexity score threshold
```

**Routing Logic:**
- Score < threshold → use light model
- Score >= threshold → use primary model

**Complexity Features:**
- Token estimate (CJK-aware)
- Code block count
- Recent tool calls (last 6 history entries)
- Conversation depth
- Media attachments

Reference: `pkg/routing/router.go:61-72`, `pkg/routing/features.go:43-51`.

## 4. Workspace State Persistence

Workspace state (last channel/chat) is stored in `<workspace>/state/state.json`.

**Migration:** System automatically migrates from legacy `state.json` to `state/state.json` on first run.

Reference: `pkg/state/state.go:36-73`.

## 5. Session Key Patterns

Session keys follow these patterns:

| Pattern | Example | Description |
|---------|---------|-------------|
| Default | `telegram:123456` | Channel + Chat ID |
| Agent-specific | `agent:telegram:123456` | Agent prefix for isolation |
| Group chat | `telegram:-100123456` | Negative Chat ID for groups |

Reference: `pkg/session/manager.go:153-155` for sanitization logic.

## 6. Verify Configuration

Check logs for session-related messages:
- `Forced compression executed` - Emergency compression triggered
- `Model routing: light model selected` - Light model routing active
- `Memory threshold reached` - Background summarization started

Run with debug logging to see compression scores and routing decisions.
