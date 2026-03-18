package wecom

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/h2non/filetype"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/identity"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/media"
	"github.com/sipeed/picoclaw/pkg/utils"
)

const (
	wecomOfficialDefaultWSURL       = "wss://openws.work.weixin.qq.com"
	wecomOfficialCmdSubscribe       = "aibot_subscribe"
	wecomOfficialCmdHeartbeat       = "ping"
	wecomOfficialCmdRespondMessage  = "aibot_respond_msg"
	wecomOfficialCmdRespondUpdate   = "aibot_respond_update_msg"
	wecomOfficialCmdRespondWelcome  = "aibot_respond_welcome_msg"
	wecomOfficialCmdSendMessage     = "aibot_send_msg"
	wecomOfficialCmdMessageCallback = "aibot_msg_callback"
	wecomOfficialCmdEventCallback   = "aibot_event_callback"

	wecomOfficialMaxMessageLength    = 4000
	wecomOfficialMaxReconnects       = 100
	wecomOfficialMaxMissedHeartbeats = 2
	wecomOfficialDefaultCardTitle    = "PicoClaw"
	wecomOfficialCardSummaryLimit    = 112
	wecomOfficialCardQuoteLimit      = 1024

	wecomOfficialDialTimeout          = 15 * time.Second
	wecomOfficialWriteTimeout         = 10 * time.Second
	wecomOfficialAuthTimeout          = 10 * time.Second
	wecomOfficialSendTimeout          = 15 * time.Second
	wecomOfficialHeartbeatInterval    = 30 * time.Second
	wecomOfficialHeartbeatAckTimeout  = 10 * time.Second
	wecomOfficialMediaDownloadTimeout = 30 * time.Second
	wecomOfficialReplyIdleClose       = 250 * time.Millisecond
	wecomOfficialReplyTaskMaxAge      = 10 * time.Minute
	wecomOfficialStreamUpdateExpiry   = 6 * time.Minute
	wecomOfficialUpdateTaskMaxAge     = 5 * time.Second
	wecomOfficialResponseURLExpiry    = 1 * time.Hour
)

const (
	wecomOfficialReplyModeStream             = "stream"
	wecomOfficialReplyModeUpdateTemplateCard = "update_template_card"
)

var (
	errWeComOfficialAuthFailed = errors.New("wecom official authentication failed")
	wecomOfficialAtPattern     = regexp.MustCompile(`@\S+`)
	wecomOfficialCardHeadingRE = regexp.MustCompile(`(?m)^\s{0,3}#{1,6}\s*`)
	wecomOfficialCardQuoteRE   = regexp.MustCompile(`(?m)^\s*>\s?`)
	wecomOfficialCardBlankRE   = regexp.MustCompile(`\n{3,}`)
	wecomOfficialCardReplacer  = strings.NewReplacer(
		"\r\n", "\n",
		"```", "",
		"**", "",
		"__", "",
		"~~", "",
		"`", "",
	)
)

type wecomOfficialHeaders struct {
	ReqID string `json:"req_id"`
}

type wecomOfficialFrame struct {
	Cmd     string               `json:"cmd,omitempty"`
	Headers wecomOfficialHeaders `json:"headers"`
	Body    json.RawMessage      `json:"body,omitempty"`
	ErrCode int                  `json:"errcode,omitempty"`
	ErrMsg  string               `json:"errmsg,omitempty"`
}

type wecomOfficialCommandFrame struct {
	Cmd     string               `json:"cmd,omitempty"`
	Headers wecomOfficialHeaders `json:"headers"`
	Body    any                  `json:"body,omitempty"`
}

type wecomOfficialWaitResult struct {
	frame wecomOfficialFrame
	err   error
}

type wecomOfficialMessage struct {
	MsgID       string `json:"msgid"`
	AIBotID     string `json:"aibotid"`
	ChatID      string `json:"chatid"`
	ChatType    string `json:"chattype"`
	ResponseURL string `json:"response_url"`
	MsgType     string `json:"msgtype"`
	From        struct {
		UserID string `json:"userid"`
		CorpID string `json:"corpid,omitempty"`
	} `json:"from"`
	Text *struct {
		Content string `json:"content"`
	} `json:"text,omitempty"`
	Voice *struct {
		Content string `json:"content"`
	} `json:"voice,omitempty"`
	Image *struct {
		URL    string `json:"url"`
		AESKey string `json:"aeskey,omitempty"`
	} `json:"image,omitempty"`
	File *struct {
		URL    string `json:"url"`
		AESKey string `json:"aeskey,omitempty"`
	} `json:"file,omitempty"`
	Mixed *struct {
		MsgItem []struct {
			MsgType string `json:"msgtype"`
			Text    *struct {
				Content string `json:"content"`
			} `json:"text,omitempty"`
			Image *struct {
				URL    string `json:"url"`
				AESKey string `json:"aeskey,omitempty"`
			} `json:"image,omitempty"`
		} `json:"msg_item"`
	} `json:"mixed,omitempty"`
	Quote *struct {
		MsgType string `json:"msgtype"`
		Text    *struct {
			Content string `json:"content"`
		} `json:"text,omitempty"`
		Voice *struct {
			Content string `json:"content"`
		} `json:"voice,omitempty"`
		Image *struct {
			URL    string `json:"url"`
			AESKey string `json:"aeskey,omitempty"`
		} `json:"image,omitempty"`
		File *struct {
			URL    string `json:"url"`
			AESKey string `json:"aeskey,omitempty"`
		} `json:"file,omitempty"`
	} `json:"quote,omitempty"`
	CreateTime int64 `json:"create_time,omitempty"`
	Event      *struct {
		EventType         string                          `json:"eventtype"`
		EventKey          string                          `json:"event_key,omitempty"` // legacy flat fallback
		TaskID            string                          `json:"task_id,omitempty"`   // legacy flat fallback
		CardType          string                          `json:"card_type,omitempty"` // legacy flat fallback
		TemplateCardEvent *wecomOfficialTemplateCardEvent `json:"template_card_event,omitempty"`
	} `json:"event,omitempty"`
}

type wecomOfficialTemplateCardEvent struct {
	CardType      string                                  `json:"card_type,omitempty"`
	EventKey      string                                  `json:"event_key,omitempty"`
	TaskID        string                                  `json:"task_id,omitempty"`
	SelectedItems *wecomOfficialTemplateCardSelectedItems `json:"selected_items,omitempty"`
}

type wecomOfficialTemplateCardSelectedItems struct {
	SelectedItem []wecomOfficialTemplateCardSelectedItem `json:"selected_item,omitempty"`
}

type wecomOfficialTemplateCardSelectedItem struct {
	QuestionKey string `json:"question_key,omitempty"`
	OptionIDs   *struct {
		OptionID []string `json:"option_id,omitempty"`
	} `json:"option_ids,omitempty"`
}

func (m wecomOfficialMessage) templateCardEvent() *wecomOfficialTemplateCardEvent {
	if m.Event == nil || strings.TrimSpace(m.Event.EventType) != "template_card_event" {
		return nil
	}
	if m.Event.TemplateCardEvent != nil {
		return m.Event.TemplateCardEvent
	}
	return &wecomOfficialTemplateCardEvent{
		CardType: m.Event.CardType,
		EventKey: m.Event.EventKey,
		TaskID:   m.Event.TaskID,
	}
}

type wecomOfficialMediaSource struct {
	Kind   string
	URL    string
	AESKey string
}

type wecomOfficialReplyTask struct {
	ReqID        string
	ChatID       string
	ChatType     string
	StreamID     string
	CreatedAt    time.Time
	EditDeadline time.Time
	ResponseMode string
	ResponseURL  string
	TaskID       string

	accumulated          string
	cardSent             bool
	closedNaturally      bool
	finalDeliveryPending bool
	pendingFinal         string
	mu                   sync.Mutex
	timer                *time.Timer
	sequence             uint64
	finalized            bool
}

func (c *WeComOfficialChannel) placeholderEnabled() bool {
	if c.config.SendThinkingMessage != nil {
		return *c.config.SendThinkingMessage
	}
	return c.config.Placeholder.Enabled
}

func (c *WeComOfficialChannel) placeholderText() string {
	text := strings.TrimSpace(c.config.Placeholder.Text)
	if text == "" {
		return "Thinking... 💭"
	}
	return text
}

func (c *WeComOfficialChannel) cardEnabled() bool {
	return c.config.Card.Enabled
}

func (c *WeComOfficialChannel) cardTitle() string {
	title := strings.TrimSpace(c.config.Card.Title)
	if title == "" {
		return wecomOfficialDefaultCardTitle
	}
	return title
}

func (c *WeComOfficialChannel) streamCloseBeforeExpiry() time.Duration {
	return 30 * time.Second
}

func (c *WeComOfficialChannel) streamClosingText() string {
	return "I am still working on this. I will send the full result in a new message shortly."
}

// CanReuseReply reports whether a callback-scoped reply token is still safe to
// reuse for callback-scoped delivery. The channel may still fall back from
// stream/card editing to response_url delivery when the original edit window has
// expired, so callers should preserve reply_to while that fallback remains
// available.
func (c *WeComOfficialChannel) CanReuseReply(chatID, replyTo string) bool {
	task := c.activeReplyTask(chatID, replyTo)
	if task == nil {
		return false
	}

	task.mu.Lock()
	defer task.mu.Unlock()

	if task.finalized || task.closedNaturally {
		return c.canUseResponseURLLocked(task)
	}
	if task.ResponseMode == wecomOfficialReplyModeUpdateTemplateCard {
		return time.Until(task.EditDeadline) > 0 || c.canUseResponseURLLocked(task)
	}
	return time.Until(task.EditDeadline) > c.streamCloseBeforeExpiry() || c.canUseResponseURLLocked(task)
}

