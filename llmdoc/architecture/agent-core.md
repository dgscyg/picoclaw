# Agent Core Architecture

## 1. Identity

- **What it is:** The central message processing engine of PicoClaw, orchestrating LLM interactions, tool execution, and conversation state management.
- **Purpose:** Provides the core agent loop with fallback support, context building with caching, multi-agent routing, and persistent memory storage.

## 2. Core Components

- `pkg/agent/loop.go` (`AgentLoop`, `Run`, `processMessageDetailed`): Central message-processing entrypoint handling route resolution, same-scope steering drain, reply-aware final delivery, and round-scoped tool-send de-duplication.
- `pkg/agent/loop.go` (`runAgentLoop`, `runTurn`, `publishResponseIfNeeded`): Turn shell plus full turn lifecycle, including hook execution, Muninn auto recall/capture integration, reply-aware final sends, and suppression of redundant assistant replies after direct tool delivery.
- `pkg/agent/context.go` (`ContextBuilder`, `BuildMessages`, `BuildSystemPromptWithCache`): Constructs system prompts with mtime-based caching for performance optimization.
- `pkg/agent/context.go` (`sanitizeHistoryForProvider`): History sanitization ensuring provider compatibility by removing orphaned tool messages.
- `pkg/agent/instance.go` (`AgentInstance`, `NewAgentInstance`): Fully configured agent with workspace, session manager, context builder, tool registry, and model configuration.
- `pkg/agent/registry.go` (`AgentRegistry`, `GetAgent`, `ResolveRoute`): Thread-safe registry managing multiple agent instances with routing support.
- `pkg/agent/turn.go` (`turnState`, `turnResult`, `ActiveTurnInfo`): Per-turn lifecycle state, abort/restore bookkeeping, SubTurn parent-child tracking, and current-turn consumption signalling.
- `pkg/agent/steering.go` (`dequeueSteeringMessagesForScope`, `Continue`): Queued same-session user steering and idle-turn continuation.
- `pkg/agent/hooks.go` (`HookManager`): Before/after LLM and tool interception, approval, and event-linked control flow.
- `pkg/agent/memory.go` (`MemoryStore`, `GetMemoryContext`): Persistent memory system with long-term storage and daily notes.
- `pkg/agent/thinking.go` (`ThinkingLevel`, `parseThinkingLevel`): Provider thinking parameter configuration with levels: off, low, medium, high, xhigh, adaptive.

## 3. Execution Flow (LLM Retrieval Map)

### Message Processing Flow

- **1. Ingestion:** `AgentLoop.Run()` consumes inbound messages from `bus.MessageBus`, then drains immediately queued same-scope inbound traffic into the steering queue while the active turn is running in `pkg/agent/loop.go:489` and `pkg/agent/steering.go:268-328`.
- **2. Routing:** `processMessageDetailed()` resolves the target agent and session scope, normalizes template-card callbacks into user-history-safe text, and creates a round ID used by direct-send suppression in `pkg/agent/loop.go:1398`.
- **3. Context Building:** `runTurn()` loads session history, injects dynamic prompt blocks from Muninn auto recall when enabled, and rebuilds messages after emergency compression in `pkg/agent/loop.go:1958`.
- **4. Turn Execution:** `runTurn()` owns the full lifecycle: turn start/end events, graceful interrupt polling, steering injection, SubTurn result intake, LLM retries, and session restore on abort in `pkg/agent/loop.go:1958` and `pkg/agent/turn.go:42-110`.
- **5. Tool Execution:** `runTurn()` executes provider-returned tool calls in order, wrapping each call with hook stages, `reply_to` routing context, user-message context, and round ID propagation before `ToolRegistry.ExecuteWithContext(...)` in `pkg/agent/loop.go:2586-2975`.
- **6. Response:** `publishResponseIfNeeded()` and `sendFinalMessage()` publish the final answer only when no direct-send tool already consumed the turn; callback-aware channels still receive normalized `reply_to` on the final send in `pkg/agent/loop.go:205-257` and `pkg/agent/loop.go:730-772`.

### Context Building Flow

- **1. Cache Check:** `BuildSystemPromptWithCache()` checks `sourceFilesChangedLocked()` for mtime changes in `pkg/agent/context.go:132-170`.
- **2. Identity Assembly:** `getIdentity()` constructs base identity with workspace paths in `pkg/agent/context.go:72-95`.
- **3. Bootstrap Loading:** `LoadBootstrapFiles()` prefers `AGENT.md` / `AGENTS.md`, then `SOUL.md` and `USER.md`, and only falls back to legacy `IDENTITY.md` when the new per-workspace agent files are absent in `pkg/agent/context.go:351-419`.
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

### Reply-Aware Direct Delivery

`processOptions` carries per-turn `ReplyTo`, `RoundID`, `HistoryUserMessage`, and optional dynamic prompt blocks in `pkg/agent/loop.go:74-91`. This lets the merged agent core preserve platform callback semantics for `wecom_official`, de-duplicate direct `message` / `wecom_card` sends within one round, and still keep session history clean when callback payloads need a normalized user-facing representation.
