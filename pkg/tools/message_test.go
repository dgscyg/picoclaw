package tools

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestMessageTool_Execute_Success(t *testing.T) {
	tool := NewMessageTool()

	var sentChannel, sentChatID, sentContent, sentReplyTo string
	tool.SetSendCallback(func(ctx context.Context, channel, chatID, content string) error {
		sentChannel = channel
		sentChatID = chatID
		sentContent = content
		sentReplyTo = ToolReplyTo(ctx)
		return nil
	})

	ctx := WithToolRoutingContext(context.Background(), "test-channel", "test-chat-id", "reply-1")
	args := map[string]any{
		"content": "Hello, world!",
	}

	result := tool.Execute(ctx, args)

	// Verify message was sent with correct parameters
	if sentChannel != "test-channel" {
		t.Errorf("Expected channel 'test-channel', got '%s'", sentChannel)
	}
	if sentChatID != "test-chat-id" {
		t.Errorf("Expected chatID 'test-chat-id', got '%s'", sentChatID)
	}
	if sentContent != "Hello, world!" {
		t.Errorf("Expected content 'Hello, world!', got '%s'", sentContent)
	}
	if sentReplyTo != "reply-1" {
		t.Errorf("Expected replyTo 'reply-1', got '%s'", sentReplyTo)
	}

	// Verify ToolResult meets US-011 criteria:
	// - Send success returns SilentResult (Silent=true)
	if !result.Silent {
		t.Error("Expected Silent=true for successful send")
	}
	if !result.ConsumesCurrentTurn {
		t.Error("Expected ConsumesCurrentTurn=true for current conversation send")
	}

	// - ForLLM contains send status description
	if result.ForLLM != "Message sent to test-channel:test-chat-id" {
		t.Errorf("Expected ForLLM 'Message sent to test-channel:test-chat-id', got '%s'", result.ForLLM)
	}

	// - ForUser is empty (user already received message directly)
	if result.ForUser != "" {
		t.Errorf("Expected ForUser to be empty, got '%s'", result.ForUser)
	}

	// - IsError should be false
	if result.IsError {
		t.Error("Expected IsError=false for successful send")
	}
}

func TestMessageTool_Execute_WithCustomChannel(t *testing.T) {
	tool := NewMessageTool()

	var sentChannel, sentChatID string
	tool.SetSendCallback(func(_ context.Context, channel, chatID, content string) error {
		sentChannel = channel
		sentChatID = chatID
		return nil
	})

	ctx := WithToolContext(context.Background(), "default-channel", "default-chat-id")
	args := map[string]any{
		"content": "Test message",
		"channel": "custom-channel",
		"chat_id": "custom-chat-id",
	}

	result := tool.Execute(ctx, args)

	// Verify custom channel/chatID were used instead of defaults
	if sentChannel != "custom-channel" {
		t.Errorf("Expected channel 'custom-channel', got '%s'", sentChannel)
	}
	if sentChatID != "custom-chat-id" {
		t.Errorf("Expected chatID 'custom-chat-id', got '%s'", sentChatID)
	}

	if !result.Silent {
		t.Error("Expected Silent=true")
	}
	if result.ConsumesCurrentTurn {
		t.Error("Expected ConsumesCurrentTurn=false for cross-target send")
	}
	if result.ForLLM != "Message sent to custom-channel:custom-chat-id" {
		t.Errorf("Expected ForLLM 'Message sent to custom-channel:custom-chat-id', got '%s'", result.ForLLM)
	}
}

func TestMessageTool_Execute_SeparateMessageClearsReplyTo(t *testing.T) {
	tool := NewMessageTool()
	const roundID = "round-separate"

	var sentReplyTo string
	tool.SetSendCallback(func(ctx context.Context, channel, chatID, content string) error {
		sentReplyTo = ToolReplyTo(ctx)
		return nil
	})

	ctx := WithToolRoundID(
		WithToolRoutingContext(context.Background(), "wecom_official", "YangXu", "callback-1"),
		roundID,
	)
	args := map[string]any{
		"content":          "independent",
		"separate_message": true,
	}

	result := tool.Execute(ctx, args)
	if !result.Silent {
		t.Fatal("expected silent result")
	}
	if sentReplyTo != "" {
		t.Fatalf("expected empty replyTo for separate message, got %q", sentReplyTo)
	}
	if result.ConsumesCurrentTurn {
		t.Fatal("separate_message should not consume the current turn")
	}
	if tool.HasSentInRound(roundID) {
		t.Fatal("separate_message should not mark the round as already replied")
	}
}