// WeComOfficialChannel implements the official WeCom Smart Bot websocket channel.
// It can receive inbound messages over the official websocket callback stream and
// send proactive markdown notifications via aibot_send_msg.
type WeComOfficialChannel struct {
	*channels.BaseChannel
	config        config.WeComOfficialConfig
	httpClient    *http.Client
	processedMsgs *MessageDeduplicator
	ctx           context.Context
	cancel        context.CancelFunc
	connMu        sync.RWMutex
	conn          *websocket.Conn
	writeMu       sync.Mutex
	pendingMu     sync.Mutex
	pendingAcks   map[string]chan wecomOfficialWaitResult
	taskMu        sync.Mutex
	replyTasks    map[string][]*wecomOfficialReplyTask
	cardMu        sync.RWMutex
	cardStates    map[string]map[string]any
}

// NewWeComOfficialChannel creates a new official WeCom websocket channel instance.
func NewWeComOfficialChannel(
	cfg config.WeComOfficialConfig,
	messageBus *bus.MessageBus,
) (*WeComOfficialChannel, error) {
	if strings.TrimSpace(cfg.BotID) == "" || strings.TrimSpace(cfg.Secret) == "" {
		return nil, fmt.Errorf("wecom_official bot_id and secret are required")
	}
	if strings.TrimSpace(cfg.WebSocketURL) == "" {
		cfg.WebSocketURL = wecomOfficialDefaultWSURL
	}

	base := channels.NewBaseChannel(
		"wecom_official",
		cfg,
		messageBus,
		cfg.AllowFrom,
		channels.WithMaxMessageLength(wecomOfficialMaxMessageLength),
		channels.WithGroupTrigger(cfg.GroupTrigger),
		channels.WithReasoningChannelID(cfg.ReasoningChannelID),
	)

	return &WeComOfficialChannel{
		BaseChannel:   base,
		config:        cfg,
		httpClient:    &http.Client{Timeout: wecomOfficialMediaDownloadTimeout},
		processedMsgs: NewMessageDeduplicator(wecomMaxProcessedMessages),
		pendingAcks:   make(map[string]chan wecomOfficialWaitResult),
		replyTasks:    make(map[string][]*wecomOfficialReplyTask),
		cardStates:    make(map[string]map[string]any),
	}, nil
}

// Start launches the background websocket connection loop.
func (c *WeComOfficialChannel) Start(ctx context.Context) error {
	if c.IsRunning() {
		return nil
	}

	logger.InfoC("wecom_official", "Starting WeCom Official channel...")
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.SetRunning(true)
	go c.connectionLoop()
	logger.InfoC("wecom_official", "WeCom Official channel started")
	return nil
}

// Stop shuts down the websocket channel and all pending waiters.
func (c *WeComOfficialChannel) Stop(ctx context.Context) error {
	if !c.IsRunning() {
		return nil
	}

	logger.InfoC("wecom_official", "Stopping WeCom Official channel...")
	c.SetRunning(false)
	if c.cancel != nil {
		c.cancel()
	}
	c.closeConn(nil, fmt.Errorf("wecom_official stopped"))
	logger.InfoC("wecom_official", "WeCom Official channel stopped")
	return nil
}

// Send uses the official callback stream only when the outbound message carries
// a matching reply correlation token. Otherwise it falls back to proactive push.
func (c *WeComOfficialChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return channels.ErrNotRunning
	}
	if strings.TrimSpace(msg.ChatID) == "" {
		return fmt.Errorf("wecom_official send: empty chat id: %w", channels.ErrSendFailed)
	}
	task := c.activeReplyTask(msg.ChatID, msg.ReplyTo)
	if payload, ok := parseWeComOfficialTemplateCardPayload(msg.Content); ok {
		if task != nil {
			return c.sendReplyTemplateCard(ctx, task, payload)
		}
		if strings.TrimSpace(msg.ReplyTo) != "" {
			return fmt.Errorf("wecom_official template card callback window expired; `template_card_event` updates must be sent within 5 seconds and do not use the 6-minute stream window; cannot fall back to proactive send: %w", channels.ErrSendFailed)
		}
		_, err := c.sendCommandAndWait(ctx, wecomOfficialCmdSendMessage, map[string]any{
			"chatid":        msg.ChatID,
			"msgtype":       "template_card",
			"template_card": payload,
		}, wecomOfficialSendTimeout)
		if err == nil {
			c.rememberTemplateCard(payload)
		}
		return err
	}
	if task != nil {
		return c.sendReplyMarkdown(ctx, task, msg.ChatID, msg.Content)
	}

	return c.sendMarkdownMessage(ctx, msg.ChatID, msg.Content)
}

func parseWeComOfficialTemplateCardPayload(content string) (map[string]any, bool) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return nil, false
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil, false
	}
	msgType, _ := payload["msgtype"].(string)
	if msgType != "template_card" {
		return nil, false
	}
	templateCard, ok := payload["template_card"].(map[string]any)
	if !ok {
		return nil, false
	}
	return templateCard, true
}

func (c *WeComOfficialChannel) connectionLoop() {
	attempt := 0

	for {
		if c.ctx.Err() != nil || !c.IsRunning() {
			return
		}

		authenticated, err := c.connectAndServe()
		if c.ctx.Err() != nil || !c.IsRunning() {
			return
		}
		if err != nil {
			if errors.Is(err, errWeComOfficialAuthFailed) {
				logger.ErrorCF("wecom_official", "Authentication failed, stopping reconnect loop", map[string]any{
					"error": err.Error(),
				})
				return
			}
			logger.WarnCF("wecom_official", "Connection loop ended", map[string]any{
				"error": err.Error(),
			})
		}

		if authenticated {
			attempt = 0
		}
		attempt++
		if attempt > wecomOfficialMaxReconnects {
			logger.ErrorCF("wecom_official", "Max reconnect attempts reached", map[string]any{
				"attempts": wecomOfficialMaxReconnects,
			})
			return
		}

		delay := c.reconnectDelay(attempt)
		logger.InfoCF("wecom_official", "Scheduling reconnect", map[string]any{
			"attempt": attempt,
			"delay":   delay.String(),
		})

		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-c.ctx.Done():
			timer.Stop()
			return
		}
	}
}

func (c *WeComOfficialChannel) connectAndServe() (bool, error) {
	dialer := websocket.Dialer{
		HandshakeTimeout: wecomOfficialDialTimeout,
	}

	conn, _, err := dialer.DialContext(c.ctx, c.config.WebSocketURL, nil)
	if err != nil {
		return false, channels.ClassifyNetError(err)
	}
	c.setConn(conn)
	logger.InfoCF("wecom_official", "WebSocket connected", map[string]any{
		"url": c.config.WebSocketURL,
	})

	readErrCh := make(chan error, 1)
	go c.readLoop(conn, readErrCh)

	authCtx, cancel := context.WithTimeout(c.ctx, wecomOfficialAuthTimeout)
	defer cancel()

	_, err = c.sendCommandAndWait(authCtx, wecomOfficialCmdSubscribe, map[string]string{
		"bot_id": c.config.BotID,
		"secret": c.config.Secret,
	}, wecomOfficialAuthTimeout)
	if err != nil {
		c.closeConn(conn, err)
		if errors.Is(err, channels.ErrSendFailed) {
			return false, fmt.Errorf("%w: %v", errWeComOfficialAuthFailed, err)
		}
		return false, err
	}

	logger.InfoC("wecom_official", "Authentication successful")

	heartbeatErrCh := make(chan error, 1)
	go c.heartbeatLoop(heartbeatErrCh)

	select {
	case err := <-readErrCh:
		c.closeConn(conn, err)
		if err == nil {
			return true, nil
		}
		return true, channels.ClassifyNetError(err)
	case err := <-heartbeatErrCh:
		c.closeConn(conn, err)
		if err == nil {
			return true, nil
		}
		return true, err
	case <-c.ctx.Done():
		c.closeConn(conn, c.ctx.Err())
		return true, c.ctx.Err()
	}
}

func (c *WeComOfficialChannel) readLoop(conn *websocket.Conn, errCh chan<- error) {
	defer func() {
		select {
		case errCh <- io.EOF:
		default:
		}
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			select {
			case errCh <- err:
			default:
			}
			return
		}

		var frame wecomOfficialFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			logger.WarnCF("wecom_official", "Failed to parse websocket frame", map[string]any{
				"error": err.Error(),
				"body":  string(data),
			})
			continue
		}

		if c.deliverPendingAck(frame) {
			continue
		}

		switch frame.Cmd {
		case wecomOfficialCmdMessageCallback, wecomOfficialCmdEventCallback:
			go c.handleCallbackFrame(frame)
		default:
			logger.DebugCF("wecom_official", "Ignoring unmatched websocket frame", map[string]any{
				"cmd":    frame.Cmd,
				"req_id": frame.Headers.ReqID,
			})
		}
	}
}

