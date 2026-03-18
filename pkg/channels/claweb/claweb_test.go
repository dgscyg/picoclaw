package claweb

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/media"
)

func TestClawebInboundPublishesMessage(t *testing.T) {
	t.Parallel()

	msgBus := bus.NewMessageBus()
	ch := newStartedTestChannel(t, msgBus)
	conn := dialTestChannel(t, ch)
	defer conn.Close()

	mustWriteJSON(t, conn, clawebHelloFrame{
		Type:     "hello",
		Token:    "secret-token",
		ClientID: "client-1",
		UserID:   "user-1",
	})

	var ready clawebReadyFrame
	mustReadJSON(t, conn, &ready)
	if ready.Type != "ready" {
		t.Fatalf("ready.Type = %q, want ready", ready.Type)
	}

	mustWriteJSON(t, conn, clawebMessageFrame{
		Type: "message",
		ID:   "turn-1",
		Text: "hello from claweb",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got, ok := msgBus.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("ConsumeInbound timed out")
	}

	if got.Channel != clawebChannelName {
		t.Fatalf("InboundMessage.Channel = %q, want %q", got.Channel, clawebChannelName)
	}
	if got.Content != "hello from claweb" {
		t.Fatalf("InboundMessage.Content = %q, want %q", got.Content, "hello from claweb")
	}
	if got.Metadata["reply_to"] != "turn-1" {
		t.Fatalf("InboundMessage.Metadata[reply_to] = %q, want %q", got.Metadata["reply_to"], "turn-1")
	}
	if got.ChatID != buildChatID("user-1", "", "client-1") {
		t.Fatalf("InboundMessage.ChatID = %q, want %q", got.ChatID, buildChatID("user-1", "", "client-1"))
	}
	if got.Sender.CanonicalID != "claweb:user-1" {
		t.Fatalf("InboundMessage.Sender.CanonicalID = %q, want %q", got.Sender.CanonicalID, "claweb:user-1")
	}
}

func TestClawebInboundIgnoresAssistantRoleFrames(t *testing.T) {
	t.Parallel()

	msgBus := bus.NewMessageBus()
	ch := newStartedTestChannel(t, msgBus)
	conn := dialTestChannel(t, ch)
	defer conn.Close()

	mustWriteJSON(t, conn, clawebHelloFrame{
		Type:     "hello",
		Token:    "secret-token",
		ClientID: "client-role",
		UserID:   "user-role",
	})

	var ready clawebReadyFrame
	mustReadJSON(t, conn, &ready)

	mustWriteJSON(t, conn, clawebMessageFrame{
		Type: "message",
		ID:   "assistant-echo",
		Role: "assistant",
		Text: "echoed assistant content",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if got, ok := msgBus.ConsumeInbound(ctx); ok {
		t.Fatalf("unexpected inbound message: %#v", got)
	}
}

func TestClawebSendUsesReplyToAsFrameID(t *testing.T) {
	t.Parallel()

	msgBus := bus.NewMessageBus()
	ch := newStartedTestChannel(t, msgBus)
	conn := dialTestChannel(t, ch)
	defer conn.Close()

	mustWriteJSON(t, conn, clawebHelloFrame{
		Type:     "hello",
		Token:    "secret-token",
		ClientID: "client-2",
		UserID:   "user-2",
	})

	var ready clawebReadyFrame
	mustReadJSON(t, conn, &ready)

	chatID := buildChatID("user-2", "", "client-2")
	if err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: clawebChannelName,
		ChatID:  chatID,
		Content: "assistant reply",
		ReplyTo: "turn-2",
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	var frame clawebMessageFrame
	mustReadJSON(t, conn, &frame)
	if frame.Type != "message" {
		t.Fatalf("frame.Type = %q, want message", frame.Type)
	}
	if frame.ID != "turn-2" {
		t.Fatalf("frame.ID = %q, want %q", frame.ID, "turn-2")
	}
	if frame.MessageID != "turn-2" {
		t.Fatalf("frame.MessageID = %q, want %q", frame.MessageID, "turn-2")
	}
	if frame.Role != "assistant" {
		t.Fatalf("frame.Role = %q, want assistant", frame.Role)
	}
	if frame.Text != "assistant reply" {
		t.Fatalf("frame.Text = %q, want %q", frame.Text, "assistant reply")
	}
}

func TestClawebSend_SuppressesDuplicateAssistantFrameForSameTurn(t *testing.T) {
	t.Parallel()

	msgBus := bus.NewMessageBus()
	ch := newStartedTestChannel(t, msgBus)
	conn := dialTestChannel(t, ch)
	defer conn.Close()

	mustWriteJSON(t, conn, clawebHelloFrame{
		Type:     "hello",
		Token:    "secret-token",
		ClientID: "client-dup",
		UserID:   "user-dup",
	})

	var ready clawebReadyFrame
	mustReadJSON(t, conn, &ready)

	chatID := buildChatID("user-dup", "", "client-dup")
	if err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: clawebChannelName,
		ChatID:  chatID,
		Content: "first reply",
		ReplyTo: "turn-dup-1",
	}); err != nil {
		t.Fatalf("first Send() error = %v", err)
	}
	var first clawebMessageFrame
	mustReadJSON(t, conn, &first)
	if got, want := first.Text, "first reply"; got != want {
		t.Fatalf("first frame.Text = %q, want %q", got, want)
	}

	if err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: clawebChannelName,
		ChatID:  chatID,
		Content: "second reply should be suppressed",
		ReplyTo: "turn-dup-1",
	}); err != nil {
		t.Fatalf("second Send() error = %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
	var second clawebMessageFrame
	if err := conn.ReadJSON(&second); err == nil {
		t.Fatalf("unexpected duplicate frame: %#v", second)
	}
	_ = conn.SetReadDeadline(time.Time{})
}

func TestClawebSend_AllowsNewAssistantFrameAfterNewInboundTurn(t *testing.T) {
	t.Parallel()

	msgBus := bus.NewMessageBus()
	ch := newStartedTestChannel(t, msgBus)
	conn := dialTestChannel(t, ch)
	defer conn.Close()

	mustWriteJSON(t, conn, clawebHelloFrame{
		Type:     "hello",
		Token:    "secret-token",
		ClientID: "client-next",
		UserID:   "user-next",
	})

	var ready clawebReadyFrame
	mustReadJSON(t, conn, &ready)

	mustWriteJSON(t, conn, clawebMessageFrame{
		Type: "message",
		ID:   "turn-user-1",
		Text: "hello",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, ok := msgBus.ConsumeInbound(ctx); !ok {
		t.Fatal("expected inbound message")
	}

	chatID := buildChatID("user-next", "", "client-next")
	if err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: clawebChannelName,
		ChatID:  chatID,
		Content: "reply one",
		ReplyTo: "turn-user-1",
	}); err != nil {
		t.Fatalf("Send() first turn error = %v", err)
	}
	var first clawebMessageFrame
	mustReadJSON(t, conn, &first)

	mustWriteJSON(t, conn, clawebMessageFrame{
		Type: "message",
		ID:   "turn-user-2",
		Text: "hello again",
	})

	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	if _, ok := msgBus.ConsumeInbound(ctx2); !ok {
		t.Fatal("expected second inbound message")
	}

	if err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: clawebChannelName,
		ChatID:  chatID,
		Content: "reply two",
		ReplyTo: "turn-user-2",
	}); err != nil {
		t.Fatalf("Send() second turn error = %v", err)
	}
	var second clawebMessageFrame
	mustReadJSON(t, conn, &second)
	if got, want := second.ID, "turn-user-2"; got != want {
		t.Fatalf("second frame.ID = %q, want %q", got, want)
	}
	if got, want := second.Text, "reply two"; got != want {
		t.Fatalf("second frame.Text = %q, want %q", got, want)
	}
}

