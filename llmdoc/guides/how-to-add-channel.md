# How to Add a New Channel

This guide describes how to implement a new messaging platform channel in PicoClaw.

## 1. Create Channel Package

Create a new directory under `pkg/channels/<platform>/` with a main implementation file.

## 2. Implement Channel Interface

Implement the `Channel` interface defined in `pkg/channels/base.go`:

```go
type Channel interface {
    Name() string
    Start() error
    Stop() error
    Send(chatID, content string, replyTo *string) error
    IsRunning() bool
    IsAllowed(chatID string) bool
    IsAllowedSender(sender *bus.SenderInfo) bool
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

- **TypingCapable:** `StartTyping(chatID string) (stop func())`
- **MessageEditor:** `EditMessage(chatID, messageID, content string) error`
- **ReactionCapable:** `ReactToMessage(chatID, messageID string) (undo func(), err error)`
- **PlaceholderCapable:** `SendPlaceholder(chatID string) (messageID string, err error)`
- **MediaSender:** `SendMedia(chatID string, parts []*media.MediaPart) error`
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

Call `BaseChannel.HandleMessage` for inbound messages:

```go
func (c *MyChannel) handleIncoming(msg *PlatformMessage) {
    inbound := &bus.InboundMessage{
        Channel:   c.Name(),
        SenderID:  msg.SenderID,
        Sender:    &bus.SenderInfo{...},
        ChatID:    msg.ChatID,
        Content:   msg.Text,
        MessageID: msg.ID,
    }
    c.HandleMessage(inbound)
}
```

Reference: `pkg/channels/base.go:HandleMessage` for allowlist and capability auto-triggering.

## 6. Add Rate Limit Configuration

Update `channelRateConfig` in `pkg/channels/manager.go` with appropriate rate limit for the platform.

## 7. Verify Integration

1. Import the new package in `pkg/channels/` (Go's `init()` auto-registers).
2. Add configuration section in `config.yaml`.
3. Run `go test ./pkg/channels/<platform>/...`.
4. Start PicoClaw and verify channel appears in logs.