func (c *WeComOfficialChannel) heartbeatLoop(errCh chan<- error) {
	ticker := time.NewTicker(wecomOfficialHeartbeatInterval)
	defer ticker.Stop()

	missed := 0

	for {
		select {
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(c.ctx, wecomOfficialHeartbeatAckTimeout)
			_, err := c.sendCommandAndWait(pingCtx, wecomOfficialCmdHeartbeat, nil, wecomOfficialHeartbeatAckTimeout)
			cancel()
			if err != nil {
				missed++
				logger.WarnCF("wecom_official", "Heartbeat failed", map[string]any{
					"missed": missed,
					"error":  err.Error(),
				})
				if missed >= wecomOfficialMaxMissedHeartbeats {
					select {
					case errCh <- err:
					default:
					}
					return
				}
				continue
			}
			missed = 0
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *WeComOfficialChannel) handleCallbackFrame(frame wecomOfficialFrame) {
	if len(frame.Body) == 0 {
		return
	}

	var msg wecomOfficialMessage
	if err := json.Unmarshal(frame.Body, &msg); err != nil {
		logger.ErrorCF("wecom_official", "Failed to decode callback payload", map[string]any{
			"cmd":   frame.Cmd,
			"error": err.Error(),
		})
		return
	}

	c.processIncomingMessage(frame, msg)
}

func (c *WeComOfficialChannel) processIncomingMessage(frame wecomOfficialFrame, msg wecomOfficialMessage) {
	if msg.MsgID != "" && !c.processedMsgs.MarkMessageProcessed(msg.MsgID) {
		logger.DebugCF("wecom_official", "Skipping duplicate message", map[string]any{
			"msgid": msg.MsgID,
		})
		return
	}

	userID := strings.TrimSpace(msg.From.UserID)
	if userID == "" {
		userID = "unknown"
	}

	chatID := strings.TrimSpace(msg.ChatID)
	if chatID == "" {
		chatID = userID
	}
	isGroupChat := msg.ChatType == "group"

	sender := bus.SenderInfo{
		Platform:    "wecom_official",
		PlatformID:  userID,
		CanonicalID: identity.BuildCanonicalID("wecom_official", userID),
		DisplayName: userID,
	}
	if !c.IsAllowedSender(sender) {
		return
	}
	if msg.MsgType == "event" {
		c.handleEventMessage(frame, chatID, msg)
		return
	}

	content, mediaSources, quoteContent := parseWeComOfficialMessage(msg)
	if isGroupChat {
		content = strings.TrimSpace(wecomOfficialAtPattern.ReplaceAllString(content, ""))
	}
	if content == "" && quoteContent != "" {
		content = quoteContent
	}

	mediaRefs, mediaTags := c.downloadIncomingMedia(chatID, msg.MsgID, mediaSources)
	content = appendWeComOfficialMediaTags(content, mediaTags)
	if content == "" && len(mediaRefs) == 0 {
		return
	}

	peer := bus.Peer{Kind: "direct", ID: chatID}
	if isGroupChat {
		peer = bus.Peer{Kind: "group", ID: chatID}
		respond, cleaned := c.ShouldRespondInGroup(false, content)
		if !respond {
			return
		}
		content = cleaned
	}

	metadata := map[string]string{
		"msg_type":  msg.MsgType,
		"chat_type": msg.ChatType,
		"msgid":     msg.MsgID,
		"aibotid":   msg.AIBotID,
		"req_id":    frame.Headers.ReqID,
		"reply_to":  frame.Headers.ReqID,
	}
	if msg.ResponseURL != "" {
		metadata["response_url"] = msg.ResponseURL
	}
	if quoteContent != "" {
		metadata["quote_content"] = quoteContent
	}

	logger.DebugCF("wecom_official", "Received message", map[string]any{
		"chat_id":   chatID,
		"sender_id": userID,
		"msg_type":  msg.MsgType,
		"preview":   utils.Truncate(content, 80),
	})

	task := c.enqueueReplyTask(
		chatID,
		frame.Headers.ReqID,
		wecomOfficialReplyModeStream,
		"",
		msg.ResponseURL,
		msg.ChatType,
	)
	c.maybeSendThinkingPlaceholder(task)
	c.HandleMessage(c.ctx, peer, msg.MsgID, userID, chatID, content, mediaRefs, metadata, sender)
}

func (c *WeComOfficialChannel) handleEventMessage(frame wecomOfficialFrame, chatID string, msg wecomOfficialMessage) {
	if msg.Event == nil {
		return
	}
	switch msg.Event.EventType {
	case "enter_chat":
		c.handleEnterChatEvent(frame, chatID)
	case "template_card_event":
		c.handleTemplateCardEvent(frame, chatID, msg)
	default:
		logger.DebugCF("wecom_official", "Ignoring event callback", map[string]any{
			"event_type": msg.Event.EventType,
		})
	}
}

func (c *WeComOfficialChannel) handleEnterChatEvent(frame wecomOfficialFrame, chatID string) {
	welcome := strings.TrimSpace(c.config.WelcomeMessage)
	if welcome == "" {
		return
	}

	ctx, cancel := context.WithTimeout(c.ctx, wecomOfficialSendTimeout)
	defer cancel()

	sendWelcome := c.sendWelcomeText
	if c.cardEnabled() {
		sendWelcome = c.sendWelcomeTemplateCard
	}

	if err := sendWelcome(ctx, frame.Headers.ReqID, welcome); err != nil {
		logger.ErrorCF("wecom_official", "Failed to send welcome message", map[string]any{
			"chat_id": chatID,
			"error":   err.Error(),
		})
	}
}

func (c *WeComOfficialChannel) handleTemplateCardEvent(
	frame wecomOfficialFrame,
	chatID string,
	msg wecomOfficialMessage,
) {
	event := msg.templateCardEvent()
	if event == nil {
		return
	}

	userID := strings.TrimSpace(msg.From.UserID)
	if userID == "" {
		userID = "unknown"
	}
	if chatID == "" {
		chatID = userID
	}

	cardType := strings.TrimSpace(event.CardType)
	eventKey := strings.TrimSpace(event.EventKey)
	taskID := strings.TrimSpace(event.TaskID)
	actionText, cardContext := c.describeTemplateCardEvent(taskID, eventKey)
	selectedItemsText, selectedItemsJSON := c.describeTemplateCardSelectedItems(taskID, event.SelectedItems)
	if actionText == "" {
		actionText = humanizeTemplateCardEventKey(eventKey)
	}
	autoUpdated := false
	if taskID != "" {
		updateCtx, cancel := context.WithTimeout(c.ctx, wecomOfficialSendTimeout)
		updated, err := c.sendAutomaticTemplateCardEventUpdate(updateCtx, frame.Headers.ReqID, taskID, eventKey)
		cancel()
		if err != nil {
			logger.WarnCF("wecom_official", "Failed to auto-update template card event", map[string]any{
				"chat_id":     chatID,
				"callback_id": frame.Headers.ReqID,
				"task_id":     taskID,
				"event_key":   eventKey,
				"error":       err.Error(),
			})
		} else {
			autoUpdated = updated
		}
	}
	c.enqueueReplyTask(
		chatID,
		frame.Headers.ReqID,
		wecomOfficialReplyModeUpdateTemplateCard,
		taskID,
		msg.ResponseURL,
		msg.ChatType,
	)
	content := buildTemplateCardEventUserContent(actionText, eventKey, cardContext, selectedItemsText)

	peer := bus.Peer{Kind: "direct", ID: chatID}
	if msg.ChatType == "group" {
		peer = bus.Peer{Kind: "group", ID: chatID}
	}

	metadata := map[string]string{
		"msg_type":    msg.MsgType,
		"chat_type":   msg.ChatType,
		"msgid":       msg.MsgID,
		"aibotid":     msg.AIBotID,
		"create_time": fmt.Sprintf("%d", msg.CreateTime),
		"req_id":      frame.Headers.ReqID,
		"reply_to":    frame.Headers.ReqID,
		"event_type":  msg.Event.EventType,
	}
	if cardType != "" {
		metadata["card_type"] = cardType
	}
	if eventKey != "" {
		metadata["event_key"] = eventKey
	}
	if taskID != "" {
		metadata["task_id"] = taskID
	}
	if actionText != "" {
		metadata["event_action_text"] = actionText
	}
	if cardContext != "" {
		metadata["card_context"] = cardContext
	}
	if selectedItemsText != "" {
		metadata["selected_items_text"] = selectedItemsText
	}
	if selectedItemsJSON != "" {
		metadata["selected_items_json"] = selectedItemsJSON
	}
	if autoUpdated {
		metadata["card_auto_updated"] = "true"
	}
	if msg.ResponseURL != "" {
		metadata["response_url"] = msg.ResponseURL
	}

	sender := bus.SenderInfo{
		Platform:    "wecom_official",
		PlatformID:  userID,
		CanonicalID: identity.BuildCanonicalID("wecom_official", userID),
		DisplayName: userID,
	}
	logger.DebugCF("wecom_official", "Received template card event", map[string]any{
		"chat_id":     chatID,
		"sender_id":   userID,
		"card_type":   cardType,
		"event_key":   eventKey,
		"task_id":     taskID,
		"callback_id": frame.Headers.ReqID,
	})
	c.HandleMessage(c.ctx, peer, msg.MsgID, userID, chatID, content, nil, metadata, sender)
}

func buildTemplateCardEventUserContent(actionText, eventKey, cardContext, selectedItemsText string) string {
	parts := make([]string, 0, 3)
	switch {
	case strings.TrimSpace(actionText) != "":
		parts = append(parts, fmt.Sprintf("User clicked template card action: %s.", strings.TrimSpace(actionText)))
	case strings.TrimSpace(eventKey) != "":
		parts = append(parts, fmt.Sprintf("User clicked template card action key: %s.", strings.TrimSpace(eventKey)))
	default:
		parts = append(parts, "User clicked a template card action.")
	}
	if strings.TrimSpace(selectedItemsText) != "" {
		parts = append(parts, fmt.Sprintf("Selected items: %s.", strings.TrimSpace(selectedItemsText)))
	}
	if strings.TrimSpace(cardContext) != "" {
		parts = append(parts, fmt.Sprintf("Card context: %s.", strings.TrimSpace(cardContext)))
	}
	return strings.Join(parts, " ")
}

func (c *WeComOfficialChannel) describeTemplateCardEvent(taskID, eventKey string) (string, string) {
	card := c.cardState(taskID)
	if card == nil {
		return "", ""
	}

	actionText := strings.TrimSpace(lookupTemplateCardActionText(card, eventKey))
	contextParts := make([]string, 0, 6)
	if mainTitle, ok := card["main_title"].(map[string]any); ok {
		if title, _ := mainTitle["title"].(string); strings.TrimSpace(title) != "" {
			contextParts = append(contextParts, strings.TrimSpace(title))
		}
		if desc, _ := mainTitle["desc"].(string); strings.TrimSpace(desc) != "" {
			contextParts = append(contextParts, strings.TrimSpace(desc))
		}
	}
	if subTitle, _ := card["sub_title_text"].(string); strings.TrimSpace(subTitle) != "" {
		contextParts = append(contextParts, strings.TrimSpace(subTitle))
	}
	if emphasisContent, ok := card["emphasis_content"].(map[string]any); ok {
		title, _ := emphasisContent["title"].(string)
		desc, _ := emphasisContent["desc"].(string)
		title = strings.TrimSpace(title)
		desc = strings.TrimSpace(desc)
		switch {
		case title != "" && desc != "":
			contextParts = append(contextParts, fmt.Sprintf("%s: %s", title, desc))
		case title != "":
			contextParts = append(contextParts, title)
		case desc != "":
			contextParts = append(contextParts, desc)
		}
	}
	if horizontalList, ok := card["horizontal_content_list"].([]any); ok {
		for _, raw := range horizontalList {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			keyname, _ := item["keyname"].(string)
			value, _ := item["value"].(string)
			keyname = strings.TrimSpace(keyname)
			value = strings.TrimSpace(value)
			if keyname == "" || value == "" {
				continue
			}
			contextParts = append(contextParts, fmt.Sprintf("%s=%s", keyname, value))
			if len(contextParts) >= 4 {
				break
			}
		}
	}
	if verticalList, ok := card["vertical_content_list"].([]any); ok {
		for _, raw := range verticalList {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			title, _ := item["title"].(string)
			desc, _ := item["desc"].(string)
			title = strings.TrimSpace(title)
			desc = strings.TrimSpace(desc)
			switch {
			case title != "" && desc != "":
				contextParts = append(contextParts, fmt.Sprintf("%s: %s", title, desc))
			case title != "":
				contextParts = append(contextParts, title)
			case desc != "":
				contextParts = append(contextParts, desc)
			}
			if len(contextParts) >= 6 {
				break
			}
		}
	}
	return actionText, strings.Join(contextParts, ", ")
}

func lookupTemplateCardActionText(card map[string]any, eventKey string) string {
	eventKey = strings.TrimSpace(eventKey)
	if eventKey == "" {
		return ""
	}

	if buttonList, ok := card["button_list"].([]any); ok {
		for _, raw := range buttonList {
			button, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			key, _ := button["key"].(string)
			if strings.TrimSpace(key) == eventKey {
				text, _ := button["text"].(string)
				return strings.TrimSpace(text)
			}
		}
	}
	if actionMenu, ok := card["action_menu"].(map[string]any); ok {
		if actionList, ok := actionMenu["action_list"].([]any); ok {
			for _, raw := range actionList {
				item, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				key, _ := item["key"].(string)
				if strings.TrimSpace(key) == eventKey {
					text, _ := item["text"].(string)
					return strings.TrimSpace(text)
				}
			}
		}
	}
	if submitButton, ok := card["submit_button"].(map[string]any); ok {
		key, _ := submitButton["key"].(string)
		if strings.TrimSpace(key) == eventKey {
			text, _ := submitButton["text"].(string)
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func humanizeTemplateCardEventKey(eventKey string) string {
	eventKey = strings.TrimSpace(eventKey)
	if eventKey == "" {
		return ""
	}
	eventKey = strings.ReplaceAll(eventKey, "_", " ")
	eventKey = strings.ReplaceAll(eventKey, "-", " ")
	return strings.TrimSpace(eventKey)
}

func (c *WeComOfficialChannel) describeTemplateCardSelectedItems(
	taskID string,
	selectedItems *wecomOfficialTemplateCardSelectedItems,
) (string, string) {
	if selectedItems == nil || len(selectedItems.SelectedItem) == 0 {
		return "", ""
	}

	raw, err := json.Marshal(selectedItems.SelectedItem)
	if err != nil {
		raw = nil
	}

	card := c.cardState(taskID)
	parts := make([]string, 0, len(selectedItems.SelectedItem))
	for _, item := range selectedItems.SelectedItem {
		questionKey := strings.TrimSpace(item.QuestionKey)
		optionIDs := make([]string, 0)
		if item.OptionIDs != nil {
			optionIDs = append(optionIDs, item.OptionIDs.OptionID...)
		}
		questionTitle, optionTexts := lookupTemplateCardSelectionText(card, questionKey, optionIDs)
		label := questionKey
		if strings.TrimSpace(questionTitle) != "" {
			label = strings.TrimSpace(questionTitle)
		}
		displayOptions := optionIDs
		if len(optionTexts) > 0 {
			displayOptions = optionTexts
		}
		switch {
		case label != "" && len(displayOptions) > 0:
			parts = append(parts, fmt.Sprintf("%s=%s", label, strings.Join(displayOptions, ",")))
		case label != "":
			parts = append(parts, label)
		case len(displayOptions) > 0:
			parts = append(parts, strings.Join(displayOptions, ","))
		}
	}

	return strings.Join(parts, "; "), string(raw)
}

func lookupTemplateCardSelectionText(card map[string]any, questionKey string, optionIDs []string) (string, []string) {
	if card == nil {
		return "", nil
	}
	findOptionTexts := func(optionList []any, wanted []string) []string {
		if len(wanted) == 0 {
			return nil
		}
		result := make([]string, 0, len(wanted))
		seen := make(map[string]struct{}, len(wanted))
		for _, raw := range optionList {
			option, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id, _ := option["id"].(string)
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			for _, wantedID := range wanted {
				if strings.TrimSpace(wantedID) != id {
					continue
				}
				if _, ok := seen[id]; ok {
					continue
				}
				text, _ := option["text"].(string)
				text = strings.TrimSpace(text)
				if text == "" {
					text = id
				}
				result = append(result, text)
				seen[id] = struct{}{}
			}
		}
		return result
	}

	matchQuestion := func(section map[string]any) (string, []string, bool) {
		key, _ := section["question_key"].(string)
		if strings.TrimSpace(key) != strings.TrimSpace(questionKey) {
			return "", nil, false
		}
		title, _ := section["title"].(string)
		optionList, _ := section["option_list"].([]any)
		return strings.TrimSpace(title), findOptionTexts(optionList, optionIDs), true
	}

	if buttonSelection, ok := card["button_selection"].(map[string]any); ok {
		if title, optionTexts, ok := matchQuestion(buttonSelection); ok {
			return title, optionTexts
		}
	}
	if checkbox, ok := card["checkbox"].(map[string]any); ok {
		if title, optionTexts, ok := matchQuestion(checkbox); ok {
			return title, optionTexts
		}
	}
	if selectList, ok := card["select_list"].([]any); ok {
		for _, raw := range selectList {
			section, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if title, optionTexts, ok := matchQuestion(section); ok {
				return title, optionTexts
			}
		}
	}
	return "", nil
}

func parseWeComOfficialMessage(msg wecomOfficialMessage) (string, []wecomOfficialMediaSource, string) {
	textParts := make([]string, 0, 4)
	mediaSources := make([]wecomOfficialMediaSource, 0, 4)
	var quoteContent string

	addImage := func(url, aesKey string) {
		if strings.TrimSpace(url) == "" {
			return
		}
		mediaSources = append(mediaSources, wecomOfficialMediaSource{
			Kind:   "image",
			URL:    url,
			AESKey: aesKey,
		})
	}
	addFile := func(url, aesKey string) {
		if strings.TrimSpace(url) == "" {
			return
		}
		mediaSources = append(mediaSources, wecomOfficialMediaSource{
			Kind:   "file",
			URL:    url,
			AESKey: aesKey,
		})
	}

	if msg.MsgType == "mixed" && msg.Mixed != nil {
		for _, item := range msg.Mixed.MsgItem {
			switch item.MsgType {
			case "text":
				if item.Text != nil && item.Text.Content != "" {
					textParts = append(textParts, item.Text.Content)
				}
			case "image":
				if item.Image != nil {
					addImage(item.Image.URL, item.Image.AESKey)
				}
			}
		}
	} else {
		if msg.Text != nil && msg.Text.Content != "" {
			textParts = append(textParts, msg.Text.Content)
		}
		if msg.MsgType == "voice" && msg.Voice != nil && msg.Voice.Content != "" {
			textParts = append(textParts, msg.Voice.Content)
		}
		if msg.Image != nil {
			addImage(msg.Image.URL, msg.Image.AESKey)
		}
		if msg.File != nil {
			addFile(msg.File.URL, msg.File.AESKey)
		}
	}

	if msg.Quote != nil {
		switch msg.Quote.MsgType {
		case "text":
			if msg.Quote.Text != nil {
				quoteContent = msg.Quote.Text.Content
			}
		case "voice":
			if msg.Quote.Voice != nil {
				quoteContent = msg.Quote.Voice.Content
			}
		case "image":
			if msg.Quote.Image != nil {
				addImage(msg.Quote.Image.URL, msg.Quote.Image.AESKey)
			}
		case "file":
			if msg.Quote.File != nil {
				addFile(msg.Quote.File.URL, msg.Quote.File.AESKey)
			}
		}
	}

	return strings.TrimSpace(strings.Join(textParts, "\n")), mediaSources, strings.TrimSpace(quoteContent)
}

func appendWeComOfficialMediaTags(content string, tags []string) string {
	if len(tags) == 0 {
		return strings.TrimSpace(content)
	}

	tagText := strings.Join(tags, " ")
	content = strings.TrimSpace(content)
	if content == "" {
		return tagText
	}
	return strings.TrimSpace(content + " " + tagText)
}

func (c *WeComOfficialChannel) downloadIncomingMedia(
	chatID, messageID string,
	sources []wecomOfficialMediaSource,
) ([]string, []string) {
	if len(sources) == 0 {
		return nil, nil
	}

	refs := make([]string, 0, len(sources))
	tags := make([]string, 0, len(sources))
	scope := channels.BuildMediaScope(c.Name(), chatID, messageID)
	store := c.GetMediaStore()

	for idx, source := range sources {
		tags = append(tags, mediaTagForWeComOfficialSource(source.Kind))
		if store == nil {
			continue
		}

		localPath, meta, err := c.downloadMediaSource(source, messageID, idx)
		if err != nil {
			logger.ErrorCF("wecom_official", "Failed to download inbound media", map[string]any{
				"url":   source.URL,
				"kind":  source.Kind,
				"error": err.Error(),
			})
			continue
		}

		ref, err := store.Store(localPath, meta, scope)
		if err != nil {
			_ = os.Remove(localPath)
			logger.ErrorCF("wecom_official", "Failed to store inbound media", map[string]any{
				"url":   source.URL,
				"error": err.Error(),
			})
			continue
		}
		refs = append(refs, ref)
	}

	return refs, tags
}

func (c *WeComOfficialChannel) downloadMediaSource(
	source wecomOfficialMediaSource,
	messageID string,
	index int,
) (string, media.MediaMeta, error) {
	req, err := http.NewRequestWithContext(c.ctx, http.MethodGet, source.URL, nil)
	if err != nil {
		return "", media.MediaMeta{}, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", media.MediaMeta{}, channels.ClassifyNetError(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", media.MediaMeta{}, channels.ClassifySendError(
			resp.StatusCode,
			fmt.Errorf("wecom media download returned status %d", resp.StatusCode),
		)
	}

	maxBytes := int64(config.DefaultMaxMediaSize)
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return "", media.MediaMeta{}, err
	}
	if int64(len(data)) > maxBytes {
		return "", media.MediaMeta{}, fmt.Errorf("wecom media exceeds max size: %d", len(data))
	}

	if source.AESKey != "" {
		data, err = decryptWeComOfficialMedia(data, source.AESKey)
		if err != nil {
			return "", media.MediaMeta{}, err
		}
	}

	filename := deriveWeComOfficialFilename(resp.Header.Get("Content-Disposition"), source.URL, messageID, index)
	contentType, ext := detectWeComOfficialMediaType(data, source.Kind)
	if filepath.Ext(filename) == "" && ext != "" {
		filename += ext
	}

	mediaDir := filepath.Join(os.TempDir(), "picoclaw_media")
	if err := os.MkdirAll(mediaDir, 0o700); err != nil {
		return "", media.MediaMeta{}, err
	}

	localPath := filepath.Join(
		mediaDir,
		uuid.NewString()[:8]+"_"+utils.SanitizeFilename(filename),
	)
	if err := os.WriteFile(localPath, data, 0o600); err != nil {
		return "", media.MediaMeta{}, err
	}

	return localPath, media.MediaMeta{
		Filename:    filename,
		ContentType: contentType,
		Source:      "wecom_official",
	}, nil
}

func decryptWeComOfficialMedia(data []byte, aesKey string) ([]byte, error) {
	key, err := decodeWeComOfficialMediaAESKey(aesKey)
	if err != nil {
		return nil, fmt.Errorf("decode media aes key: %w", err)
	}
	return decryptAESCBC(key, data)
}

func decodeWeComOfficialMediaAESKey(aesKey string) ([]byte, error) {
	aesKey = strings.TrimSpace(aesKey)
	if aesKey == "" {
		return nil, fmt.Errorf("empty aes key")
	}

	if mod := len(aesKey) % 4; mod != 0 {
		aesKey += strings.Repeat("=", 4-mod)
	}

	key, err := base64.StdEncoding.DecodeString(aesKey)
	if err != nil {
		key, err = base64.URLEncoding.DecodeString(aesKey)
		if err != nil {
			return nil, err
		}
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid media aes key length: %d", len(key))
	}
	return key, nil
}

func deriveWeComOfficialFilename(
	contentDisposition, rawURL, messageID string,
	index int,
) string {
	if filename := parseWeComOfficialContentDisposition(contentDisposition); filename != "" {
		return filename
	}

	if u, err := url.Parse(rawURL); err == nil {
		if base := path.Base(u.Path); base != "." && base != "/" && base != "" {
			return base
		}
	}

	return fmt.Sprintf("wecom-%s-%d", messageID, index)
}

func parseWeComOfficialContentDisposition(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}

	_, params, err := mime.ParseMediaType(value)
	if err != nil {
		return ""
	}

	if name := params["filename*"]; name != "" {
		if strings.HasPrefix(strings.ToLower(name), "utf-8''") {
			name = name[7:]
		}
		if decoded, err := url.QueryUnescape(name); err == nil {
			return decoded
		}
		return name
	}

	if name := params["filename"]; name != "" {
		return name
	}

	return ""
}

func detectWeComOfficialMediaType(data []byte, kind string) (string, string) {
	if matched, err := filetype.Match(data); err == nil && matched != filetype.Unknown {
		return matched.MIME.Value, "." + matched.Extension
	}

	sniffLen := min(len(data), 512)
	contentType := http.DetectContentType(data[:sniffLen])

	switch contentType {
	case "image/png":
		return contentType, ".png"
	case "image/jpeg":
		return contentType, ".jpg"
	case "image/gif":
		return contentType, ".gif"
	case "application/pdf":
		return contentType, ".pdf"
	case "audio/mpeg":
		return contentType, ".mp3"
	}

	if kind == "image" {
		return "image/jpeg", ".jpg"
	}
	return contentType, ".bin"
}

func mediaTagForWeComOfficialSource(kind string) string {
	switch kind {
	case "image":
		return "[image: photo]"
	case "file":
		return "[file]"
	default:
		return "[attachment]"
	}
}

func normalizeWeComOfficialCardText(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}

	content = wecomOfficialCardReplacer.Replace(content)
	content = wecomOfficialCardHeadingRE.ReplaceAllString(content, "")
	content = wecomOfficialCardQuoteRE.ReplaceAllString(content, "")
	content = wecomOfficialCardBlankRE.ReplaceAllString(content, "\n\n")

	lines := strings.Split(content, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		cleaned = append(cleaned, strings.TrimRight(line, " \t"))
	}

	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func truncateWeComOfficialCardText(text string, limit int) string {
	if limit <= 0 {
		return text
	}

	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

func buildWeComOfficialTemplateCard(title, content string) map[string]any {
	plain := normalizeWeComOfficialCardText(content)
	card := map[string]any{
		"card_type": "text_notice",
		"main_title": map[string]string{
			"title": strings.TrimSpace(title),
		},
		// text_notice cards require a valid card_action in current WeCom validation.
		"card_action": map[string]any{
			"type": 1,
			"url":  "https://work.weixin.qq.com/",
		},
	}
	if plain == "" {
		return card
	}

	card["sub_title_text"] = truncateWeComOfficialCardText(plain, wecomOfficialCardSummaryLimit)
	if strings.Contains(plain, "\n") || len([]rune(plain)) > wecomOfficialCardSummaryLimit {
		card["quote_area"] = map[string]string{
			"title":      "Reply",
			"quote_text": truncateWeComOfficialCardText(plain, wecomOfficialCardQuoteLimit),
		}
	}
	return card
}

func (c *WeComOfficialChannel) sendMarkdownMessage(
	ctx context.Context,
	chatID, content string,
) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	_, err := c.sendCommandAndWait(ctx, wecomOfficialCmdSendMessage, map[string]any{
		"chatid":  chatID,
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": content,
		},
	}, wecomOfficialSendTimeout)
	return err
}

func (c *WeComOfficialChannel) sendViaResponseURL(
	ctx context.Context,
	responseURL string,
	payload map[string]any,
) error {
	responseURL = strings.TrimSpace(responseURL)
	if responseURL == "" {
		return fmt.Errorf("empty response_url: %w", channels.ErrSendFailed)
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal response_url payload: %w", err)
	}

	requestCtx := ctx
	cancel := func() {}
	if _, ok := ctx.Deadline(); !ok {
		requestCtx, cancel = context.WithTimeout(ctx, wecomOfficialSendTimeout)
	}
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, responseURL, bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("create response_url request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	client := &http.Client{Timeout: wecomOfficialSendTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post to response_url failed: %w: %w", channels.ErrTemporary, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response_url body: %w: %w", channels.ErrTemporary, err)
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return fmt.Errorf("response_url rate limited (%d): %s: %w", resp.StatusCode, respBody, channels.ErrRateLimit)
	case resp.StatusCode >= 500:
		return fmt.Errorf("response_url server error (%d): %s: %w", resp.StatusCode, respBody, channels.ErrTemporary)
	default:
		return fmt.Errorf("response_url returned %d: %s: %w", resp.StatusCode, respBody, channels.ErrSendFailed)
	}
}

func (c *WeComOfficialChannel) sendMarkdownViaResponseURL(
	ctx context.Context,
	task *wecomOfficialReplyTask,
	content string,
) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	if task == nil {
		return fmt.Errorf("missing reply task for response_url markdown: %w", channels.ErrSendFailed)
	}
	return c.sendViaResponseURL(ctx, task.ResponseURL, map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]string{
			"content": content,
		},
	})
}

