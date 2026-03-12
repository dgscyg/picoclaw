package wecom

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
)

func boolPtr(v bool) *bool { return &v }

type wecomOfficialTestServer struct {
	t        *testing.T
	server   *httptest.Server
	wsURL    string
	authCh   chan struct{}
	sendCh   chan map[string]any
	replyCh  chan wecomOfficialFrame
	onAuth   func(conn *websocket.Conn)
	authOnce sync.Once
}

func newWeComOfficialTestServer(
	t *testing.T,
	onAuth func(conn *websocket.Conn),
) *wecomOfficialTestServer {
	t.Helper()

	s := &wecomOfficialTestServer{
		t:       t,
		authCh:  make(chan struct{}, 1),
		sendCh:  make(chan map[string]any, 4),
		replyCh: make(chan wecomOfficialFrame, 8),
		onAuth:  onAuth,
	}

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}

		go s.handleConn(conn)
	}))

	s.wsURL = "ws" + strings.TrimPrefix(s.server.URL, "http")
	return s
}

func (s *wecomOfficialTestServer) close() {
	s.server.Close()
}

func (s *wecomOfficialTestServer) waitAuth(t *testing.T) {
	t.Helper()
	select {
	case <-s.authCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for websocket authentication")
	}
}

func (s *wecomOfficialTestServer) waitSend(t *testing.T) map[string]any {
	t.Helper()
	select {
	case payload := <-s.sendCh:
		return payload
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for proactive send")
		return nil
	}
}

func (s *wecomOfficialTestServer) waitReply(t *testing.T) wecomOfficialFrame {
	t.Helper()
	select {
	case frame := <-s.replyCh:
		return frame
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for reply stream frame")
		return wecomOfficialFrame{}
	}
}

func (s *wecomOfficialTestServer) handleConn(conn *websocket.Conn) {
	defer conn.Close()

	for {
		var frame wecomOfficialFrame
		if err := conn.ReadJSON(&frame); err != nil {
			return
		}

		switch frame.Cmd {
		case wecomOfficialCmdSubscribe:
			s.writeAck(conn, frame.Headers.ReqID)
			s.authOnce.Do(func() {
				s.authCh <- struct{}{}
				if s.onAuth != nil {
					go s.onAuth(conn)
				}
			})
		case wecomOfficialCmdHeartbeat:
			s.writeAck(conn, frame.Headers.ReqID)
		case wecomOfficialCmdRespondMessage, wecomOfficialCmdRespondWelcome:
			s.replyCh <- frame
			s.writeAck(conn, frame.Headers.ReqID)
		case wecomOfficialCmdSendMessage:
			var payload map[string]any
			if err := json.Unmarshal(frame.Body, &payload); err != nil {
				s.t.Errorf("unmarshal send payload: %v", err)
				return
			}
			s.sendCh <- payload
			s.writeAck(conn, frame.Headers.ReqID)
		default:
			s.t.Errorf("unexpected client cmd: %s", frame.Cmd)
			return
		}
	}
}

func (s *wecomOfficialTestServer) writeAck(conn *websocket.Conn, reqID string) {
	s.t.Helper()
	if err := conn.WriteJSON(wecomOfficialFrame{
		Headers: wecomOfficialHeaders{ReqID: reqID},
		ErrCode: 0,
		ErrMsg:  "ok",
	}); err != nil {
		s.t.Errorf("write ack: %v", err)
	}
}

func TestNewWeComOfficialChannel(t *testing.T) {
	msgBus := bus.NewMessageBus()

	t.Run("requires bot id and secret", func(t *testing.T) {
		_, err := NewWeComOfficialChannel(config.WeComOfficialConfig{}, msgBus)
		if err == nil {
			t.Fatal("expected constructor to reject empty bot credentials")
		}
	})

	t.Run("defaults websocket url", func(t *testing.T) {
		ch, err := NewWeComOfficialChannel(config.WeComOfficialConfig{
			BotID:  "bot-id",
			Secret: "bot-secret",
		}, msgBus)
		if err != nil {
			t.Fatalf("NewWeComOfficialChannel() error = %v", err)
		}
		if ch.config.WebSocketURL != wecomOfficialDefaultWSURL {
			t.Fatalf("WebSocketURL = %q, want %q", ch.config.WebSocketURL, wecomOfficialDefaultWSURL)
		}
	})
}

func TestBuildWeComOfficialTemplateCardNormalizesMarkdown(t *testing.T) {
	card := buildWeComOfficialTemplateCard("Ops Bot", "# Heading\n\n**bold**\n> quoted")

	raw, err := json.Marshal(card)
	if err != nil {
		t.Fatalf("marshal card: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal card: %v", err)
	}

	if got, want := payload["card_type"], "text_notice"; got != want {
		t.Fatalf("card_type = %v, want %v", got, want)
	}
	if got, want := payload["sub_title_text"], "Heading\n\nbold\nquoted"; got != want {
		t.Fatalf("sub_title_text = %v, want %v", got, want)
	}
	quoteArea, ok := payload["quote_area"].(map[string]any)
	if !ok {
		t.Fatalf("quote_area missing or wrong type: %#v", payload["quote_area"])
	}
	if got, want := quoteArea["quote_text"], "Heading\n\nbold\nquoted"; got != want {
		t.Fatalf("quote_area.quote_text = %v, want %v", got, want)
	}
}

