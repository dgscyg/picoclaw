# How to Add a New Channel

This guide describes how to implement a new messaging platform channel in PicoClaw.

## 1. Create Channel Package

Create a new directory under `pkg/channels/<platform>/` with a main implementation file.

## 2. Implement Channel Interface

Implement the `Channel` interface defined in `pkg/channels/base.go`:

```go
type Channel interface {
    Name() string
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    Send(ctx context.Context, msg bus.OutboundMessage) error
    IsRunning() bool
    IsAllowed(senderID string) bool
    IsAllowedSender(sender bus.SenderInfo) bool
    ReasoningChannelID() string
}
```

Embed `BaseChannel` for shared functionality:

```go
type MyChannel struct {
    channels.BaseChannel
    // platform-specific fields
}
```

## 3. Implement Optional Capabilities

Implement optional interfaces from `pkg/channels/interfaces.go` based on platform support:

- **TypingCapable:** `StartTyping(ctx context.Context, chatID string) (stop func(), err error)`
- **MessageEditor:** `EditMessage(ctx context.Context, chatID, messageID, content string) error`
- **MessageDeleter:** `DeleteMessage(ctx context.Context, chatID, messageID string) error`
- **ReactionCapable:** `ReactToMessage(ctx context.Context, chatID, messageID string) (undo func(), err error)`
- **PlaceholderCapable:** `SendPlaceholder(ctx context.Context, chatID string) (messageID string, err error)`
- **StreamingCapable:** `BeginStream(ctx context.Context, chatID string) (channels.Streamer, error)`
- **FinalMessageCapable:** `SendFinal(ctx context.Context, msg bus.OutboundMessage) error`
- **MediaSender:** `SendMedia(ctx context.Context, msg bus.OutboundMediaMessage) error`
- **WebhookHandler:** `WebhookPath() string` + `http.Handler` (for webhook-based channels)

## 4. Create Factory and Register

Create `init.go` in the package to self-register:

```go
func init() {
    channels.RegisterFactory("myplatform", func(cfg *config.Config, bus *bus.MessageBus) (channels.Channel, error) {
        return NewMyChannel(cfg, bus)
    })
}
```

Reference: `pkg/channels/telegram/init.go` for a complete example.

## 5. Handle Inbound Messages

Call `BaseChannel.HandleMessage` for normal inbound messages:

```go
func (c *MyChannel) handleIncoming(msg *PlatformMessage) {
    peer := bus.Peer{Kind: "direct", ID: msg.ChatID}
    sender := bus.SenderInfo{
        Platform:    c.Name(),
        PlatformID:  msg.SenderID,
        CanonicalID: identity.BuildCanonicalID(c.Name(), msg.SenderID),
    }
    c.HandleMessage(ctx, peer, msg.ID, msg.SenderID, msg.ChatID, msg.Text, nil, nil, sender)
}
```

If the upstream platform exposes a better per-conversation anchor than `chatID`, use `HandleMessageWithSessionKey(...)` instead and pass an explicit `sessionKey`. This is important for platforms like WeCom where multiple independent callback threads can exist inside one chat.

Reference: `pkg/channels/base.go:HandleMessage` and `HandleMessageWithSessionKey` for allowlist checks, media scope construction, and capability auto-triggering.

## 6. Add Rate Limit Configuration

Update `channelRateConfig` in `pkg/channels/manager.go` with appropriate rate limit for the platform.

## 7. Verify Integration

1. Import the new package in `pkg/channels/` (Go's `init()` auto-registers).
2. Add configuration fields in `pkg/config/config.go`, defaults if needed, and `config/config.example.json`.
3. Run `go test ./pkg/channels/<platform>/...`.
4. Start PicoClaw and verify the channel appears in logs and, if applicable, on the shared webhook server.