func (c *WeComOfficialChannel) sendTemplateCardViaResponseURL(
	ctx context.Context,
	task *wecomOfficialReplyTask,
	templateCard map[string]any,
) error {
	if task == nil {
		return fmt.Errorf("missing reply task for response_url template_card: %w", channels.ErrSendFailed)
	}
	return c.sendViaResponseURL(ctx, task.ResponseURL, map[string]any{
		"msgtype":       "template_card",
		"template_card": templateCard,
	})
}

func (c *WeComOfficialChannel) sendTemplateCardMessage(
	ctx context.Context,
	chatID, content string,
) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	_, err := c.sendCommandAndWait(ctx, wecomOfficialCmdSendMessage, map[string]any{
		"chatid":        chatID,
		"msgtype":       "template_card",
		"template_card": buildWeComOfficialTemplateCard(c.cardTitle(), content),
	}, wecomOfficialSendTimeout)
	return err
}

func (c *WeComOfficialChannel) canUseResponseURLLocked(task *wecomOfficialReplyTask) bool {
	if task == nil || strings.TrimSpace(task.ResponseURL) == "" {
		return false
	}
	return time.Since(task.CreatedAt) < wecomOfficialResponseURLExpiry
}

func (c *WeComOfficialChannel) canUseResponseURL(task *wecomOfficialReplyTask) bool {
	if task == nil {
		return false
	}
	task.mu.Lock()
	defer task.mu.Unlock()
	return c.canUseResponseURLLocked(task)
}

