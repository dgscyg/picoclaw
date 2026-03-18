# How to Extend Agent Behavior

A guide for extending PicoClaw agent functionality.

## Adding New Tools

1. **Create Tool Implementation:** Implement `tools.Tool` interface in `pkg/tools/` with `Name()`, `Description()`, `Parameters()`, and `Execute()` methods.
2. **Register Tool:** In `pkg/agent/instance.go:NewAgentInstance()`, add registration call: `toolsRegistry.Register(tools.NewYourTool(workspace, restrict, paths))`.
3. **Enable in Config:** Add tool name to `config.json` under `tools.enabled` array, or use `tools.IsToolEnabled("your_tool")` for conditional registration.

Reference: `pkg/agent/instance.go:72-94` for built-in tool registration pattern.

## Customizing Context Building

1. **Add Bootstrap File:** Create new `.md` file in workspace root (alongside AGENTS.md, SOUL.md, USER.md, IDENTITY.md).
2. **Modify Source Paths:** Edit `ContextBuilder.sourcePaths()` in `pkg/agent/context.go:190-198` to include new file.
3. **Extend Build Order:** Update `BuildSystemPrompt()` in `pkg/agent/context.go:97-127` to append new content section.

Cache invalidation is automatic via mtime tracking in `sourceFilesChangedLocked()`.

## Configuring Fallback Providers

1. **Define Model List:** Add provider entries to `model_list` in `config.json` with protocol prefix (e.g., `openai/gpt-4`, `anthropic/claude-3`).
2. **Configure Agent Fallbacks:** Set `agents.list[].model.fallbacks` array with model names from `model_list`.
3. **Fallback Resolution:** `resolveAgentFallbacks()` in `pkg/agent/instance.go:258-263` merges agent-specific with default fallbacks.
4. **Candidate Pre-computation:** `providers.ResolveCandidatesWithLookup()` resolves at agent creation time, avoiding runtime lookups.

Reference: `pkg/agent/instance.go:145-189` for candidate resolution logic.

## Adding Memory Types

1. **Extend MemoryStore:** Add new methods to `pkg/agent/memory.go` following `ReadLongTerm()`/`WriteLongTerm()` pattern.
2. **Update Context:** Modify `GetMemoryContext()` in `pkg/agent/memory.go:134-158` to include new memory type in prompt.
3. **File Organization:** Use subdirectories under `memory/` for different memory categories.

## Custom Agent Routing

1. **Define Route Rules:** Configure `agents.routes` in `config.json` with `channel`, `peer`, `guild_id`, `team_id` patterns.
2. **Subagent Permissions:** Set `agents.list[].subagents.allow_agents` array for spawn tool allowlist (use `["*"]` for all).
3. **Light Model Routing:** Configure `agents.defaults.routing.enabled` and `routing.light_model` for complexity-based model selection.

Reference: `pkg/agent/registry.go:84-102` for `CanSpawnSubagent()` permission check.

## Verification

After modifications:
1. Run `go build ./...` to verify compilation.
2. Test with `picoclaw serve` and check logs for agent initialization.
3. Verify tool availability with `/tools` command in any channel.