func TestDecodeWeComOfficialMediaAESKeyAcceptsUnpaddedBase64(t *testing.T) {
	rawKey := []byte("0123456789abcdef0123456789abcdef")
	encoded := strings.TrimRight(base64.StdEncoding.EncodeToString(rawKey), "=")

	decoded, err := decodeWeComOfficialMediaAESKey(encoded)
	if err != nil {
		t.Fatalf("decodeWeComOfficialMediaAESKey() error = %v", err)
	}
	if string(decoded) != string(rawKey) {
		t.Fatalf("decoded key mismatch: got %x want %x", decoded, rawKey)
	}
}

func TestWeComOfficialSend(t *testing.T) {
	server := newWeComOfficialTestServer(t, nil)
	defer server.close()

	msgBus := bus.NewMessageBus()
	ch, err := NewWeComOfficialChannel(config.WeComOfficialConfig{
		Enabled:             true,
		BotID:               "bot-id",
		Secret:              "bot-secret",
		WebSocketURL:        server.wsURL,
		SendThinkingMessage: boolPtr(false),
	}, msgBus)
	if err != nil {
		t.Fatalf("NewWeComOfficialChannel() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ch.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = ch.Stop(context.Background()) }()

	server.waitAuth(t)

	if err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "wecom_official",
		ChatID:  "chat-1",
		Content: "hello from picoclaw",
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	payload := server.waitSend(t)
	if got, want := payload["chatid"], "chat-1"; got != want {
		t.Fatalf("chatid = %v, want %v", got, want)
	}
	if got, want := payload["msgtype"], "markdown"; got != want {
		t.Fatalf("msgtype = %v, want %v", got, want)
	}

	markdown, ok := payload["markdown"].(map[string]any)
	if !ok {
		t.Fatalf("markdown payload missing or wrong type: %#v", payload["markdown"])
	}
	if got, want := markdown["content"], "hello from picoclaw"; got != want {
		t.Fatalf("markdown.content = %v, want %v", got, want)
	}
}

func TestWeComOfficialSendUsesTemplateCardWhenEnabled(t *testing.T) {
	server := newWeComOfficialTestServer(t, nil)
	defer server.close()

	msgBus := bus.NewMessageBus()
	ch, err := NewWeComOfficialChannel(config.WeComOfficialConfig{
		Enabled:      true,
		BotID:        "bot-id",
		Secret:       "bot-secret",
		WebSocketURL: server.wsURL,
		AllowFrom:    config.FlexibleStringSlice{"chat-card-1"},
		Card: config.CardConfig{
			Enabled: true,
			Title:   "Ops Bot",
		},
	}, msgBus)
	if err != nil {
		t.Fatalf("NewWeComOfficialChannel() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ch.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = ch.Stop(context.Background()) }()

	server.waitAuth(t)

	if err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "wecom_official",
		ChatID:  "chat-card-1",
		Content: "hello from card mode",
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	payload := server.waitSend(t)
	if got, want := payload["msgtype"], "template_card"; got != want {
		t.Fatalf("msgtype = %v, want %v", got, want)
	}

	card, ok := payload["template_card"].(map[string]any)
	if !ok {
		t.Fatalf("template_card missing or wrong type: %#v", payload["template_card"])
	}
	if got, want := card["card_type"], "text_notice"; got != want {
		t.Fatalf("card_type = %v, want %v", got, want)
	}
	mainTitle, ok := card["main_title"].(map[string]any)
	if !ok {
		t.Fatalf("main_title missing or wrong type: %#v", card["main_title"])
	}
	if got, want := mainTitle["title"], "Ops Bot"; got != want {
		t.Fatalf("main_title.title = %v, want %v", got, want)
	}
	if got, want := card["sub_title_text"], "hello from card mode"; got != want {
		t.Fatalf("sub_title_text = %v, want %v", got, want)
	}
	cardAction, ok := card["card_action"].(map[string]any)
	if !ok {
		t.Fatalf("card_action missing or wrong type: %#v", card["card_action"])
	}
	if got, want := cardAction["type"], float64(1); got != want {
		t.Fatalf("card_action.type = %v, want %v", got, want)
	}
	if got, want := cardAction["url"], "https://work.weixin.qq.com/"; got != want {
		t.Fatalf("card_action.url = %v, want %v", got, want)
	}
}
func TestWeComOfficialInboundTextPublishesToBus(t *testing.T) {
	server := newWeComOfficialTestServer(t, func(conn *websocket.Conn) {
		body, err := json.Marshal(map[string]any{
			"msgid":    "msg-1",
			"aibotid":  "bot-id",
			"chattype": "single",
			"from": map[string]any{
				"userid": "user-1",
			},
			"msgtype": "text",
			"text": map[string]any{
				"content": "hello from wecom",
			},
		})
		if err != nil {
			t.Errorf("marshal callback body: %v", err)
			return
		}
		if err := conn.WriteJSON(wecomOfficialFrame{
			Cmd:     wecomOfficialCmdMessageCallback,
			Headers: wecomOfficialHeaders{ReqID: "callback-1"},
			Body:    body,
		}); err != nil {
			t.Errorf("write callback: %v", err)
		}
	})
	defer server.close()

	msgBus := bus.NewMessageBus()
	ch, err := NewWeComOfficialChannel(config.WeComOfficialConfig{
		Enabled:             true,
		BotID:               "bot-id",
		Secret:              "bot-secret",
		WebSocketURL:        server.wsURL,
		SendThinkingMessage: boolPtr(false),
	}, msgBus)
	if err != nil {
		t.Fatalf("NewWeComOfficialChannel() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ch.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = ch.Stop(context.Background()) }()

	server.waitAuth(t)

	consumeCtx, consumeCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer consumeCancel()

	inbound, ok := msgBus.ConsumeInbound(consumeCtx)
	if !ok {
		t.Fatal("expected inbound message from WeCom official channel")
	}

	if got, want := inbound.Channel, "wecom_official"; got != want {
		t.Fatalf("Channel = %q, want %q", got, want)
	}
	if got, want := inbound.ChatID, "user-1"; got != want {
		t.Fatalf("ChatID = %q, want %q", got, want)
	}
	if got, want := inbound.Content, "hello from wecom"; got != want {
		t.Fatalf("Content = %q, want %q", got, want)
	}
	if got, want := inbound.Sender.CanonicalID, "wecom_official:user-1"; got != want {
		t.Fatalf("Sender.CanonicalID = %q, want %q", got, want)
	}
	if got, want := inbound.Metadata["reply_to"], "callback-1"; got != want {
		t.Fatalf("Metadata[reply_to] = %q, want %q", got, want)
	}
}

func TestWeComOfficialReplyUsesCallbackReqID(t *testing.T) {
	server := newWeComOfficialTestServer(t, func(conn *websocket.Conn) {
		body, err := json.Marshal(map[string]any{
			"msgid":    "msg-2",
			"aibotid":  "bot-id",
			"chattype": "single",
			"from": map[string]any{
				"userid": "user-2",
			},
			"msgtype": "text",
			"text": map[string]any{
				"content": "stream me",
			},
		})
		if err != nil {
			t.Errorf("marshal callback body: %v", err)
			return
		}
		if err := conn.WriteJSON(wecomOfficialFrame{
			Cmd:     wecomOfficialCmdMessageCallback,
			Headers: wecomOfficialHeaders{ReqID: "callback-req-1"},
			Body:    body,
		}); err != nil {
			t.Errorf("write callback: %v", err)
		}
	})
	defer server.close()

	msgBus := bus.NewMessageBus()
	ch, err := NewWeComOfficialChannel(config.WeComOfficialConfig{
		Enabled:             true,
		BotID:               "bot-id",
		Secret:              "bot-secret",
		WebSocketURL:        server.wsURL,
		SendThinkingMessage: boolPtr(false),
	}, msgBus)
	if err != nil {
		t.Fatalf("NewWeComOfficialChannel() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ch.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = ch.Stop(context.Background()) }()

	server.waitAuth(t)

	consumeCtx, consumeCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer consumeCancel()
	inbound, ok := msgBus.ConsumeInbound(consumeCtx)
	if !ok {
		t.Fatal("expected inbound message from callback")
	}

	if err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "wecom_official",
		ChatID:  "user-2",
		Content: "first streamed reply",
		ReplyTo: inbound.Metadata["reply_to"],
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	first := server.waitReply(t)
	if got, want := first.Cmd, wecomOfficialCmdRespondMessage; got != want {
		t.Fatalf("first reply cmd = %q, want %q", got, want)
	}
	if got, want := first.Headers.ReqID, "callback-req-1"; got != want {
		t.Fatalf("first reply req_id = %q, want %q", got, want)
	}

	var firstBody map[string]any
	if err := json.Unmarshal(first.Body, &firstBody); err != nil {
		t.Fatalf("unmarshal first reply body: %v", err)
	}
	stream, ok := firstBody["stream"].(map[string]any)
	if !ok {
		t.Fatalf("first reply missing stream body: %#v", firstBody)
	}
	streamID, _ := stream["id"].(string)
	if streamID == "" {
		t.Fatal("expected stream id in first reply")
	}
	if got, want := stream["finish"], false; got != want {
		t.Fatalf("first reply finish = %v, want %v", got, want)
	}
	if got, want := stream["content"], "first streamed reply"; got != want {
		t.Fatalf("first reply content = %v, want %v", got, want)
	}

	final := server.waitReply(t)
	if got, want := final.Cmd, wecomOfficialCmdRespondMessage; got != want {
		t.Fatalf("final reply cmd = %q, want %q", got, want)
	}
	if got, want := final.Headers.ReqID, "callback-req-1"; got != want {
		t.Fatalf("final reply req_id = %q, want %q", got, want)
	}

	var finalBody map[string]any
	if err := json.Unmarshal(final.Body, &finalBody); err != nil {
		t.Fatalf("unmarshal final reply body: %v", err)
	}
	finalStream, ok := finalBody["stream"].(map[string]any)
	if !ok {
		t.Fatalf("final reply missing stream body: %#v", finalBody)
	}
	if got, want := finalStream["id"], streamID; got != want {
		t.Fatalf("final reply stream id = %v, want %v", got, want)
	}
	if got, want := finalStream["finish"], true; got != want {
		t.Fatalf("final reply finish = %v, want %v", got, want)
	}
	if got, want := finalStream["content"], "first streamed reply"; got != want {
		t.Fatalf("final reply content = %v, want %v", got, want)
	}
}

func TestWeComOfficialActiveReplyTaskRequiresReplyTo(t *testing.T) {
	server := newWeComOfficialTestServer(t, func(conn *websocket.Conn) {
		body, err := json.Marshal(map[string]any{
			"msgid":    "msg-4",
			"aibotid":  "bot-id",
			"chattype": "single",
			"from": map[string]any{
				"userid": "user-4",
			},
			"msgtype": "text",
			"text": map[string]any{
				"content": "hello",
			},
		})
		if err != nil {
			t.Errorf("marshal callback body: %v", err)
			return
		}
		if err := conn.WriteJSON(wecomOfficialFrame{
			Cmd:     wecomOfficialCmdMessageCallback,
			Headers: wecomOfficialHeaders{ReqID: "callback-req-4"},
			Body:    body,
		}); err != nil {
			t.Errorf("write callback: %v", err)
		}
	})
	defer server.close()

	msgBus := bus.NewMessageBus()
	ch, err := NewWeComOfficialChannel(config.WeComOfficialConfig{
		Enabled:             true,
		BotID:               "bot-id",
		Secret:              "bot-secret",
		WebSocketURL:        server.wsURL,
		SendThinkingMessage: boolPtr(false),
	}, msgBus)
	if err != nil {
		t.Fatalf("NewWeComOfficialChannel() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ch.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = ch.Stop(context.Background()) }()

	server.waitAuth(t)

	consumeCtx, consumeCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer consumeCancel()
	if _, ok := msgBus.ConsumeInbound(consumeCtx); !ok {
		t.Fatal("expected inbound message from callback")
	}

	if err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "wecom_official",
		ChatID:  "user-4",
		Content: "proactive fallback",
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	payload := server.waitSend(t)
	if got, want := payload["chatid"], "user-4"; got != want {
		t.Fatalf("chatid = %v, want %v", got, want)
	}

	select {
	case frame := <-server.replyCh:
		t.Fatalf("unexpected reply stream frame: %+v", frame)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestWeComOfficialReplyAccumulatesStreamContent(t *testing.T) {
	server := newWeComOfficialTestServer(t, func(conn *websocket.Conn) {
		body, err := json.Marshal(map[string]any{
			"msgid":    "msg-5",
			"aibotid":  "bot-id",
			"chattype": "single",
			"from": map[string]any{
				"userid": "user-5",
			},
			"msgtype": "text",
			"text": map[string]any{
				"content": "stream me twice",
			},
		})
		if err != nil {
			t.Errorf("marshal callback body: %v", err)
			return
		}
		if err := conn.WriteJSON(wecomOfficialFrame{
			Cmd:     wecomOfficialCmdMessageCallback,
			Headers: wecomOfficialHeaders{ReqID: "callback-req-5"},
			Body:    body,
		}); err != nil {
			t.Errorf("write callback: %v", err)
		}
	})
	defer server.close()

	msgBus := bus.NewMessageBus()
	ch, err := NewWeComOfficialChannel(config.WeComOfficialConfig{
		Enabled:             true,
		BotID:               "bot-id",
		Secret:              "bot-secret",
		WebSocketURL:        server.wsURL,
		SendThinkingMessage: boolPtr(false),
	}, msgBus)
	if err != nil {
		t.Fatalf("NewWeComOfficialChannel() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ch.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = ch.Stop(context.Background()) }()

	server.waitAuth(t)

	consumeCtx, consumeCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer consumeCancel()
	inbound, ok := msgBus.ConsumeInbound(consumeCtx)
	if !ok {
		t.Fatal("expected inbound message from callback")
	}

	replyTo := inbound.Metadata["reply_to"]
	if err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "wecom_official",
		ChatID:  "user-5",
		Content: "part 1",
		ReplyTo: replyTo,
	}); err != nil {
		t.Fatalf("first Send() error = %v", err)
	}

	first := server.waitReply(t)
	var firstBody map[string]any
	if err := json.Unmarshal(first.Body, &firstBody); err != nil {
		t.Fatalf("unmarshal first reply body: %v", err)
	}
	firstStream := firstBody["stream"].(map[string]any)
	if got, want := firstStream["content"], "part 1"; got != want {
		t.Fatalf("first reply content = %v, want %v", got, want)
	}

	if err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "wecom_official",
		ChatID:  "user-5",
		Content: "\npart 2",
		ReplyTo: replyTo,
	}); err != nil {
		t.Fatalf("second Send() error = %v", err)
	}

	second := server.waitReply(t)
	var secondBody map[string]any
	if err := json.Unmarshal(second.Body, &secondBody); err != nil {
		t.Fatalf("unmarshal second reply body: %v", err)
	}
	secondStream := secondBody["stream"].(map[string]any)
	if got, want := secondStream["content"], "part 1\npart 2"; got != want {
		t.Fatalf("second reply content = %v, want %v", got, want)
	}
	if got, want := secondStream["finish"], false; got != want {
		t.Fatalf("second reply finish = %v, want %v", got, want)
	}

	final := server.waitReply(t)
	var finalBody map[string]any
	if err := json.Unmarshal(final.Body, &finalBody); err != nil {
		t.Fatalf("unmarshal final reply body: %v", err)
	}
	finalStream := finalBody["stream"].(map[string]any)
	if got, want := finalStream["content"], "part 1\npart 2"; got != want {
		t.Fatalf("final reply content = %v, want %v", got, want)
	}
	if got, want := finalStream["finish"], true; got != want {
		t.Fatalf("final reply finish = %v, want %v", got, want)
	}
}

func TestWeComOfficialReplyFinishUsesStreamWithTemplateCardWhenEnabled(t *testing.T) {
	server := newWeComOfficialTestServer(t, func(conn *websocket.Conn) {
		body, err := json.Marshal(map[string]any{
			"msgid":    "msg-card-1",
			"aibotid":  "bot-id",
			"chattype": "single",
			"from": map[string]any{
				"userid": "user-card-1",
			},
			"msgtype": "text",
			"text": map[string]any{
				"content": "render a card",
			},
		})
		if err != nil {
			t.Errorf("marshal callback body: %v", err)
			return
		}
		if err := conn.WriteJSON(wecomOfficialFrame{
			Cmd:     wecomOfficialCmdMessageCallback,
			Headers: wecomOfficialHeaders{ReqID: "callback-card-1"},
			Body:    body,
		}); err != nil {
			t.Errorf("write callback: %v", err)
		}
	})
	defer server.close()

	msgBus := bus.NewMessageBus()
	ch, err := NewWeComOfficialChannel(config.WeComOfficialConfig{
		Enabled:             true,
		BotID:               "bot-id",
		Secret:              "bot-secret",
		WebSocketURL:        server.wsURL,
		SendThinkingMessage: boolPtr(false),
		Card: config.CardConfig{
			Enabled: true,
			Title:   "Ops Bot",
		},
	}, msgBus)
	if err != nil {
		t.Fatalf("NewWeComOfficialChannel() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ch.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = ch.Stop(context.Background()) }()

	server.waitAuth(t)

	consumeCtx, consumeCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer consumeCancel()
	inbound, ok := msgBus.ConsumeInbound(consumeCtx)
	if !ok {
		t.Fatal("expected inbound message from callback")
	}

	if err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "wecom_official",
		ChatID:  "user-card-1",
		Content: "final card reply",
		ReplyTo: inbound.Metadata["reply_to"],
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	first := server.waitReply(t)
	var firstBody map[string]any
	if err := json.Unmarshal(first.Body, &firstBody); err != nil {
		t.Fatalf("unmarshal first reply body: %v", err)
	}
	if got, want := firstBody["msgtype"], "stream_with_template_card"; got != want {
		t.Fatalf("first msgtype = %v, want %v", got, want)
	}
	firstStream, ok := firstBody["stream"].(map[string]any)
	if !ok {
		t.Fatalf("first stream missing or wrong type: %#v", firstBody["stream"])
	}
	if got, want := firstStream["content"], "final card reply"; got != want {
		t.Fatalf("first stream.content = %v, want %v", got, want)
	}
	if got, want := firstStream["finish"], false; got != want {
		t.Fatalf("first stream.finish = %v, want %v", got, want)
	}
	firstCard, ok := firstBody["template_card"].(map[string]any)
	if !ok {
		t.Fatalf("first template_card missing or wrong type: %#v", firstBody["template_card"])
	}
	if got, want := firstCard["sub_title_text"], "final card reply"; got != want {
		t.Fatalf("first sub_title_text = %v, want %v", got, want)
	}

	final := server.waitReply(t)
	var finalBody map[string]any
	if err := json.Unmarshal(final.Body, &finalBody); err != nil {
		t.Fatalf("unmarshal final reply body: %v", err)
	}
	if got, want := finalBody["msgtype"], "stream"; got != want {
		t.Fatalf("final msgtype = %v, want %v", got, want)
	}
	stream, ok := finalBody["stream"].(map[string]any)
	if !ok {
		t.Fatalf("stream missing or wrong type: %#v", finalBody["stream"])
	}
	if got, want := stream["content"], "final card reply"; got != want {
		t.Fatalf("stream.content = %v, want %v", got, want)
	}
	if got, want := stream["finish"], true; got != want {
		t.Fatalf("stream.finish = %v, want %v", got, want)
	}
	if _, ok := finalBody["template_card"]; ok {
		t.Fatalf("final frame should not include template_card: %#v", finalBody["template_card"])
	}
}

func TestWeComOfficialThinkingPlaceholderUsesConfiguredText(t *testing.T) {
	server := newWeComOfficialTestServer(t, func(conn *websocket.Conn) {
		body, err := json.Marshal(map[string]any{
			"msgid":    "msg-6",
			"aibotid":  "bot-id",
			"chattype": "single",
			"from": map[string]any{
				"userid": "user-6",
			},
			"msgtype": "text",
			"text": map[string]any{
				"content": "hello",
			},
		})
		if err != nil {
			t.Errorf("marshal callback body: %v", err)
			return
		}
		if err := conn.WriteJSON(wecomOfficialFrame{
			Cmd:     wecomOfficialCmdMessageCallback,
			Headers: wecomOfficialHeaders{ReqID: "callback-req-6"},
			Body:    body,
		}); err != nil {
			t.Errorf("write callback: %v", err)
		}
	})
	defer server.close()

	msgBus := bus.NewMessageBus()
	ch, err := NewWeComOfficialChannel(config.WeComOfficialConfig{
		Enabled:      true,
		BotID:        "bot-id",
		Secret:       "bot-secret",
		WebSocketURL: server.wsURL,
		Placeholder: config.PlaceholderConfig{
			Enabled: true,
			Text:    "婵繐绲藉﹢顏堝箑濠靛ň鍋撻崘褑鍘?..",
		},
	}, msgBus)
	if err != nil {
		t.Fatalf("NewWeComOfficialChannel() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ch.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = ch.Stop(context.Background()) }()

	server.waitAuth(t)

	reply := server.waitReply(t)
	if got, want := reply.Cmd, wecomOfficialCmdRespondMessage; got != want {
		t.Fatalf("placeholder cmd = %q, want %q", got, want)
	}
	if got, want := reply.Headers.ReqID, "callback-req-6"; got != want {
		t.Fatalf("placeholder req_id = %q, want %q", got, want)
	}

	var body map[string]any
	if err := json.Unmarshal(reply.Body, &body); err != nil {
		t.Fatalf("unmarshal placeholder body: %v", err)
	}
	stream, ok := body["stream"].(map[string]any)
	if !ok {
		t.Fatalf("placeholder missing stream body: %#v", body)
	}
	if got, want := stream["content"], "婵繐绲藉﹢顏堝箑濠靛ň鍋撻崘褑鍘?.."; got != want {
		t.Fatalf("placeholder content = %v, want %v", got, want)
	}
	if got, want := stream["finish"], false; got != want {
		t.Fatalf("placeholder finish = %v, want %v", got, want)
	}
}

func TestWeComOfficialWelcomeUsesWelcomeReplyCommand(t *testing.T) {
	server := newWeComOfficialTestServer(t, func(conn *websocket.Conn) {
		body, err := json.Marshal(map[string]any{
			"msgid":   "event-1",
			"aibotid": "bot-id",
			"from": map[string]any{
				"userid": "user-3",
			},
			"msgtype": "event",
			"event": map[string]any{
				"eventtype": "enter_chat",
			},
		})
		if err != nil {
			t.Errorf("marshal event body: %v", err)
			return
		}
		if err := conn.WriteJSON(wecomOfficialFrame{
			Cmd:     wecomOfficialCmdEventCallback,
			Headers: wecomOfficialHeaders{ReqID: "event-req-1"},
			Body:    body,
		}); err != nil {
			t.Errorf("write event callback: %v", err)
		}
	})
	defer server.close()

	msgBus := bus.NewMessageBus()
	ch, err := NewWeComOfficialChannel(config.WeComOfficialConfig{
		Enabled:             true,
		BotID:               "bot-id",
		Secret:              "bot-secret",
		WebSocketURL:        server.wsURL,
		SendThinkingMessage: boolPtr(false),
		WelcomeMessage:      "welcome aboard",
	}, msgBus)
	if err != nil {
		t.Fatalf("NewWeComOfficialChannel() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ch.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = ch.Stop(context.Background()) }()

	server.waitAuth(t)

	reply := server.waitReply(t)
	if got, want := reply.Cmd, wecomOfficialCmdRespondWelcome; got != want {
		t.Fatalf("welcome reply cmd = %q, want %q", got, want)
	}
	if got, want := reply.Headers.ReqID, "event-req-1"; got != want {
		t.Fatalf("welcome reply req_id = %q, want %q", got, want)
	}

	var body map[string]any
	if err := json.Unmarshal(reply.Body, &body); err != nil {
		t.Fatalf("unmarshal welcome body: %v", err)
	}
	text, ok := body["text"].(map[string]any)
	if !ok {
		t.Fatalf("welcome reply missing text body: %#v", body)
	}
	if got, want := text["content"], "welcome aboard"; got != want {
		t.Fatalf("welcome text = %v, want %v", got, want)
	}
}

func TestWeComOfficialWelcomeUsesTemplateCardWhenCardEnabled(t *testing.T) {
	server := newWeComOfficialTestServer(t, func(conn *websocket.Conn) {
		body, err := json.Marshal(map[string]any{
			"msgid":   "event-card-1",
			"aibotid": "bot-id",
			"from": map[string]any{
				"userid": "user-welcome-card",
			},
			"msgtype": "event",
			"event": map[string]any{
				"eventtype": "enter_chat",
			},
		})
		if err != nil {
			t.Errorf("marshal event body: %v", err)
			return
		}
		if err := conn.WriteJSON(wecomOfficialFrame{
			Cmd:     wecomOfficialCmdEventCallback,
			Headers: wecomOfficialHeaders{ReqID: "event-card-req-1"},
			Body:    body,
		}); err != nil {
			t.Errorf("write event callback: %v", err)
		}
	})
	defer server.close()

	msgBus := bus.NewMessageBus()
	ch, err := NewWeComOfficialChannel(config.WeComOfficialConfig{
		Enabled:             true,
		BotID:               "bot-id",
		Secret:              "bot-secret",
		WebSocketURL:        server.wsURL,
		SendThinkingMessage: boolPtr(false),
		WelcomeMessage:      "welcome aboard",
		Card: config.CardConfig{
			Enabled: true,
			Title:   "Ops Bot",
		},
	}, msgBus)
	if err != nil {
		t.Fatalf("NewWeComOfficialChannel() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := ch.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = ch.Stop(context.Background()) }()

	server.waitAuth(t)

	reply := server.waitReply(t)
	if got, want := reply.Cmd, wecomOfficialCmdRespondWelcome; got != want {
		t.Fatalf("welcome reply cmd = %q, want %q", got, want)
	}

	var body map[string]any
	if err := json.Unmarshal(reply.Body, &body); err != nil {
		t.Fatalf("unmarshal welcome body: %v", err)
	}
	if got, want := body["msgtype"], "template_card"; got != want {
		t.Fatalf("welcome msgtype = %v, want %v", got, want)
	}
	card, ok := body["template_card"].(map[string]any)
	if !ok {
		t.Fatalf("template_card missing or wrong type: %#v", body["template_card"])
	}
	mainTitle, ok := card["main_title"].(map[string]any)
	if !ok {
		t.Fatalf("main_title missing or wrong type: %#v", card["main_title"])
	}
	if got, want := mainTitle["title"], "Ops Bot"; got != want {
		t.Fatalf("main_title.title = %v, want %v", got, want)
	}
	cardAction, ok := card["card_action"].(map[string]any)
	if !ok {
		t.Fatalf("card_action missing or wrong type: %#v", card["card_action"])
	}
	if got, want := cardAction["type"], float64(1); got != want {
		t.Fatalf("card_action.type = %v, want %v", got, want)
	}
	if got, want := cardAction["url"], "https://work.weixin.qq.com/"; got != want {
		t.Fatalf("card_action.url = %v, want %v", got, want)
	}
}

func TestWeComOfficialReplyClosesBeforeExpiryAndDefersFinalAsProactiveSend(t *testing.T) {
	server := newWeComOfficialTestServer(t, nil)
	defer server.close()

	msgBus := bus.NewMessageBus()
	ch, err := NewWeComOfficialChannel(config.WeComOfficialConfig{
		Enabled:      true,
		BotID:        "bot-id",
		Secret:       "bot-secret",
		WebSocketURL: server.wsURL,
	}, msgBus)
	if err != nil {
		t.Fatalf("NewWeComOfficialChannel() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer ch.Stop(context.Background())
	server.waitAuth(t)

	task := ch.enqueueReplyTask("user-expiry", "callback-expiry")
	if task == nil {
		t.Fatal("expected reply task")
	}
	task.EditDeadline = time.Now().Add(10 * time.Second)

	if err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "wecom_official",
		ChatID:  "user-expiry",
		ReplyTo: "callback-expiry",
		Content: "final answer after long work",
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	reply := server.waitReply(t)
	var replyBody map[string]any
	if err := json.Unmarshal(reply.Body, &replyBody); err != nil {
		t.Fatalf("unmarshal reply body: %v", err)
	}
	stream := replyBody["stream"].(map[string]any)
	if got, want := stream["finish"], true; got != want {
		t.Fatalf("finish = %v, want %v", got, want)
	}
	if content := stream["content"].(string); !strings.Contains(content, "I am still working on this. I will send the full result in a new message shortly.") {
		t.Fatalf("closing content = %q, want natural closing text", content)
	}

	payload := server.waitSend(t)
	if got, want := payload["chatid"], "user-expiry"; got != want {
		t.Fatalf("chatid = %v, want %v", got, want)
	}
	markdown := payload["markdown"].(map[string]any)
	if got, want := markdown["content"], "final answer after long work"; got != want {
		t.Fatalf("markdown.content = %v, want %v", got, want)
	}
	if active := ch.activeReplyTask("user-expiry", "callback-expiry"); active != nil {
		t.Fatal("expected reply task to be removed after natural close")
	}
}

func TestWeComOfficialSendTemplateCardPayloadPassthrough(t *testing.T) {
	server := newWeComOfficialTestServer(t, nil)
	defer server.close()

	msgBus := bus.NewMessageBus()
	ch, err := NewWeComOfficialChannel(config.WeComOfficialConfig{
		Enabled:      true,
		BotID:        "bot-id",
		Secret:       "bot-secret",
		WebSocketURL: server.wsURL,
	}, msgBus)
	if err != nil {
		t.Fatalf("NewWeComOfficialChannel() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer ch.Stop(context.Background())
	server.waitAuth(t)

	content := `{"msgtype":"template_card","template_card":{"card_type":"text_notice","main_title":{"title":"藤蔓电气"},"sub_title_text":"国家高新技术企业","card_action":{"type":1,"url":"https://www.tengwan.com"}}}`
	if err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "wecom_official",
		ChatID:  "YangXu",
		Content: content,
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	payload := server.waitSend(t)
	if got, want := payload["msgtype"], "template_card"; got != want {
		t.Fatalf("msgtype = %v, want %v", got, want)
	}
	card, ok := payload["template_card"].(map[string]any)
	if !ok {
		t.Fatalf("template_card missing or wrong type: %#v", payload["template_card"])
	}
	mainTitle := card["main_title"].(map[string]any)
	if got, want := mainTitle["title"], "藤蔓电气"; got != want {
		t.Fatalf("main_title.title = %v, want %v", got, want)
	}
	cardAction := card["card_action"].(map[string]any)
	if got, want := cardAction["url"], "https://www.tengwan.com"; got != want {
		t.Fatalf("card_action.url = %v, want %v", got, want)
	}
}

func TestWeComOfficialReplyTemplateCardPayloadUsesReplyStreamContext(t *testing.T) {
	server := newWeComOfficialTestServer(t, func(conn *websocket.Conn) {
		body, err := json.Marshal(map[string]any{
			"msgid":    "msg-card-payload-1",
			"aibotid":  "bot-id",
			"chattype": "single",
			"from": map[string]any{
				"userid": "user-card-payload-1",
			},
			"msgtype": "text",
			"text": map[string]any{
				"content": "send me a card",
			},
		})
		if err != nil {
			t.Errorf("marshal callback body: %v", err)
			return
		}
		if err := conn.WriteJSON(wecomOfficialFrame{
			Cmd:     wecomOfficialCmdMessageCallback,
			Headers: wecomOfficialHeaders{ReqID: "callback-card-payload-1"},
			Body:    body,
		}); err != nil {
			t.Errorf("write callback: %v", err)
		}
	})
	defer server.close()

	msgBus := bus.NewMessageBus()
	ch, err := NewWeComOfficialChannel(config.WeComOfficialConfig{
		Enabled:      true,
		BotID:        "bot-id",
		Secret:       "bot-secret",
		WebSocketURL: server.wsURL,
		Placeholder: config.PlaceholderConfig{
			Enabled: true,
			Text:    "Thinking...",
		},
	}, msgBus)
	if err != nil {
		t.Fatalf("NewWeComOfficialChannel() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer ch.Stop(context.Background())
	server.waitAuth(t)

	consumeCtx, consumeCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer consumeCancel()
	inbound, ok := msgBus.ConsumeInbound(consumeCtx)
	if !ok {
		t.Fatal("expected inbound message from callback")
	}

	placeholder := server.waitReply(t)
	var placeholderBody map[string]any
	if err := json.Unmarshal(placeholder.Body, &placeholderBody); err != nil {
		t.Fatalf("unmarshal placeholder body: %v", err)
	}
	placeholderStream, ok := placeholderBody["stream"].(map[string]any)
	if !ok {
		t.Fatalf("placeholder missing stream body: %#v", placeholderBody)
	}
	if placeholderStreamID, _ := placeholderStream["id"].(string); placeholderStreamID == "" {
		t.Fatal("expected placeholder stream id")
	}

	content := `{"msgtype":"template_card","template_card":{"card_type":"button_interaction","main_title":{"title":"按钮卡片","desc":"无整体跳转"},"button_list":[{"text":"确认","key":"confirm"}],"task_id":"task-card-1","card_action":{"type":0}}}`
	if err := ch.Send(context.Background(), bus.OutboundMessage{
		Channel: "wecom_official",
		ChatID:  "user-card-payload-1",
		ReplyTo: inbound.Metadata["reply_to"],
		Content: content,
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	reply := server.waitReply(t)
	if got, want := reply.Cmd, wecomOfficialCmdRespondMessage; got != want {
		t.Fatalf("reply cmd = %q, want %q", got, want)
	}
	if got, want := reply.Headers.ReqID, inbound.Metadata["reply_to"]; got != want {
		t.Fatalf("reply req_id = %q, want %q", got, want)
	}

	var replyBody map[string]any
	if err := json.Unmarshal(reply.Body, &replyBody); err != nil {
		t.Fatalf("unmarshal reply body: %v", err)
	}
	if got, want := replyBody["msgtype"], "template_card"; got != want {
		t.Fatalf("reply msgtype = %v, want %v", got, want)
	}
	card, ok := replyBody["template_card"].(map[string]any)
	if !ok {
		t.Fatalf("reply template_card missing or wrong type: %#v", replyBody["template_card"])
	}
	cardAction := card["card_action"].(map[string]any)
	if got, want := cardAction["type"], float64(0); got != want {
		t.Fatalf("card_action.type = %v, want %v", got, want)
	}
	if _, exists := replyBody["stream"]; exists {
		t.Fatalf("template_card reply should not include stream body: %#v", replyBody["stream"])
	}

	select {
	case payload := <-server.sendCh:
		t.Fatalf("unexpected proactive send payload: %#v", payload)
	case <-time.After(300 * time.Millisecond):
	}
	if active := ch.activeReplyTask("user-card-payload-1", inbound.Metadata["reply_to"]); active != nil {
		t.Fatal("expected reply task to be removed after template card reply")
	}
}