func (c *WeComOfficialChannel) canSendTemplateCardViaResponseURL(task *wecomOfficialReplyTask) bool {
	if !c.canUseResponseURL(task) {
		return false
	}
	task.mu.Lock()
	defer task.mu.Unlock()
	return strings.TrimSpace(task.ChatType) != "group"
}

func (c *WeComOfficialChannel) finalizeReplyTask(task *wecomOfficialReplyTask) {
	if task == nil {
		return
	}
	task.mu.Lock()
	if task.timer != nil {
		task.timer.Stop()
		task.timer = nil
	}
	task.finalized = true
	task.mu.Unlock()
	c.removeReplyTask(task)
}

func (c *WeComOfficialChannel) closeReplyStreamForFollowUp(
	ctx context.Context,
	task *wecomOfficialReplyTask,
) error {
	if task == nil {
		return nil
	}

	task.mu.Lock()
	if task.closedNaturally || task.finalized || task.ResponseMode != wecomOfficialReplyModeStream {
		task.mu.Unlock()
		return nil
	}
	if task.timer != nil {
		task.timer.Stop()
		task.timer = nil
	}
	reqID := task.ReqID
	streamID := task.StreamID
	content := strings.TrimSpace(task.accumulated)
	task.closedNaturally = true
	task.mu.Unlock()

	closingText := c.streamClosingText()
	switch {
	case content == "":
		content = closingText
	case !strings.Contains(content, closingText):
		content = strings.TrimSpace(content + "\n\n" + closingText)
	}
	return c.sendReplyStream(ctx, reqID, streamID, content, true)
}