func TestMessageTool_Execute_WeComOfficialSecondSendClearsReplyTo(t *testing.T) {
	tool := NewMessageTool()
	const roundID = "round-second-send"

	replyTos := make([]string, 0, 2)
	tool.SetSendCallback(func(ctx context.Context, channel, chatID, content string) error {
		replyTos = append(replyTos, ToolReplyTo(ctx))
		return nil
	})

	ctx := WithToolRoundID(
		WithToolRoutingContext(context.Background(), "wecom_official", "YangXu", "callback-1"),
		roundID,
	)
	first := tool.Execute(ctx, map[string]any{"content": "3"})
	if first.IsError {
		t.Fatalf("first execute error: %v", first.ForLLM)
	}
	second := tool.Execute(ctx, map[string]any{"content": "2"})
	if second.IsError {
		t.Fatalf("second execute error: %v", second.ForLLM)
	}

	if len(replyTos) != 2 {
		t.Fatalf("expected 2 sends, got %d", len(replyTos))
	}
	if got, want := replyTos[0], "callback-1"; got != want {
		t.Fatalf("first replyTo = %q, want %q", got, want)
	}
	if got := replyTos[1]; got != "" {
		t.Fatalf("second replyTo = %q, want empty", got)
	}
}

func TestMessageTool_Execute_CrossTargetSendDoesNotMarkRoundReplied(t *testing.T) {
	tool := NewMessageTool()
	const roundID = "round-cross-target"

	var sentReplyTo string
	tool.SetSendCallback(func(ctx context.Context, channel, chatID, content string) error {
		sentReplyTo = ToolReplyTo(ctx)
		return nil
	})

	ctx := WithToolRoundID(
		WithToolRoutingContext(context.Background(), "wecom_official", "dragonsss", "callback-1"),
		roundID,
	)
	result := tool.Execute(ctx, map[string]any{
		"content": "任务进度",
		"chat_id": "WangCheng",
	})

	if result.IsError {
		t.Fatalf("execute error: %v", result.ForLLM)
	}
	if sentReplyTo != "" {
		t.Fatalf("expected cross-target send to clear replyTo, got %q", sentReplyTo)
	}
	if result.ConsumesCurrentTurn {
		t.Fatal("cross-target send should not consume the current turn")
	}
	if tool.HasSentInRound(roundID) {
		t.Fatal("cross-target send should not mark the current round as already replied")
	}
}

