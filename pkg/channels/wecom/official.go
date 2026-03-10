package wecom

import (
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
	wecomOfficialCmdRespondWelcome  = "aibot_respond_welcome_msg"
	wecomOfficialCmdSendMessage     = "aibot_send_msg"
	wecomOfficialCmdMessageCallback = "aibot_msg_callback"
	wecomOfficialCmdEventCallback   = "aibot_event_callback"

	wecomOfficialMaxMessageLength    = 4000
	wecomOfficialMaxReconnects       = 100
	wecomOfficialMaxMissedHeartbeats = 2

	wecomOfficialDialTimeout          = 15 * time.Second
	wecomOfficialWriteTimeout         = 10 * time.Second
	wecomOfficialAuthTimeout          = 10 * time.Second
	wecomOfficialSendTimeout          = 15 * time.Second
	wecomOfficialHeartbeatInterval    = 30 * time.Second
	wecomOfficialHeartbeatAckTimeout  = 10 * time.Second
	wecomOfficialMediaDownloadTimeout = 30 * time.Second
	wecomOfficialReplyIdleClose       = 250 * time.Millisecond
	wecomOfficialReplyTaskMaxAge      = 10 * time.Minute
)

var (
	errWeComOfficialAuthFailed = errors.New("wecom official authentication failed")
	wecomOfficialAtPattern     = regexp.MustCompile(`@\S+`)
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
	Event *struct {
		EventType string `json:"eventtype"`
		EventKey  string `json:"event_key,omitempty"`
		TaskID    string `json:"task_id,omitempty"`
	} `json:"event,omitempty"`
}

type wecomOfficialMediaSource struct {
	Kind   string
	URL    string
	AESKey string
}

type wecomOfficialReplyTask struct {
	ReqID     string
	ChatID    string
	StreamID  string
	CreatedAt time.Time

	accumulated string
	mu          sync.Mutex
	timer       *time.Timer
	sequence    uint64
	finalized   bool
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

	if task := c.activeReplyTask(msg.ChatID, msg.ReplyTo); task != nil {
		if err := c.sendReplyChunk(ctx, task, msg.Content); err != nil {
			return err
		}
		return nil
	}

	return c.sendMarkdownMessage(ctx, msg.ChatID, msg.Content)
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
	if msg.ChatType == "group" {
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
	if msg.ChatType == "group" {
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

	c.enqueueReplyTask(chatID, frame.Headers.ReqID)
	c.HandleMessage(c.ctx, peer, msg.MsgID, userID, chatID, content, mediaRefs, metadata, sender)
}

func (c *WeComOfficialChannel) handleEventMessage(frame wecomOfficialFrame, chatID string, msg wecomOfficialMessage) {
	if msg.Event == nil {
		return
	}
	if msg.Event.EventType != "enter_chat" {
		logger.DebugCF("wecom_official", "Ignoring event callback", map[string]any{
			"event_type": msg.Event.EventType,
		})
		return
	}

	welcome := strings.TrimSpace(c.config.WelcomeMessage)
	if welcome == "" {
		return
	}

	ctx, cancel := context.WithTimeout(c.ctx, wecomOfficialSendTimeout)
	defer cancel()

	if err := c.sendWelcomeText(ctx, frame.Headers.ReqID, welcome); err != nil {
		logger.ErrorCF("wecom_official", "Failed to send welcome message", map[string]any{
			"chat_id": chatID,
			"error":   err.Error(),
		})
	}
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
	key, err := base64.StdEncoding.DecodeString(aesKey)
	if err != nil {
		return nil, fmt.Errorf("decode media aes key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid media aes key length: %d", len(key))
	}
	return decryptAESCBC(key, data)
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
		return nil
	}
	if task.timer != nil {
		task.timer.Stop()
		task.timer = nil
	}
	task.accumulated += content
	task.sequence++
	seq := task.sequence
	accumulated := task.accumulated

	err := c.sendReplyStream(ctx, task.ReqID, task.StreamID, accumulated, false)
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

func (c *WeComOfficialChannel) enqueueReplyTask(chatID, reqID string) {
	chatID = strings.TrimSpace(chatID)
	reqID = strings.TrimSpace(reqID)
	if chatID == "" || reqID == "" {
		return
	}

	c.taskMu.Lock()
	defer c.taskMu.Unlock()

	c.compactReplyTasksLocked(chatID)
	c.replyTasks[chatID] = append(c.replyTasks[chatID], &wecomOfficialReplyTask{
		ReqID:     reqID,
		ChatID:    chatID,
		StreamID:  generateWeComOfficialReqID("stream"),
		CreatedAt: time.Now(),
	})
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
		expired := now.Sub(head.CreatedAt) > wecomOfficialReplyTaskMaxAge
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
	err := c.sendReplyStream(ctx, task.ReqID, task.StreamID, accumulated, true)
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