func (c *WeComOfficialChannel) sendDeferredMarkdown(
	ctx context.Context,
	task *wecomOfficialReplyTask,
	chatID, content string,
) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	if task != nil && c.canUseResponseURL(task) {
		if err := c.sendMarkdownViaResponseURL(ctx, task, content); err != nil {
			return err
		}
		c.finalizeReplyTask(task)
		return nil
	}
	if err := c.sendMarkdownMessage(ctx, chatID, content); err != nil {
		return err
	}
	if task != nil {
		c.finalizeReplyTask(task)
	}
	return nil
}

func (c *WeComOfficialChannel) sendDeferredTemplateCard(
	ctx context.Context,
	task *wecomOfficialReplyTask,
	chatID string,
	templateCard map[string]any,
) error {
	if task != nil && c.canSendTemplateCardViaResponseURL(task) {
		if err := c.sendTemplateCardViaResponseURL(ctx, task, templateCard); err != nil {
			return err
		}
		c.rememberTemplateCard(templateCard)
		c.finalizeReplyTask(task)
		return nil
	}
	_, err := c.sendCommandAndWait(ctx, wecomOfficialCmdSendMessage, map[string]any{
		"chatid":        chatID,
		"msgtype":       "template_card",
		"template_card": templateCard,
	}, wecomOfficialSendTimeout)
	if err != nil {
		return err
	}
	c.rememberTemplateCard(templateCard)
	if task != nil {
		c.finalizeReplyTask(task)
	}
	return nil
}

func (c *WeComOfficialChannel) sendReplyMarkdown(
	ctx context.Context,
	task *wecomOfficialReplyTask,
	chatID, content string,
) error {
	if task == nil {
		return c.sendMarkdownMessage(ctx, chatID, content)
	}
	if task.ResponseMode == wecomOfficialReplyModeUpdateTemplateCard {
		return c.sendDeferredMarkdown(ctx, task, chatID, content)
	}
	return c.sendReplyChunk(ctx, task, content)
}

func (c *WeComOfficialChannel) sendReplyChunk(
	ctx context.Context,
	task *wecomOfficialReplyTask,
	content string,
) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}

	task.mu.Lock()
	if task.finalized {
		task.mu.Unlock()
		c.removeReplyTask(task)
		return c.sendMarkdownMessage(ctx, task.ChatID, content)
	}
	if task.closedNaturally {
		task.mu.Unlock()
		return c.sendDeferredMarkdown(ctx, task, task.ChatID, content)
	}
	if time.Until(task.EditDeadline) <= c.streamCloseBeforeExpiry() {
		task.mu.Unlock()
		if err := c.closeReplyStreamForFollowUp(ctx, task); err != nil {
			return err
		}
		return c.sendDeferredMarkdown(ctx, task, task.ChatID, content)
	}
	if task.timer != nil {
		task.timer.Stop()
		task.timer = nil
	}
	task.accumulated += content
	task.sequence++
	seq := task.sequence
	accumulated := task.accumulated
	sendWithCard := c.cardEnabled() && !task.cardSent
	var err error
	if sendWithCard {
		err = c.sendReplyStreamWithCard(ctx, task.ReqID, task.StreamID, accumulated, false, true)
		if err == nil {
			task.cardSent = true
		}
	} else {
		err = c.sendReplyStream(ctx, task.ReqID, task.StreamID, accumulated, false)
	}
	if err != nil {
		task.mu.Unlock()
		return err
	}

	task.timer = time.AfterFunc(wecomOfficialReplyIdleClose, func() {
		c.finishReplyTask(task, seq)
	})
	task.mu.Unlock()
	return nil
}

func (c *WeComOfficialChannel) deliverDeferredFinal(task *wecomOfficialReplyTask) {
	if task == nil {
		return
	}
	task.mu.Lock()
	content := strings.TrimSpace(task.pendingFinal)
	task.pendingFinal = ""
	task.finalDeliveryPending = false
	chatID := task.ChatID
	task.mu.Unlock()
	if content == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), wecomOfficialSendTimeout)
	defer cancel()
	if err := c.sendMarkdownMessage(ctx, chatID, content); err != nil {
		logger.WarnCF("wecom_official", "Failed to deliver deferred final response", map[string]any{
			"chat_id": chatID,
			"error":   err.Error(),
		})
	}
}

func (c *WeComOfficialChannel) maybeSendThinkingPlaceholder(task *wecomOfficialReplyTask) {
	if task == nil || !c.placeholderEnabled() {
		return
	}

	text := c.placeholderText()
	if strings.TrimSpace(text) == "" {
		return
	}

	ctx, cancel := context.WithTimeout(c.ctx, wecomOfficialSendTimeout)
	defer cancel()

	if err := c.sendReplyStream(ctx, task.ReqID, task.StreamID, text, false); err != nil {
		logger.WarnCF("wecom_official", "Failed to send thinking placeholder", map[string]any{
			"chat_id":   task.ChatID,
			"stream_id": task.StreamID,
			"req_id":    task.ReqID,
			"error":     err.Error(),
		})
	}
}

