# Agent Core Architecture

## 1. Identity

- **What it is:** The central message processing engine of PicoClaw, orchestrating LLM interactions, tool execution, and conversation state management.
- **Purpose:** Provides the core agent loop with fallback support, context building with caching, multi-agent routing, and persistent memory storage.

## 2. Core Components

- `pkg/agent/loop.go` (`AgentLoop`, `processMessage`, `runLLMIteration`): Central message processing loop handling inbound messages, tool execution, and LLM iteration with fallback chain.
- `pkg/agent/loop.go` (`selectCandidates`, `runAgentLoop`): Model routing logic selecting between primary and light models based on message complexity scoring.
- `pkg/agent/context.go` (`ContextBuilder`, `BuildMessages`, `BuildSystemPromptWithCache`): Constructs system prompts with mtime-based caching for performance optimization.
- `pkg/agent/context.go` (`sanitizeHistoryForProvider`): History sanitization ensuring provider compatibility by removing orphaned tool messages.
- `pkg/agent/instance.go` (`AgentInstance`, `NewAgentInstance`): Fully configured agent with workspace, session manager, context builder, tool registry, and model configuration.
- `pkg/agent/registry.go` (`AgentRegistry`, `GetAgent`, `ResolveRoute`): Thread-safe registry managing multiple agent instances with routing support.
- `pkg/agent/memory.go` (`MemoryStore`, `GetMemoryContext`): Persistent memory system with long-term storage and daily notes.
- `pkg/agent/thinking.go` (`ThinkingLevel`, `parseThinkingLevel`): Provider thinking parameter configuration with levels: off, low, medium, high, xhigh, adaptive.

## 3. Execution Flow (LLM Retrieval Map)

### Message Processing Flow

- **1. Ingestion:** `AgentLoop.Run()` consumes inbound messages from `bus.MessageBus` via `al.bus.ConsumeInbound(ctx)` in `pkg/agent/loop.go:310-318`.
- **2. Routing:** `processMessage()` calls `resolveMessageRoute()` to determine target agent via `registry.ResolveRoute()` in `pkg/agent/loop.go:635-654`.
- **3. Context Building:** `runAgentLoop()` builds messages using `agent.ContextBuilder.BuildMessages()` with cached system prompt in `pkg/agent/loop.go:756-763`.
- **4. LLM Iteration:** `runLLMIteration()` executes the LLM call loop with tool handling in `pkg/agent/loop.go:874-1272`.
- **5. Tool Execution:** Parallel tool execution via `agent.Tools.ExecuteWithContext()` with async callback support in `pkg/agent/loop.go:1201-1209`.
- **6. Response:** Final content returned and optionally published via `al.bus.PublishOutbound()` in `pkg/agent/loop.go:797-803`. If a tool result marks the current turn as already consumed, `runAgentLoop()` exits without publishing or storing an extra assistant reply.

### Context Building Flow

- **1. Cache Check:** `BuildSystemPromptWithCache()` checks `sourceFilesChangedLocked()` for mtime changes in `pkg/agent/context.go:132-170`.
- **2. Identity Assembly:** `getIdentity()` constructs base identity with workspace paths in `pkg/agent/context.go:72-95`.
- **3. Bootstrap Loading:** `LoadBootstrapFiles()` reads AGENTS.md, SOUL.md, USER.md, IDENTITY.md in `pkg/agent/context.go:400-417`.
- **4. Skills Summary:** `skillsLoader.BuildSkillsSummary()` generates available skills list in `pkg/agent/context.go:110-117`.
- **5. Memory Context:** `memory.GetMemoryContext()` retrieves long-term memory and recent daily notes in `pkg/agent/memory.go:134-158`.
- **6. Dynamic Context:** `buildDynamicContext()` adds per-request time, runtime, session info in `pkg/agent/context.go:427-439`.
- **7. History Sanitization:** `sanitizeHistoryForProvider()` ensures proper message ordering for provider API requirements in `pkg/agent/context.go:546-662`.

### Fallback Chain Flow

- **1. Candidate Selection:** `selectCandidates()` chooses primary or light model based on routing configuration in `pkg/agent/loop.go:1282-1310`.
- **2. Fallback Execution:** `al.fallback.Execute()` iterates through `FallbackCandidate` providers on errors in `pkg/agent/loop.go:944-967`.
- **3. Error Handling:** Context window errors trigger `forceCompression()` for history reduction in `pkg/agent/loop.go:1332-1381`.
- **4. Summarization:** `maybeSummarize()` triggers background summarization when thresholds exceeded in `pkg/agent/loop.go:1312-1328`.

## 4. Design Rationale

### System Prompt Caching

The `ContextBuilder` uses mtime-based cache invalidation (`cachedAt`, `existedAtCache`, `skillFilesAtCache`) to avoid rebuilding prompts on every request. This addresses issue #607 (repeated context reprocessing). The cache detects:
- File modifications (mtime comparison)
- File creation/deletion (existence tracking)
- Skill tree changes (recursive file snapshot)

### Provider Compatibility

`sanitizeHistoryForProvider()` enforces strict message ordering rules:
- Single system message at position 0 (required by Anthropic, Codex)
- Tool messages must follow assistant messages with matching tool_calls
- No orphaned tool messages (DeepSeek requirement)

### Memory Architecture

Two-tier storage for agent memory:
- **Long-term:** `memory/MEMORY.md` - persistent facts and knowledge
- **Daily notes:** `memory/YYYYMM/YYYYMMDD.md` - time-based context

Atomic writes with `fsync` ensure flash storage reliability.
