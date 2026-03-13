# Channel System Architecture

## 1. Identity

- **What it is:** A registry-based multi-platform messaging abstraction layer with optional capability interfaces.
- **Purpose:** Decouples platform-specific messaging implementations from the agent core via a Message Bus pattern, supporting 15+ platforms with unified message flow.

## 2. Core Components

- `pkg/channels/base.go` (`Channel`, `BaseChannel`): Core interface defining 7 required methods; shared abstraction providing allowlist checking, group trigger handling, and capability auto-triggering.
- `pkg/channels/interfaces.go` (`TypingCapable`, `MessageEditor`, `ReactionCapable`, `PlaceholderCapable`, `MediaSender`, `WebhookHandler`): Optional capability interfaces for platform-specific features.
- `pkg/channels/registry.go` (`ChannelFactory`, `RegisterFactory`, `getFactory`): Factory registry pattern enabling self-registering channel sub-packages.
- `pkg/channels/manager.go` (`Manager`, `channelWorker`, `runWorker`): Central orchestration for channel lifecycle, rate limiting, retry logic, and message dispatch.
- `pkg/channels/errors.go` (`ErrNotRunning`, `ErrRateLimit`, `ErrTemporary`, `ErrSendFailed`): Sentinel errors for retry strategy classification.
- `pkg/channels/split.go` (`SplitMessage`): Message splitting preserving code block integrity.
- `pkg/bus/bus.go` (`MessageBus`, `PublishInbound`, `SubscribeOutbound`): Buffered channel-based (size 64) message routing between channels and agent.
- `pkg/bus/types.go` (`InboundMessage`, `OutboundMessage`, `OutboundMediaMessage`, `SenderInfo`, `Peer`): Structured message types for bus communication.

### Channel Implementations

- `pkg/channels/telegram/telegram.go` (`TelegramChannel`): Telegram via telego; supports typing, editing, placeholder, media.
- `pkg/channels/discord/discord.go` (`DiscordChannel`): Discord via discordgo; supports typing, editing, placeholder, media.
- `pkg/channels/slack/slack.go` (`SlackChannel`): Slack via Socket Mode; supports reaction, editing, media.
- `pkg/channels/feishu/feishu_64.go` (`FeishuChannel`): Lark SDK; supports cards, reaction, placeholder, media.
- `pkg/channels/qq/qq.go` (`QQChannel`): Tencent QQ via botgo; WebSocket-based with deduplication.
- `pkg/channels/wecom/bot.go` (`WeComBotChannel`): WeCom webhook; implements WebhookHandler for callbacks.
- `pkg/channels/wecom/official.go`, `app.go`, `aibot.go`: WeCom variants for official accounts, apps, and AI bots. `official.go` now covers callback-scoped `replyStream`, explicit `template_card` replies, `template_card_event` auto-update handling via `aibot_respond_update_msg`, callback `response_url` markdown follow-up, and sanitized template-card event content that preserves only user action semantics plus card context in metadata. Template-card callbacks are parsed from the official nested payload `event.template_card_event.*`, so button `event_key`, `card_type`, `task_id`, and `selected_items` survive into inbound metadata instead of collapsing into a generic click event.
- `pkg/channels/qclaw/qclaw.go` (`QClawChannel`): QClaw AGP WebSocket; WeChat service account integration with streaming responses.

## 3. Execution Flow (LLM Retrieval Map)

### Inbound Message Flow

- **1. Receive:** Platform-specific channel receives message (e.g., `pkg/channels/telegram/telegram.go:handleMessage`).
- **2. Delegate:** Channel calls `BaseChannel.HandleMessage` (`pkg/channels/base.go`).
- **3. Validate:** `HandleMessage` checks allowlist via `IsAllowed`/`IsAllowedSender`, applies group trigger filtering via `ShouldRespondInGroup`.
- **4. Capability Trigger:** Auto-triggers typing/reaction/placeholder if channel implements respective interfaces.
- **5. Publish:** `PublishInbound` to `MessageBus` (`pkg/bus/bus.go`).
- **6. Consume:** Agent loop consumes via `ConsumeInbound` (`pkg/agent/loop.go`).

### Outbound Message Flow

- **1. Publish:** Agent publishes `OutboundMessage` via `PublishOutbound` (`pkg/bus/bus.go`).
- **2. Dispatch:** `Manager.dispatchOutbound` routes to per-channel worker (`pkg/channels/manager.go`).
- **3. Pre-send:** `preSend` stops typing, undoes reactions, edits placeholders.
- **4. Split:** Long messages split via `SplitMessage` (`pkg/channels/split.go`).
- **5. Send:** `sendWithRetry` with rate limiting and exponential backoff (`pkg/channels/manager.go`).
- **6. Error Classify:** `ClassifySendError`/`ClassifyNetError` determines retry strategy (`pkg/channels/errutil.go`).

### Channel Registration Flow

- **1. Init:** Channel sub-package `init()` calls `RegisterFactory("channelName", factoryFunc)` (`pkg/channels/registry.go`).
- **2. Lookup:** `Manager.initChannels` calls `getFactory(name)` to retrieve factory.
- **3. Create:** Factory creates channel instance with config and MessageBus reference.
- **4. Inject:** Manager injects `PlaceholderRecorder` and `MediaStore` if channel supports them.

## 4. Design Rationale

### Registry Pattern

Channels self-register via `init()` functions, eliminating circular dependencies. Manager discovers channels by name lookup rather than direct imports.

### Optional Capabilities

Interfaces like `TypingCapable`, `ReactionCapable` enable feature detection via type assertion. `BaseChannel.HandleMessage` auto-triggers available capabilities without each channel reimplementing logic.

### Error Classification

Sentinel errors (`ErrRateLimit`, `ErrTemporary`, `ErrSendFailed`) enable appropriate retry strategies: fixed delay for rate limits, exponential backoff for temporary failures, no retry for permanent failures.

### Rate Limiting

Per-channel rate limits configured in `channelRateConfig`: telegram (20/s), discord (1/s), slack (1/s), matrix (2/s), line (10/s), irc (2/s), qclaw (10/s).

### Group Trigger Modes

Centralized in `ShouldRespondInGroup`: `mention_only` (requires @mention), `prefixes` (matches command prefixes), permissive default (all messages).