func (c *WeComOfficialChannel) sendReplyStream(
	ctx context.Context,
	reqID, streamID, content string,
	finish bool,
) error {
	_, err := c.sendCommandWithReqIDAndWait(ctx, reqID, wecomOfficialCmdRespondMessage, map[string]any{
		"msgtype": "stream",
		"stream": map[string]any{
			"id":      streamID,
			"finish":  finish,
			"content": content,
		},
	}, wecomOfficialSendTimeout)
	return err
}

func (c *WeComOfficialChannel) sendReplyStreamWithCard(
	ctx context.Context,
	reqID, streamID, content string,
	finish bool,
	includeCard bool,
) error {
	body := map[string]any{
		"msgtype": "stream_with_template_card",
		"stream": map[string]any{
			"id":      streamID,
			"finish":  finish,
			"content": content,
		},
	}
	if includeCard {
		body["template_card"] = buildWeComOfficialTemplateCard(c.cardTitle(), content)
	}

	_, err := c.sendCommandWithReqIDAndWait(ctx, reqID, wecomOfficialCmdRespondMessage, body, wecomOfficialSendTimeout)
	return err
}

func (c *WeComOfficialChannel) sendReplyTemplateCard(
	ctx context.Context,
	task *wecomOfficialReplyTask,
	templateCard map[string]any,
) error {
	if task == nil {
		return nil
	}

	task.mu.Lock()
	if task.finalized {
		task.mu.Unlock()
		c.removeReplyTask(task)
		return c.sendDeferredTemplateCard(ctx, nil, task.ChatID, templateCard)
	}
	if task.ResponseMode == wecomOfficialReplyModeUpdateTemplateCard &&
		time.Now().After(task.EditDeadline) {
		task.mu.Unlock()
		return fmt.Errorf("wecom_official template card callback window expired; `template_card_event` updates must be sent within 5 seconds and later replies should use the callback response_url or a new markdown message: %w", channels.ErrSendFailed)
	}
	if task.ResponseMode == wecomOfficialReplyModeStream &&
		(task.closedNaturally || time.Until(task.EditDeadline) <= c.streamCloseBeforeExpiry()) {
		task.mu.Unlock()
		if err := c.closeReplyStreamForFollowUp(ctx, task); err != nil {
			return err
		}
		return c.sendDeferredTemplateCard(ctx, task, task.ChatID, templateCard)
	}
	if task.timer != nil {
		task.timer.Stop()
		task.timer = nil
	}
	task.sequence++
	if err := c.sendReplyTemplateCardPayload(ctx, task, templateCard); err != nil {
		task.mu.Unlock()
		return err
	}
	task.cardSent = true
	if task.ResponseMode == wecomOfficialReplyModeUpdateTemplateCard {
		task.finalized = true
		task.mu.Unlock()
		c.removeReplyTask(task)
		return nil
	}
	task.mu.Unlock()
	return nil
}

func (c *WeComOfficialChannel) sendReplyTemplateCardPayload(
	ctx context.Context,
	task *wecomOfficialReplyTask,
	templateCard map[string]any,
) error {
	if task == nil {
		return nil
	}
	if task.ResponseMode == wecomOfficialReplyModeUpdateTemplateCard {
		if task.TaskID != "" {
			templateCard["task_id"] = task.TaskID
		}
		_, err := c.sendCommandWithReqIDAndWait(ctx, task.ReqID, wecomOfficialCmdRespondUpdate, map[string]any{
			"response_type": "update_template_card",
			"template_card": templateCard,
		}, wecomOfficialSendTimeout)
		if err == nil {
			c.rememberTemplateCard(templateCard)
		}
		return err
	}
	_, err := c.sendCommandWithReqIDAndWait(ctx, task.ReqID, wecomOfficialCmdRespondMessage, map[string]any{
		"msgtype":       "template_card",
		"template_card": templateCard,
	}, wecomOfficialSendTimeout)
	if err == nil {
		c.rememberTemplateCard(templateCard)
	}
	return err
}

func (c *WeComOfficialChannel) rememberTemplateCard(templateCard map[string]any) {
	taskID := templateCardTaskID(templateCard)
	if taskID == "" {
		return
	}
	cloned, err := cloneTemplateCard(templateCard)
	if err != nil {
		logger.WarnCF("wecom_official", "Failed to clone template card state", map[string]any{
			"task_id": taskID,
			"error":   err.Error(),
		})
		return
	}
	c.cardMu.Lock()
	defer c.cardMu.Unlock()
	c.cardStates[taskID] = cloned
}

func (c *WeComOfficialChannel) cardState(taskID string) map[string]any {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil
	}
	c.cardMu.RLock()
	card := c.cardStates[taskID]
	c.cardMu.RUnlock()
	if card == nil {
		return nil
	}
	cloned, err := cloneTemplateCard(card)
	if err != nil {
		return nil
	}
	return cloned
}

func (c *WeComOfficialChannel) sendAutomaticTemplateCardEventUpdate(
	ctx context.Context,
	reqID, taskID, eventKey string,
) (bool, error) {
	card := c.cardState(taskID)
	if card == nil {
		return false, nil
	}
	updated := applyAutomaticTemplateCardEventMutation(card, eventKey)
	if !updated {
		return false, nil
	}
	_, err := c.sendCommandWithReqIDAndWait(ctx, reqID, wecomOfficialCmdRespondUpdate, map[string]any{
		"response_type": "update_template_card",
		"template_card": card,
	}, wecomOfficialSendTimeout)
	if err != nil {
		return false, err
	}
	c.rememberTemplateCard(card)
	return true, nil
}

func applyAutomaticTemplateCardEventMutation(card map[string]any, eventKey string) bool {
	cardType, _ := card["card_type"].(string)
	eventKey = strings.TrimSpace(eventKey)
	updated := false

	if subTitle, ok := card["sub_title_text"].(string); ok {
		if strings.TrimSpace(subTitle) != "已收到操作，处理中..." {
			card["sub_title_text"] = "已收到操作，处理中..."
			updated = true
		}
	} else {
		card["sub_title_text"] = "已收到操作，处理中..."
		updated = true
	}

	switch cardType {
	case "button_interaction":
		if buttonList, ok := card["button_list"].([]any); ok {
			for _, raw := range buttonList {
				button, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				key, _ := button["key"].(string)
				if eventKey != "" && key == eventKey {
					button["text"] = "处理中"
					button["style"] = 3
					updated = true
				}
			}
		}
		if selection, ok := card["button_selection"].(map[string]any); ok {
			selection["disable"] = true
			updated = true
		}
	case "vote_interaction":
		if checkbox, ok := card["checkbox"].(map[string]any); ok {
			checkbox["disable"] = true
			updated = true
		}
		if submit, ok := card["submit_button"].(map[string]any); ok {
			submit["text"] = "处理中"
			updated = true
		}
	case "multiple_interaction":
		if selectList, ok := card["select_list"].([]any); ok {
			for _, raw := range selectList {
				item, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				item["disable"] = true
				updated = true
			}
		}
		if submit, ok := card["submit_button"].(map[string]any); ok {
			submit["text"] = "处理中"
			updated = true
		}
	}

	return updated
}

func templateCardTaskID(card map[string]any) string {
	taskID, _ := card["task_id"].(string)
	return strings.TrimSpace(taskID)
}

func cloneTemplateCard(card map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(card)
	if err != nil {
		return nil, err
	}
	var cloned map[string]any
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return nil, err
	}
	return cloned, nil
}

func templateCardStreamContent(card map[string]any) string {
	parts := make([]string, 0, 3)
	if mainTitle, ok := card["main_title"].(map[string]any); ok {
		if title, _ := mainTitle["title"].(string); strings.TrimSpace(title) != "" {
			parts = append(parts, strings.TrimSpace(title))
		}
		if desc, _ := mainTitle["desc"].(string); strings.TrimSpace(desc) != "" {
			parts = append(parts, strings.TrimSpace(desc))
		}
	}
	if subTitle, _ := card["sub_title_text"].(string); strings.TrimSpace(subTitle) != "" {
		parts = append(parts, strings.TrimSpace(subTitle))
	}
	if len(parts) == 0 {
		return "Template card updated"
	}
	return strings.Join(parts, "\n")
}

func (c *WeComOfficialChannel) sendWelcomeText(ctx context.Context, reqID, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	_, err := c.sendCommandWithReqIDAndWait(ctx, reqID, wecomOfficialCmdRespondWelcome, map[string]any{
		"msgtype": "text",
		"text": map[string]string{
			"content": content,
		},
	}, wecomOfficialSendTimeout)
	return err
}

func (c *WeComOfficialChannel) sendWelcomeTemplateCard(ctx context.Context, reqID, content string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	_, err := c.sendCommandWithReqIDAndWait(ctx, reqID, wecomOfficialCmdRespondWelcome, map[string]any{
		"msgtype":       "template_card",
		"template_card": buildWeComOfficialTemplateCard(c.cardTitle(), content),
	}, wecomOfficialSendTimeout)
	return err
}

func (c *WeComOfficialChannel) sendCommandAndWait(
	ctx context.Context,
	cmd string,
	body any,
	timeout time.Duration,
) (wecomOfficialFrame, error) {
	reqID := generateWeComOfficialReqID(cmd)
	return c.sendCommandWithReqIDAndWait(ctx, reqID, cmd, body, timeout)
}