func TestClawebSendMediaUsesResolvedRef(t *testing.T) {
	t.Parallel()

	msgBus := bus.NewMessageBus()
	ch := newStartedTestChannel(t, msgBus)
	store := media.NewFileMediaStore()
	ch.SetMediaStore(store)

	conn := dialTestChannel(t, ch)
	defer conn.Close()

	mustWriteJSON(t, conn, clawebHelloFrame{
		Type:     "hello",
		Token:    "secret-token",
		ClientID: "client-3",
		UserID:   "user-3",
	})

	var ready clawebReadyFrame
	mustReadJSON(t, conn, &ready)

	tempDir := t.TempDir()
	localPath := filepath.Join(tempDir, "report.txt")
	if err := os.WriteFile(localPath, []byte("claweb attachment"), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	ref, err := store.Store(localPath, media.MediaMeta{
		Filename:    "report.txt",
		ContentType: "text/plain",
		Source:      "test",
	}, "scope-1")
	if err != nil {
		t.Fatalf("store.Store() error = %v", err)
	}

	chatID := buildChatID("user-3", "", "client-3")
	if err := ch.SendMedia(context.Background(), bus.OutboundMediaMessage{
		Channel: clawebChannelName,
		ChatID:  chatID,
		Parts: []bus.MediaPart{{
			Type:        "file",
			Ref:         ref,
			Caption:     "see attachment",
			Filename:    "report.txt",
			ContentType: "text/plain",
		}},
	}); err != nil {
		t.Fatalf("SendMedia() error = %v", err)
	}

	var frame clawebMessageFrame
	mustReadJSON(t, conn, &frame)
	if frame.Type != "message" {
		t.Fatalf("frame.Type = %q, want message", frame.Type)
	}
	if frame.Text != "see attachment" {
		t.Fatalf("frame.Text = %q, want %q", frame.Text, "see attachment")
	}
	if frame.MediaURL != localPath {
		t.Fatalf("frame.MediaURL = %q, want %q", frame.MediaURL, localPath)
	}
	if frame.MediaFilename != "report.txt" {
		t.Fatalf("frame.MediaFilename = %q, want %q", frame.MediaFilename, "report.txt")
	}
	if frame.MediaType != "text/plain" {
		t.Fatalf("frame.MediaType = %q, want %q", frame.MediaType, "text/plain")
	}
	if strings.TrimSpace(frame.ID) == "" {
		t.Fatal("frame.ID is empty")
	}
}

func newStartedTestChannel(t *testing.T, msgBus *bus.MessageBus) *ClawebChannel {
	t.Helper()

	ch, err := NewClawebChannel(config.ClawebConfig{
		Enabled:    true,
		ListenHost: "127.0.0.1",
		ListenPort: freeTestPort(t),
		AuthToken:  "secret-token",
	}, msgBus)
	if err != nil {
		t.Fatalf("NewClawebChannel() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = ch.Stop(stopCtx)
	})

	if err := ch.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return ch
}

func freeTestPort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() error = %v", err)
	}
	defer listener.Close()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener.Addr() = %T, want *net.TCPAddr", listener.Addr())
	}
	return addr.Port
}

func dialTestChannel(t *testing.T, ch *ClawebChannel) *websocket.Conn {
	t.Helper()

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	url := "ws://" + ch.listenAddr
	conn, _, err := dialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("Dial(%s) error = %v", url, err)
	}
	return conn
}

func mustWriteJSON(t *testing.T, conn *websocket.Conn, v any) {
	t.Helper()

	if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetWriteDeadline() error = %v", err)
	}
	if err := conn.WriteJSON(v); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
	_ = conn.SetWriteDeadline(time.Time{})
}

func mustReadJSON(t *testing.T, conn *websocket.Conn, out any) {
	t.Helper()

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	if err := conn.ReadJSON(out); err != nil {
		t.Fatalf("ReadJSON() error = %v", err)
	}
	_ = conn.SetReadDeadline(time.Time{})
}
