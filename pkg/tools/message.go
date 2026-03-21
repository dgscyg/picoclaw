package tools

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

type SendCallback func(ctx context.Context, channel, chatID, content string) error

type MessageTool struct {
	sendCallback SendCallback
	sentInRound  sync.Map // map[roundID]struct{}
}

func NewMessageTool() *MessageTool {
	return &MessageTool{}
}

var crossTargetSendPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)chat[_\s-]*id`),
	regexp.MustCompile(`通知\s*[^\s，,。.!！?？:"“”'']+`),
	regexp.MustCompile(`发送给\s*[^\s，,。.!！?？:"“”'']+`),
	regexp.MustCompile(`发给\s*[^\s，,。.!！?？:"“”'']+`),
	regexp.MustCompile(`告诉\s*[^\s，,。.!！?？:"“”'']+`),
	regexp.MustCompile(`提醒\s*[^\s，,。.!！?？:"“”'']+`),
	regexp.MustCompile(`转告\s*[^\s，,。.!！?？:"“”'']+`),
	regexp.MustCompile(`给\s*[^\s，,。.!！?？:"“”'']+\s*发送`),
}

func looksLikeCrossTargetSendRequest(userMessage, currentChatID string) bool {
	trimmed := strings.TrimSpace(userMessage)
	if trimmed == "" {
		return false
	}

	lowerMsg := strings.ToLower(trimmed)
	if currentChatID != "" && strings.Contains(lowerMsg, strings.ToLower(strings.TrimSpace(currentChatID))) {
		return false
	}

	selfTargetHints := []string{
		"给我", "发我", "通知我", "提醒我", "回复我", "向我", "当前会话", "这个聊天",
	}
	for _, hint := range selfTargetHints {
		if strings.Contains(trimmed, hint) {
			return false
		}
	}

	for _, pattern := range crossTargetSendPatterns {
		if pattern.MatchString(trimmed) {
			return true
		}
	}
	return false
}

func (t *MessageTool) Name() string {
	return "message"
}

func (t *MessageTool) Description() string {
	return "Send a plain message to the user on a chat channel. Use this when you want to communicate something. If you are sending to the current conversation, you may omit `chat_id` and `channel`. If you are sending to a different user or group, you must provide the exact target `chat_id` explicitly; do not silently reuse the current chat. If the user names another person or group and the exact `chat_id` may be stored in memory, recall/search memory first and then call this tool with that exact ID; if the mapping is still unknown, ask the user instead of guessing. For `wecom_official`, the normal in-context path can automatically switch from reply-stream editing to the official callback `response_url` when the 6-minute stream edit window has expired, so keep the current reply context unless you truly want a separate proactive message. Use `separate_message=true` only when you intentionally want an independent proactive ordinary message; that path sends an ordinary markdown message, not a template card, and does not reuse the current callback response_url. If the original thinking placeholder or reply stream is already stale, do not try to edit it manually; just send the normal message in the current reply context and let the channel deliver a new follow-up message. For enterprise WeCom template card messages or template-card updates, use the dedicated `wecom_card` tool instead of hand-writing raw `template_card` JSON here."
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
				"description": "Optional only when sending back to the current conversation. If the message should go to a different user or group, this exact target chat/user ID is required. If you do not know it, ask the user instead of defaulting to the current chat.",
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
func (t *MessageTool) ResetSentInRound(roundID string) {
	if strings.TrimSpace(roundID) == "" {
		return
	}
	t.sentInRound.Delete(roundID)
}

// HasSentInRound returns true if the message tool sent a message during the given round.
func (t *MessageTool) HasSentInRound(roundID string) bool {
	if strings.TrimSpace(roundID) == "" {
		return false
	}
	_, ok := t.sentInRound.Load(roundID)
	return ok
}

func (t *MessageTool) SetSendCallback(callback SendCallback) {
	t.sendCallback = callback
}

func (t *MessageTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	content, ok := args["content"].(string)
	if !ok {
		return &ToolResult{ForLLM: "content is required", IsError: true}
	}

	currentChannel := ToolChannel(ctx)
	currentChatID := ToolChatID(ctx)
	userMessage := ToolUserMessage(ctx)
	channel, _ := args["channel"].(string)
	chatID, _ := args["chat_id"].(string)

	if strings.TrimSpace(chatID) == "" && looksLikeCrossTargetSendRequest(userMessage, currentChatID) {
		return &ToolResult{
			ForLLM:  "chat_id is required when the user asks to notify or send a message to another user or group; do not default to the current chat. First recall/search memory for the exact contact mapping (for example a remembered chat_id, alias, or user ID). If no exact mapping is available, ask the user for the exact chat_id.",
			IsError: true,
		}
	}

	if channel == "" {
		channel = currentChannel
	}
	if chatID == "" {
		chatID = currentChatID
	}

	if channel == "" || chatID == "" {
		return &ToolResult{ForLLM: "No target channel/chat specified", IsError: true}
	}

	separateMessage, _ := args["separate_message"].(bool)
	if !separateMessage && currentChannel != "" && currentChatID != "" &&
		(channel != currentChannel || chatID != currentChatID) {
		// Cross-target sends are never part of the current reply chain and must
		// not mark the current conversation as already replied.
		separateMessage = true
	}
	if !separateMessage &&
		channel == "wecom_official" &&
		strings.TrimSpace(ToolReplyTo(ctx)) != "" &&
		t.HasSentInRound(ToolRoundID(ctx)) {
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
		if roundID := ToolRoundID(ctx); strings.TrimSpace(roundID) != "" {
			t.sentInRound.Store(roundID, struct{}{})
		}
	}
	// Silent: user already received the message directly
	return &ToolResult{
		ForLLM:              fmt.Sprintf("Message sent to %s:%s", channel, chatID),
		Silent:              true,
		ConsumesCurrentTurn: !separateMessage,
	}
}