func (c *WeComOfficialChannel) sendCommandWithReqIDAndWait(
	ctx context.Context,
	reqID string,
	cmd string,
	body any,
	timeout time.Duration,
) (wecomOfficialFrame, error) {
	var zero wecomOfficialFrame
	if !c.IsRunning() {
		return zero, channels.ErrNotRunning
	}

	waitCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	waiter := c.registerPendingAck(reqID)

	if err := c.writeFrame(wecomOfficialCommandFrame{
		Cmd:     cmd,
		Headers: wecomOfficialHeaders{ReqID: reqID},
		Body:    body,
	}); err != nil {
		c.unregisterPendingAck(reqID)
		return zero, channels.ClassifyNetError(err)
	}

	select {
	case result := <-waiter:
		if result.err != nil {
			return zero, result.err
		}
		if result.frame.ErrCode != 0 {
			return result.frame, fmt.Errorf(
				"wecom_official %s ack error errcode=%d errmsg=%s: %w",
				cmd,
				result.frame.ErrCode,
				result.frame.ErrMsg,
				channels.ErrSendFailed,
			)
		}
		return result.frame, nil
	case <-waitCtx.Done():
		c.unregisterPendingAck(reqID)
		if errors.Is(waitCtx.Err(), context.Canceled) && !c.IsRunning() {
			return zero, channels.ErrNotRunning
		}
		return zero, channels.ClassifyNetError(waitCtx.Err())
	}
}

func (c *WeComOfficialChannel) enqueueReplyTask(
	chatID, reqID, responseMode, taskID, responseURL, chatType string,
) *wecomOfficialReplyTask {
	chatID = strings.TrimSpace(chatID)
	reqID = strings.TrimSpace(reqID)
	if chatID == "" || reqID == "" {
		return nil
	}
	responseMode = strings.TrimSpace(responseMode)
	if responseMode == "" {
		responseMode = wecomOfficialReplyModeStream
	}

	c.taskMu.Lock()
	defer c.taskMu.Unlock()

	c.compactReplyTasksLocked(chatID)
	editDeadline := time.Now().Add(wecomOfficialStreamUpdateExpiry)
	if responseMode == wecomOfficialReplyModeUpdateTemplateCard {
		editDeadline = time.Now().Add(wecomOfficialUpdateTaskMaxAge)
	}
	task := &wecomOfficialReplyTask{
		ReqID:        reqID,
		ChatID:       chatID,
		ChatType:     strings.TrimSpace(chatType),
		CreatedAt:    time.Now(),
		EditDeadline: editDeadline,
		ResponseMode: responseMode,
		ResponseURL:  strings.TrimSpace(responseURL),
		TaskID:       strings.TrimSpace(taskID),
	}
	if responseMode == wecomOfficialReplyModeStream {
		task.StreamID = generateWeComOfficialReqID("stream")
	}
	c.replyTasks[chatID] = append(c.replyTasks[chatID], task)
	return task
}

func (c *WeComOfficialChannel) activeReplyTask(chatID, reqID string) *wecomOfficialReplyTask {
	chatID = strings.TrimSpace(chatID)
	reqID = strings.TrimSpace(reqID)
	if chatID == "" || reqID == "" {
		return nil
	}

	c.taskMu.Lock()
	defer c.taskMu.Unlock()

	c.compactReplyTasksLocked(chatID)
	queue := c.replyTasks[chatID]
	for _, task := range queue {
		if task != nil && task.ReqID == reqID {
			return task
		}
	}
	return nil
}

func (c *WeComOfficialChannel) compactReplyTasksLocked(chatID string) {
	queue := c.replyTasks[chatID]
	now := time.Now()
	for len(queue) > 0 {
		head := queue[0]
		maxAge := wecomOfficialReplyTaskMaxAge
		if head != nil {
			switch {
			case strings.TrimSpace(head.ResponseURL) != "":
				maxAge = wecomOfficialResponseURLExpiry
			case head.ResponseMode == wecomOfficialReplyModeUpdateTemplateCard:
				maxAge = wecomOfficialUpdateTaskMaxAge
			}
		}
		expired := now.Sub(head.CreatedAt) > maxAge
		head.mu.Lock()
		finalized := head.finalized
		if finalized || expired {
			if head.timer != nil {
				head.timer.Stop()
				head.timer = nil
			}
		}
		head.mu.Unlock()
		if !finalized && !expired {
			break
		}
		queue = queue[1:]
	}
	if len(queue) == 0 {
		delete(c.replyTasks, chatID)
		return
	}
	c.replyTasks[chatID] = queue
}

func (c *WeComOfficialChannel) removeReplyTask(task *wecomOfficialReplyTask) {
	if task == nil {
		return
	}

	c.taskMu.Lock()
	defer c.taskMu.Unlock()

	queue := c.replyTasks[task.ChatID]
	for i, candidate := range queue {
		if candidate == task {
			queue = append(queue[:i], queue[i+1:]...)
			break
		}
	}
	if len(queue) == 0 {
		delete(c.replyTasks, task.ChatID)
		return
	}
	c.replyTasks[task.ChatID] = queue
}

func (c *WeComOfficialChannel) finishReplyTask(task *wecomOfficialReplyTask, seq uint64) {
	if task == nil {
		return
	}

	task.mu.Lock()
	if task.finalized || seq != task.sequence {
		task.mu.Unlock()
		return
	}
	accumulated := task.accumulated

	ctx, cancel := context.WithTimeout(context.Background(), wecomOfficialSendTimeout)
	sendFinal := func() error {
		return c.sendReplyStream(ctx, task.ReqID, task.StreamID, accumulated, true)
	}
	err := sendFinal()
	cancel()
	if err != nil {
		logger.WarnCF("wecom_official", "Failed to finish reply stream", map[string]any{
			"chat_id":     task.ChatID,
			"stream_id":   task.StreamID,
			"callback_id": task.ReqID,
			"error":       err.Error(),
		})
	}
	task.finalized = true
	task.timer = nil
	task.mu.Unlock()

	c.removeReplyTask(task)
}

func (c *WeComOfficialChannel) writeFrame(frame wecomOfficialCommandFrame) error {
	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()
	if conn == nil {
		return fmt.Errorf("wecom_official websocket not connected")
	}

	payload, err := json.Marshal(frame)
	if err != nil {
		return err
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := conn.SetWriteDeadline(time.Now().Add(wecomOfficialWriteTimeout)); err != nil {
		return err
	}
	if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
		return err
	}
	return nil
}

func (c *WeComOfficialChannel) registerPendingAck(reqID string) chan wecomOfficialWaitResult {
	ch := make(chan wecomOfficialWaitResult, 1)
	c.pendingMu.Lock()
	c.pendingAcks[reqID] = ch
	c.pendingMu.Unlock()
	return ch
}

func (c *WeComOfficialChannel) unregisterPendingAck(reqID string) {
	c.pendingMu.Lock()
	delete(c.pendingAcks, reqID)
	c.pendingMu.Unlock()
}

func (c *WeComOfficialChannel) deliverPendingAck(frame wecomOfficialFrame) bool {
	reqID := frame.Headers.ReqID
	if reqID == "" {
		return false
	}

	c.pendingMu.Lock()
	ch, ok := c.pendingAcks[reqID]
	if ok {
		delete(c.pendingAcks, reqID)
	}
	c.pendingMu.Unlock()
	if !ok {
		return false
	}

	select {
	case ch <- wecomOfficialWaitResult{frame: frame}:
	default:
	}
	return true
}

func (c *WeComOfficialChannel) failPendingAcks(reason error) {
	if reason == nil {
		reason = fmt.Errorf("wecom_official connection closed")
	}

	wrapped := channels.ClassifyNetError(reason)

	c.pendingMu.Lock()
	pending := c.pendingAcks
	c.pendingAcks = make(map[string]chan wecomOfficialWaitResult)
	c.pendingMu.Unlock()

	for _, ch := range pending {
		select {
		case ch <- wecomOfficialWaitResult{err: wrapped}:
		default:
		}
	}
}

func (c *WeComOfficialChannel) setConn(conn *websocket.Conn) {
	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()
}

func (c *WeComOfficialChannel) closeConn(expected *websocket.Conn, reason error) {
	c.connMu.Lock()
	if expected != nil && c.conn != expected {
		c.connMu.Unlock()
		return
	}
	conn := c.conn
	c.conn = nil
	c.connMu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}
	c.failPendingAcks(reason)
	c.clearReplyTasks()
}

func (c *WeComOfficialChannel) clearReplyTasks() {
	c.taskMu.Lock()
	tasks := c.replyTasks
	c.replyTasks = make(map[string][]*wecomOfficialReplyTask)
	c.taskMu.Unlock()

	for _, queue := range tasks {
		for _, task := range queue {
			task.mu.Lock()
			if task.timer != nil {
				task.timer.Stop()
				task.timer = nil
			}
			task.finalized = true
			task.mu.Unlock()
		}
	}
}

func (c *WeComOfficialChannel) reconnectDelay(attempt int) time.Duration {
	delay := time.Second * time.Duration(1<<min(attempt-1, 5))
	if delay > 30*time.Second {
		return 30 * time.Second
	}
	return delay
}

func generateWeComOfficialReqID(prefix string) string {
	token := strings.ReplaceAll(uuid.NewString(), "-", "")
	return fmt.Sprintf("%s-%s", prefix, token)
}
