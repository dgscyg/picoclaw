# Session & Bus System Architecture

## 1. Identity

- **What it is:** A message routing and conversation persistence layer comprising the Message Bus for async channel-agent communication, Session Manager for in-memory conversation storage, and State Manager for workspace persistence.
- **Purpose:** Decouples platform channels from the agent core via a buffered bus pattern while maintaining conversation state across sessions with configurable history compression.

## 2. Core Components

### Message Bus

- `pkg/bus/bus.go` (`MessageBus`, `NewMessageBus`): Three-channel design (inbound, outbound, outboundMedia) with 64-slot buffers; context-aware publish/consume with graceful shutdown draining.
- `pkg/bus/types.go` (`InboundMessage`, `OutboundMessage`, `OutboundMediaMessage`): Structured message types for bus communication with routing metadata. `InboundMessage.SessionKey` allows channel-supplied conversation anchors; `OutboundMessage` carries `ReplyTo` / `ReplyToMessageID`; `OutboundMediaMessage` carries `ReplyTo` for callback-scoped media delivery.
- `pkg/bus/types.go` (`Peer`, `SenderInfo`): Routing peer identification (direct/group/channel) and sender identity with platform-agnostic CanonicalID format.

### Session Management

- `pkg/session/manager.go` (`Session` struct): Session with Key, Messages ([]providers.Message), Summary, Created/Updated timestamps.
- `pkg/session/manager.go` (`SessionManager`, `NewSessionManager`): Thread-safe map with RWMutex; lazy session creation via GetOrCreate.
- `pkg/session/manager.go` (`AddMessage`, `AddFullMessage`, `GetHistory`, `SetHistory`): Message operations with deep copy isolation.
- `pkg/session/manager.go` (`Save`, `sanitizeFilename`): Atomic JSON persistence using temp file + fsync + rename; sanitizes "channel:chatID" keys for Windows compatibility.

### State Persistence

- `pkg/state/state.go` (`State` struct): Workspace state with LastChannel, LastChatID, Timestamp.
- `pkg/state/state.go` (`Manager`, `NewManager`): Atomic state persistence with migration support from legacy state.json to state/state.json.
- `pkg/state/state.go` (`saveAtomic`): Uses `fileutil.WriteFileAtomic` for crash-safe persistence (critical for SD cards/flash storage).

### Memory Store Interface

- `pkg/memory/store.go` (`Store` interface): Abstraction for persistent session storage with AddMessage, GetHistory, SetSummary, TruncateHistory, SetHistory, Compact, Close.
- `pkg/memory/jsonl.go` (`JSONLStore`): Append-only JSONL implementation with logical truncation (skip offset) and physical compaction.

### Model Routing

- `pkg/routing/router.go` (`Router`, `RouterConfig`): Rule-based model selection with complexity threshold (default 0.35).
- `pkg/routing/router.go` (`SelectModel`): Returns (model, usedLight, score) based on feature classification.
- `pkg/routing/features.go` (`Features`, `ExtractFeatures`): Feature vector with TokenEstimate, CodeBlockCount, RecentToolCalls, ConversationDepth, HasAttachments.
- `pkg/routing/features.go` (`estimateTokens`): CJK-aware token estimation (CJK = 1 token, others = 0.25 tokens).

## 3. Execution Flow (LLM Retrieval Map)

### Inbound Message Flow

- **1. Receive:** Channel receives platform message and constructs `InboundMessage` with `Peer`, `SenderInfo`, and an optional explicit `SessionKey`.
- **2. Publish:** Channel calls `bus.PublishInbound(ctx, msg)` via `BaseChannel.HandleMessage` or `BaseChannel.HandleMessageWithSessionKey` (`pkg/channels/base.go`).
- **3. Queue:** Message buffered in 64-slot inbound channel (`pkg/bus/bus.go:26`).
- **4. Consume:** Agent loop receives via `bus.ConsumeInbound(ctx)` in `pkg/agent/loop.go:310-318`.
- **5. Route:** `resolveMessageRoute()` determines target agent via registry (`pkg/agent/loop.go:635-654`).

