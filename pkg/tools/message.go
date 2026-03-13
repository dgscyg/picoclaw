package tools

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
)

type SendCallback func(ctx context.Context, channel, chatID, content string) error

type MessageTool struct {
	sendCallback SendCallback
	sentInRound  atomic.Bool // Tracks whether a reply-context message was sent in the current processing round
}

func NewMessageTool() *MessageTool {
	return &MessageTool{}
}

func (t *MessageTool) Name() string {
	return "message"
}

func (t *MessageTool) Description() string {
	return "Send a plain message to the user on a chat channel. Use this when you want to communicate something. For `wecom_official`, the normal in-context path can automatically switch from reply-stream editing to the official callback `response_url` when the 6-minute stream edit window has expired, so keep the current reply context unless you truly want a separate proactive message. Use `separate_message=true` only when you intentionally want an independent proactive ordinary message; that path sends an ordinary markdown message, not a template card, and does not reuse the current callback response_url. If the original thinking placeholder or reply stream is already stale, do not try to edit it manually; just send the normal message in the current reply context and let the channel deliver a new follow-up message. For enterprise WeCom template card messages or template-card updates, use the dedicated `wecom_card` tool instead of hand-writing raw `template_card` JSON here."
}

func (t *MessageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content": map[string]any{
				"type":        "string",
				"description": "The plain text or markdown message content to send. For enterprise WeCom template cards or template-card updates, use the `wecom_card` tool instead.",
			},
			"channel": map[string]any{
				"type":        "string",
				"description": "Optional: target channel (telegram, whatsapp, etc.)",
			},
			"chat_id": map[string]any{
				"type":        "string",
				"description": "Optional: target chat/user ID",
			},
			"separate_message": map[string]any{
				"type":        "boolean",
				"description": "Optional: when true, send as an independent message instead of reusing the current reply context. For `wecom_official`, do not set this just because the original reply stream is old; keeping the current reply context lets the channel fall back to the official callback response_url. Use `separate_message=true` only when you explicitly want a brand-new proactive message unrelated to the current callback reply chain.",
			},
		},
		"required": []string{"content"},
	}
}

// ResetSentInRound resets the per-round send tracker.
// Called by the agent loop at the start of each inbound message processing round.
func (t *MessageTool) ResetSentInRound() {
	t.sentInRound.Store(false)
}

// HasSentInRound returns true if the message tool sent a message during the current round.
func (t *MessageTool) HasSentInRound() bool {
	return t.sentInRound.Load()
}

func (t *MessageTool) SetSendCallback(callback SendCallback) {
	t.sendCallback = callback
}

func (t *MessageTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	content, ok := args["content"].(string)
	if !ok {
		return &ToolResult{ForLLM: "content is required", IsError: true}
	}

	channel, _ := args["channel"].(string)
	chatID, _ := args["chat_id"].(string)

	if channel == "" {
		channel = ToolChannel(ctx)
	}
	if chatID == "" {
		chatID = ToolChatID(ctx)
	}

	if channel == "" || chatID == "" {
		return &ToolResult{ForLLM: "No target channel/chat specified", IsError: true}
	}

	separateMessage, _ := args["separate_message"].(bool)
	if !separateMessage &&
		channel == "wecom_official" &&
		strings.TrimSpace(ToolReplyTo(ctx)) != "" &&
		t.sentInRound.Load() {
		// The first in-round send may legitimately replace the callback placeholder,
		// but additional sends in the same round must break out as independent
		// messages instead of repeatedly editing the same WeCom reply stream.
		separateMessage = true
	}
	if separateMessage {
		ctx = WithToolRoutingContext(ctx, channel, chatID, "")
	}

	if t.sendCallback == nil {
		return &ToolResult{ForLLM: "Message sending not configured", IsError: true}
	}

	if err := t.sendCallback(ctx, channel, chatID, content); err != nil {
		return &ToolResult{
			ForLLM:  fmt.Sprintf("sending message: %v", err),
			IsError: true,
			Err:     err,
		}
	}

	if !separateMessage {
		t.sentInRound.Store(true)
	}
	// Silent: user already received the message directly
	return &ToolResult{
		ForLLM: fmt.Sprintf("Message sent to %s:%s", channel, chatID),
		Silent: true,
	}
}