func TestMessageTool_Execute_CrossTargetRequestWithoutChatIDReturnsError(t *testing.T) {
	tool := NewMessageTool()

	ctx := WithToolUserMessage(
		WithToolRoutingContext(context.Background(), "wecom_official", "dragonsss", "callback-1"),
		`通知王成，给他发送 "123"`,
	)
	result := tool.Execute(ctx, map[string]any{
		"content": "王成，123",
	})

	if !result.IsError {
		t.Fatal("expected error when cross-target request omits chat_id")
	}
	if !strings.Contains(result.ForLLM, "chat_id is required") {
		t.Fatalf("unexpected error: %q", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "recall/search memory") {
		t.Fatalf("expected memory lookup hint, got %q", result.ForLLM)
	}
}

func TestMessageTool_Execute_CurrentConversationWithoutChatIDStillAllowed(t *testing.T) {
	tool := NewMessageTool()

	var sentChannel, sentChatID, sentContent string
	tool.SetSendCallback(func(_ context.Context, channel, chatID, content string) error {
		sentChannel = channel
		sentChatID = chatID
		sentContent = content
		return nil
	})

	ctx := WithToolUserMessage(
		WithToolRoutingContext(context.Background(), "wecom_official", "dragonsss", "callback-1"),
		`给我发送 "123"`,
	)
	result := tool.Execute(ctx, map[string]any{
		"content": "123",
	})

	if result.IsError {
		t.Fatalf("unexpected error: %v", result.ForLLM)
	}
	if got, want := sentChannel, "wecom_official"; got != want {
		t.Fatalf("channel = %q, want %q", got, want)
	}
	if got, want := sentChatID, "dragonsss"; got != want {
		t.Fatalf("chatID = %q, want %q", got, want)
	}
	if got, want := sentContent, "123"; got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
}

func TestMessageTool_Execute_SendFailure(t *testing.T) {
	tool := NewMessageTool()

	sendErr := errors.New("network error")
	tool.SetSendCallback(func(_ context.Context, channel, chatID, content string) error {
		return sendErr
	})

	ctx := WithToolContext(context.Background(), "test-channel", "test-chat-id")
	args := map[string]any{
		"content": "Test message",
	}

	result := tool.Execute(ctx, args)

	// Verify ToolResult for send failure:
	// - Send failure returns ErrorResult (IsError=true)
	if !result.IsError {
		t.Error("Expected IsError=true for failed send")
	}

	// - ForLLM contains error description
	expectedErrMsg := "sending message: network error"
	if result.ForLLM != expectedErrMsg {
		t.Errorf("Expected ForLLM '%s', got '%s'", expectedErrMsg, result.ForLLM)
	}

	// - Err field should contain original error
	if result.Err == nil {
		t.Error("Expected Err to be set")
	}
	if result.Err != sendErr {
		t.Errorf("Expected Err to be sendErr, got %v", result.Err)
	}
}

func TestMessageTool_Execute_MissingContent(t *testing.T) {
	tool := NewMessageTool()

	ctx := WithToolContext(context.Background(), "test-channel", "test-chat-id")
	args := map[string]any{} // content missing

	result := tool.Execute(ctx, args)

	// Verify error result for missing content
	if !result.IsError {
		t.Error("Expected IsError=true for missing content")
	}
	if result.ForLLM != "content is required" {
		t.Errorf("Expected ForLLM 'content is required', got '%s'", result.ForLLM)
	}
}

func TestMessageTool_Execute_NoTargetChannel(t *testing.T) {
	tool := NewMessageTool()
	// No WithToolContext — channel/chatID are empty

	tool.SetSendCallback(func(_ context.Context, channel, chatID, content string) error {
		return nil
	})

	ctx := context.Background()
	args := map[string]any{
		"content": "Test message",
	}

	result := tool.Execute(ctx, args)

	// Verify error when no target channel specified
	if !result.IsError {
		t.Error("Expected IsError=true when no target channel")
	}
	if result.ForLLM != "No target channel/chat specified" {
		t.Errorf("Expected ForLLM 'No target channel/chat specified', got '%s'", result.ForLLM)
	}
}

func TestMessageTool_Execute_NotConfigured(t *testing.T) {
	tool := NewMessageTool()
	// No SetSendCallback called

	ctx := WithToolContext(context.Background(), "test-channel", "test-chat-id")
	args := map[string]any{
		"content": "Test message",
	}

	result := tool.Execute(ctx, args)

	// Verify error when send callback not configured
	if !result.IsError {
		t.Error("Expected IsError=true when send callback not configured")
	}
	if result.ForLLM != "Message sending not configured" {
		t.Errorf("Expected ForLLM 'Message sending not configured', got '%s'", result.ForLLM)
	}
}

func TestMessageTool_Name(t *testing.T) {
	tool := NewMessageTool()
	if tool.Name() != "message" {
		t.Errorf("Expected name 'message', got '%s'", tool.Name())
	}
}

func TestMessageTool_Description(t *testing.T) {
	tool := NewMessageTool()
	desc := tool.Description()
	if desc == "" {
		t.Error("Description should not be empty")
	}
	if !strings.Contains(desc, "template_card") {
		t.Error("Description should mention template_card direct sending for wecom_official")
	}
	if !strings.Contains(desc, "exact target `chat_id` explicitly") {
		t.Error("Description should mention explicit chat_id requirement for cross-target sends")
	}
	if !strings.Contains(desc, "recall/search memory first") {
		t.Error("Description should instruct recalling/searching memory before cross-target sends")
	}
}

func TestMessageTool_Parameters(t *testing.T) {
	tool := NewMessageTool()
	params := tool.Parameters()

	// Verify parameters structure
	typ, ok := params["type"].(string)
	if !ok || typ != "object" {
		t.Error("Expected type 'object'")
	}

	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatal("Expected properties to be a map")
	}

	// Check required properties
	required, ok := params["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "content" {
		t.Error("Expected 'content' to be required")
	}

	// Check content property
	contentProp, ok := props["content"].(map[string]any)
	if !ok {
		t.Error("Expected 'content' property")
	}
	if contentProp["type"] != "string" {
		t.Error("Expected content type to be 'string'")
	}

	// Check channel property (optional)
	channelProp, ok := props["channel"].(map[string]any)
	if !ok {
		t.Error("Expected 'channel' property")
	}
	if channelProp["type"] != "string" {
		t.Error("Expected channel type to be 'string'")
	}

	// Check chat_id property (optional)
	chatIDProp, ok := props["chat_id"].(map[string]any)
	if !ok {
		t.Error("Expected 'chat_id' property")
	}
	if chatIDProp["type"] != "string" {
		t.Error("Expected chat_id type to be 'string'")
	}
}