### Session Key Resolution

- **1. Channel Override:** If `InboundMessage.SessionKey` is non-empty, `AgentLoop.resolveScopeKey()` uses it directly for history/context.
- **2. Route Fallback:** Otherwise routing derives the session key from agent ID, channel, peer scope, and DM scope policy (for example `agent:sales:telegram:direct:user123`).
- **3. Platform Anchors:** `wecom_official` uses explicit anchors to avoid chat-level history collisions: ordinary callbacks prefer `wecom_official:<chatID>:msg:<msgid>`, template-card events prefer `wecom_official:<chatID>:task:<task_id>`, and both fall back to `...:req:<req_id>` when no stronger anchor exists.
- **4. Main Session:** Agent-global sessions still use `agent:<agentID>:main`.
- **5. Sanitize:** File persistence replaces ":" with "_" for Windows compatibility (`pkg/session/manager.go:153-155`).

### History Compression Flow

- **1. Threshold Check:** `maybeSummarize()` checks message count and token estimate against thresholds (`pkg/agent/loop.go:1313-1328`).
- **2. Background Summarization:** Triggers LLM summarization keeping last 4 messages for continuity.
- **3. Emergency Compression:** `forceCompression()` drops oldest 50% when context limit hit (`pkg/agent/loop.go:1332-1381`).
- **4. Persist:** Updated history saved via `Sessions.SetHistory()` and `Sessions.Save()`.

### Model Routing Flow

- **1. Extract:** `ExtractFeatures(msg, history)` computes feature vector (`pkg/routing/features.go:43-51`).
- **2. Score:** `RuleClassifier.Score()` produces complexity score in [0, 1].
- **3. Select:** Score < threshold → light model; otherwise → primary model (`pkg/routing/router.go:61-72`).

### Outbound Message Flow

- **1. Publish or Direct-Finalize:** Agent publishes ordinary `OutboundMessage` / `OutboundMediaMessage` via the bus, but may call `FinalMessageCapable.SendFinal` directly when the channel needs a dedicated final-turn send path.
- **2. Subscribe:** Channel manager receives bus traffic via `SubscribeOutbound(ctx)` / `SubscribeOutboundMedia(ctx)` (`pkg/channels/manager.go`).
- **3. Dispatch:** Routes to per-channel worker with rate limiting.
- **4. Pre-send:** Handles typing stop, reaction undo, placeholder editing.
- **5. Send:** `sendWithRetry` or `sendMediaWithRetry` delivers the message; `ReplyTo` lets callback-capable channels reuse live reply tokens for text or media.

## 4. Design Rationale

### Three-Channel Bus Design

Separate channels for inbound, outbound, and outboundMedia enable independent buffering and processing rates. The 64-slot buffer absorbs burst traffic without blocking publishers.

### Atomic Persistence Pattern

Both session and state files use temp file + fsync + rename pattern:
1. Write to temp file
2. fsync to ensure durability (critical for SD cards)
3. Atomic rename (POSIX guarantees atomicity)
4. Handles Windows volume separator issue in session keys

### Dual Compression Strategy

- **Proactive (Summarization):** Triggers at message count or token threshold; runs in background; preserves conversation context via summary.
- **Reactive (Force Compression):** Emergency fallback when context window exceeded; drops 50% oldest messages immediately.

### CJK-Aware Token Estimation

Token estimation distinguishes CJK characters (1 token each) from Latin text (0.25 tokens/char), avoiding 3x underestimation for Asian languages.

### Session Isolation

Agent-prefixed session keys ("agent:channel:chatID") enable multiple agents to serve the same chat independently without history collision.

### Channel-Agnostic Bus DTOs

The bus model stays generic: final-turn semantics are not encoded as a shared `OutboundMessage.Final` field. Channels that care about finality use capability interfaces, while the bus continues to represent only routing data (`Channel`, `ChatID`, `ReplyTo`, text/media payloads).
