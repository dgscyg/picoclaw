package claweb

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/identity"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/media"
	"github.com/sipeed/picoclaw/pkg/utils"
)

const (
	clawebChannelName     = "claweb"
	clawebDefaultHost     = "127.0.0.1"
	clawebDefaultPort     = 18999
	clawebReadTimeout     = 90 * time.Second
	clawebWriteTimeout    = 15 * time.Second
	clawebPingInterval    = 30 * time.Second
	clawebMaxInboundBytes = 40 * 1024 * 1024
	clawebDownloadTimeout = 30 * time.Second
	clawebServerVersion   = "picoclaw"
	clawebAttachmentText  = "[attachment]"
	clawebImageText       = "[image]"
	clawebVideoText       = "[video]"
)

type clawebConn struct {
	id        string
	conn      *websocket.Conn
	clientID  string
	userID    string
	roomID    string
	chatID    string
	authed    atomic.Bool
	closed    atomic.Bool
	writeMu   sync.Mutex
	pingStop  chan struct{}
	pingDone  chan struct{}
	closeOnce sync.Once
}

func (c *clawebConn) writeJSON(v any) error {
	if c.closed.Load() {
		return fmt.Errorf("claweb connection closed")
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := c.conn.SetWriteDeadline(time.Now().Add(clawebWriteTimeout)); err != nil {
		return err
	}
	if err := c.conn.WriteJSON(v); err != nil {
		return err
	}
	_ = c.conn.SetWriteDeadline(time.Time{})
	return nil
}

func (c *clawebConn) close() {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		if c.pingStop != nil {
			close(c.pingStop)
		}
		_ = c.conn.Close()
		if c.pingDone != nil {
			<-c.pingDone
		}
	})
}

type ClawebChannel struct {
	*channels.BaseChannel
	config      config.ClawebConfig
	httpClient  *http.Client
	upgrader    websocket.Upgrader
	authToken   string
	ctx         context.Context
	cancel      context.CancelFunc
	server      *http.Server
	listenAddr  string
	connections sync.Map
	activeTurns sync.Map
	lastSentMu  sync.Mutex
	lastSentIDs map[string]string
	connCount   atomic.Int32
}

func NewClawebChannel(cfg config.ClawebConfig, messageBus *bus.MessageBus) (*ClawebChannel, error) {
	authToken, err := resolveAuthToken(cfg.AuthToken, cfg.AuthTokenFile)
	if err != nil {
		return nil, err
	}
	if authToken == "" {
		return nil, fmt.Errorf("claweb auth_token or auth_token_file is required")
	}

	if strings.TrimSpace(cfg.ListenHost) == "" {
		cfg.ListenHost = clawebDefaultHost
	}
	if cfg.ListenPort == 0 {
		cfg.ListenPort = clawebDefaultPort
	}

	base := channels.NewBaseChannel(
		clawebChannelName,
		cfg,
		messageBus,
		cfg.AllowFrom,
		channels.WithGroupTrigger(cfg.GroupTrigger),
		channels.WithReasoningChannelID(cfg.ReasoningChannelID),
	)

	return &ClawebChannel{
		BaseChannel: base,
		config:      cfg,
		authToken:   authToken,
		httpClient:  &http.Client{Timeout: clawebDownloadTimeout},
		upgrader: websocket.Upgrader{
			CheckOrigin:     func(r *http.Request) bool { return true },
			ReadBufferSize:  4096,
			WriteBufferSize: 4096,
		},
		lastSentIDs: make(map[string]string),
	}, nil
}

func (c *ClawebChannel) Start(ctx context.Context) error {
	if c.IsRunning() {
		return nil
	}

	c.ctx, c.cancel = context.WithCancel(ctx)
	addr := fmt.Sprintf("%s:%d", c.config.ListenHost, c.config.ListenPort)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("claweb listen %s: %w", addr, err)
	}

	c.server = &http.Server{
		Handler:      http.HandlerFunc(c.ServeHTTP),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	c.listenAddr = listener.Addr().String()

	c.SetRunning(true)
	go func() {
		if err := c.server.Serve(listener); err != nil && err != http.ErrServerClosed {
			logger.ErrorCF("claweb", "CLAWeb server stopped unexpectedly", map[string]any{
				"addr":  c.listenAddr,
				"error": err.Error(),
			})
		}
	}()

	logger.InfoCF("claweb", "CLAWeb channel started", map[string]any{"addr": c.listenAddr})
	return nil
}

func (c *ClawebChannel) Stop(ctx context.Context) error {
	if !c.IsRunning() {
		return nil
	}

	c.SetRunning(false)
	if c.cancel != nil {
		c.cancel()
	}
	if c.server != nil {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_ = c.server.Shutdown(shutdownCtx)
		c.server = nil
	}

	c.connections.Range(func(key, value any) bool {
		if conn, ok := value.(*clawebConn); ok {
			conn.close()
		}
		c.connections.Delete(key)
		return true
	})
	c.lastSentMu.Lock()
	c.lastSentIDs = make(map[string]string)
	c.lastSentMu.Unlock()

	logger.InfoC("claweb", "CLAWeb channel stopped")
	return nil
}

func (c *ClawebChannel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !c.IsRunning() {
		http.Error(w, "channel not running", http.StatusServiceUnavailable)
		return
	}

	switch r.URL.Path {
	case "", "/", "/ws":
	default:
		http.NotFound(w, r)
		return
	}

	if !websocket.IsWebSocketUpgrade(r) {
		http.Error(w, "websocket upgrade required", http.StatusUpgradeRequired)
		return
	}

	c.handleWebSocket(w, r)
}

func (c *ClawebChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return channels.ErrNotRunning
	}

	text := strings.TrimSpace(msg.Content)
	if text == "" {
		return nil
	}

	turnID := c.outboundTurnID(msg.ChatID, msg.ReplyTo)
	if c.shouldSuppressDuplicateTurn(msg.ChatID, turnID) {
		logger.InfoCF("claweb", "Suppressing duplicate assistant outbound for active turn", map[string]any{
			"chat_id": msg.ChatID,
			"turn_id": turnID,
		})
		return nil
	}
	frame := clawebMessageFrame{
		Type:      "message",
		ID:        turnID,
		MessageID: turnID,
		Role:      "assistant",
		Text:      text,
	}
	return c.broadcastToChatID(msg.ChatID, frame)
}

func (c *ClawebChannel) SendMedia(ctx context.Context, msg bus.OutboundMediaMessage) error {
	if !c.IsRunning() {
		return channels.ErrNotRunning
	}

	if len(msg.Parts) == 0 {
		return nil
	}

	store := c.GetMediaStore()
	if store == nil {
		return fmt.Errorf("claweb send media: no media store: %w", channels.ErrSendFailed)
	}

	part := msg.Parts[0]
	localPath, meta, err := store.ResolveWithMeta(part.Ref)
	if err != nil {
		return fmt.Errorf("claweb send media resolve %s: %w", part.Ref, err)
	}

	mediaType := strings.TrimSpace(part.ContentType)
	if mediaType == "" {
		mediaType = strings.TrimSpace(meta.ContentType)
	}
	filename := strings.TrimSpace(part.Filename)
	if filename == "" {
		filename = strings.TrimSpace(meta.Filename)
	}
	if filename == "" {
		filename = filepath.Base(localPath)
	}
	turnID := c.outboundTurnID(msg.ChatID, "")

	frame := clawebMessageFrame{
		Type:          "message",
		ID:            turnID,
		MessageID:     turnID,
		Role:          "assistant",
		Text:          strings.TrimSpace(part.Caption),
		MediaURL:      localPath,
		MediaType:     mediaType,
		MediaFilename: filename,
	}
	if frame.Text == "" {
		frame.Text = mediaTagForType(part.Type)
	}

	return c.broadcastToChatID(msg.ChatID, frame)
}

func (c *ClawebChannel) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := c.upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.ErrorCF("claweb", "WebSocket upgrade failed", map[string]any{"error": err.Error()})
		return
	}

	cc := &clawebConn{
		id:       uuid.NewString(),
		conn:     conn,
		pingStop: make(chan struct{}),
		pingDone: make(chan struct{}),
	}
	c.connections.Store(cc.id, cc)
	c.connCount.Add(1)

	go c.pingLoop(cc)
	go c.readLoop(cc)
}

func (c *ClawebChannel) pingLoop(conn *clawebConn) {
	defer close(conn.pingDone)

	ticker := time.NewTicker(clawebPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if conn.closed.Load() {
				return
			}
			conn.writeMu.Lock()
			err := conn.conn.WriteControl(
				websocket.PingMessage,
				nil,
				time.Now().Add(clawebWriteTimeout),
			)
			conn.writeMu.Unlock()
			if err != nil {
				return
			}
		case <-conn.pingStop:
			return
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *ClawebChannel) readLoop(conn *clawebConn) {
	defer func() {
		conn.close()
		c.connections.Delete(conn.id)
		c.connCount.Add(-1)
	}()

	_ = conn.conn.SetReadDeadline(time.Now().Add(clawebReadTimeout))
	conn.conn.SetPongHandler(func(appData string) error {
		return conn.conn.SetReadDeadline(time.Now().Add(clawebReadTimeout))
	})

	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		_, raw, err := conn.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.WarnCF("claweb", "WebSocket read failed", map[string]any{
					"conn_id": conn.id,
					"error":   err.Error(),
				})
			}
			return
		}
		_ = conn.conn.SetReadDeadline(time.Now().Add(clawebReadTimeout))

		if !conn.authed.Load() {
			if err := c.handleHelloFrame(conn, raw); err != nil {
				_ = conn.writeJSON(clawebErrorFrame{Type: "error", Message: err.Error()})
				return
			}
			continue
		}

		if err := c.handleClientMessage(conn, raw); err != nil {
			logger.WarnCF("claweb", "Failed to handle client frame", map[string]any{
				"conn_id": conn.id,
				"error":   err.Error(),
			})
		}
	}
}

func (c *ClawebChannel) handleHelloFrame(conn *clawebConn, raw []byte) error {
	var frame clawebHelloFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		return fmt.Errorf("invalid hello frame")
	}
	if frame.Type != "hello" {
		return fmt.Errorf("first frame must be hello")
	}
	if strings.TrimSpace(frame.Token) != c.authToken {
		return fmt.Errorf("auth failed")
	}

	clientID := strings.TrimSpace(frame.ClientID)
	if clientID == "" {
		clientID = uuid.NewString()
	}
	userID := strings.TrimSpace(frame.UserID)
	if userID == "" {
		userID = "web-user"
	}
	roomID := strings.TrimSpace(frame.RoomID)

	conn.clientID = clientID
	conn.userID = userID
	conn.roomID = roomID
	conn.chatID = buildChatID(userID, roomID, clientID)
	conn.authed.Store(true)

	logger.InfoCF("claweb", "WebSocket client authenticated", map[string]any{
		"conn_id":   conn.id,
		"client_id": clientID,
		"user_id":   userID,
		"room_id":   roomID,
	})

	return conn.writeJSON(clawebReadyFrame{Type: "ready", ServerVersion: clawebServerVersion})
}

func (c *ClawebChannel) handleClientMessage(conn *clawebConn, raw []byte) error {
	var frame clawebMessageFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		return conn.writeJSON(clawebErrorFrame{Type: "error", Message: "invalid_json"})
	}
	if frame.Type != "message" {
		return conn.writeJSON(clawebErrorFrame{Type: "error", Message: "unsupported frame"})
	}
	role := strings.ToLower(strings.TrimSpace(frame.Role))
	if role != "" && role != "user" && role != "human" {
		logger.WarnCF("claweb", "Ignoring non-user inbound frame", map[string]any{
			"conn_id": conn.id,
			"chat_id": conn.chatID,
			"role":    frame.Role,
		})
		return nil
	}

	messageID := strings.TrimSpace(frame.resolvedID())
	if messageID == "" {
		messageID = uuid.NewString()
	}
	content := strings.TrimSpace(frame.Text)

	mediaRefs, mediaTags := c.downloadIncomingMedia(
		conn.chatID,
		messageID,
		frame.MediaURL,
		frame.MediaDataURL,
		frame.MediaType,
		frame.MediaFilename,
	)
	content = buildInboundContent(content, mediaTags)
	if content == "" && len(mediaRefs) == 0 {
		return conn.writeJSON(clawebErrorFrame{Type: "error", ID: messageID, Message: "text is empty"})
	}

	peer := bus.Peer{Kind: "direct", ID: conn.userID}
	if conn.roomID != "" {
		peer = bus.Peer{Kind: "group", ID: conn.roomID}
		respond, cleaned := c.ShouldRespondInGroup(false, content)
		if !respond {
			return nil
		}
		content = cleaned
	}

	sender := bus.SenderInfo{
		Platform:    clawebChannelName,
		PlatformID:  conn.userID,
		CanonicalID: identity.BuildCanonicalID(clawebChannelName, conn.userID),
		DisplayName: conn.userID,
	}
	if !c.IsAllowedSender(sender) {
		return nil
	}

	metadata := map[string]string{
		"reply_to":   messageID,
		"client_id":  conn.clientID,
		"user_id":    conn.userID,
		"account_id": "default",
	}
	if conn.roomID != "" {
		metadata["room_id"] = conn.roomID
	}
	if replyTo := strings.TrimSpace(frame.ReplyTo); replyTo != "" {
		metadata["client_reply_to"] = replyTo
	}
	if preview := strings.TrimSpace(frame.ReplyPreview); preview != "" {
		metadata["client_reply_preview"] = preview
	}

	c.activeTurns.Store(conn.chatID, messageID)
	c.markInboundTurn(conn.chatID, messageID)

	logger.DebugCF("claweb", "Received message", map[string]any{
		"conn_id":     conn.id,
		"chat_id":     conn.chatID,
		"message_id":  messageID,
		"media_count": len(mediaRefs),
		"preview":     utils.Truncate(content, 80),
	})

	c.HandleMessage(c.ctx, peer, messageID, conn.userID, conn.chatID, content, mediaRefs, metadata, sender)
	return nil
}

func (c *ClawebChannel) markInboundTurn(chatID, turnID string) {
	chatID = strings.TrimSpace(chatID)
	turnID = strings.TrimSpace(turnID)
	if chatID == "" || turnID == "" {
		return
	}
	c.lastSentMu.Lock()
	delete(c.lastSentIDs, chatID)
	c.lastSentMu.Unlock()
}

func (c *ClawebChannel) shouldSuppressDuplicateTurn(chatID, turnID string) bool {
	chatID = strings.TrimSpace(chatID)
	turnID = strings.TrimSpace(turnID)
	if chatID == "" || turnID == "" {
		return false
	}
	c.lastSentMu.Lock()
	defer c.lastSentMu.Unlock()
	if prev := strings.TrimSpace(c.lastSentIDs[chatID]); prev != "" && prev == turnID {
		return true
	}
	c.lastSentIDs[chatID] = turnID
	return false
}

func (c *ClawebChannel) outboundTurnID(chatID, replyTo string) string {
	replyTo = strings.TrimSpace(replyTo)
	if replyTo != "" {
		return replyTo
	}
	if active, ok := c.activeTurns.Load(chatID); ok {
		if turnID, ok := active.(string); ok && strings.TrimSpace(turnID) != "" {
			return turnID
		}
	}
	return uuid.NewString()
}

func (c *ClawebChannel) broadcastToChatID(chatID string, frame clawebMessageFrame) error {
	if strings.TrimSpace(chatID) == "" {
		return fmt.Errorf("claweb send: empty chat id: %w", channels.ErrSendFailed)
	}

	var sent bool
	var lastErr error
	c.connections.Range(func(key, value any) bool {
		conn, ok := value.(*clawebConn)
		if !ok || conn.chatID != chatID || !conn.authed.Load() {
			return true
		}
		if err := conn.writeJSON(frame); err != nil {
			lastErr = err
			logger.WarnCF("claweb", "Failed to write frame", map[string]any{
				"conn_id": conn.id,
				"chat_id": chatID,
				"error":   err.Error(),
			})
			return true
		}
		sent = true
		return true
	})

	if sent {
		return nil
	}
	if lastErr != nil {
		return fmt.Errorf("claweb send: %w", channels.ErrTemporary)
	}
	return fmt.Errorf("claweb send: no active connections for %s: %w", chatID, channels.ErrSendFailed)
}

func (c *ClawebChannel) downloadIncomingMedia(
	chatID, messageID, mediaURL, mediaDataURL, mediaType, mediaFilename string,
) ([]string, []string) {
	mediaURL = strings.TrimSpace(mediaURL)
	mediaDataURL = strings.TrimSpace(mediaDataURL)
	if mediaURL == "" && mediaDataURL == "" {
		return nil, nil
	}

	store := c.GetMediaStore()
	kind := inferInboundMediaKind(mediaType, mediaFilename)
	tags := []string{mediaTagForType(kind)}
	if store == nil {
		return nil, tags
	}

	localPath, resolvedType, resolvedName, err := c.downloadInboundFile(
		messageID,
		mediaURL,
		mediaDataURL,
		mediaType,
		mediaFilename,
	)
	if err != nil {
		logger.WarnCF("claweb", "Failed to download inbound media", map[string]any{
			"chat_id":    chatID,
			"message_id": messageID,
			"error":      err.Error(),
		})
		return nil, tags
	}

	scope := channels.BuildMediaScope(clawebChannelName, chatID, messageID)
	ref, err := store.Store(localPath, media.MediaMeta{
		Filename:    resolvedName,
		ContentType: resolvedType,
		Source:      clawebChannelName,
	}, scope)
	if err != nil {
		_ = os.Remove(localPath)
		logger.WarnCF("claweb", "Failed to store inbound media", map[string]any{
			"chat_id":    chatID,
			"message_id": messageID,
			"error":      err.Error(),
		})
		return nil, tags
	}

	return []string{ref}, tags
}

func (c *ClawebChannel) downloadInboundFile(
	messageID, mediaURL, mediaDataURL, mediaType, mediaFilename string,
) (string, string, string, error) {
	switch {
	case mediaDataURL != "":
		return saveDataURLToTemp(messageID, mediaDataURL, mediaType, mediaFilename)
	case strings.HasPrefix(strings.ToLower(mediaURL), "http://"),
		strings.HasPrefix(strings.ToLower(mediaURL), "https://"):
		return c.downloadRemoteFile(messageID, mediaURL, mediaType, mediaFilename)
	default:
		if strings.TrimSpace(mediaURL) == "" {
			return "", "", "", fmt.Errorf("empty media reference")
		}
		if _, err := os.Stat(mediaURL); err != nil {
			return "", "", "", err
		}
		filename := strings.TrimSpace(mediaFilename)
		if filename == "" {
			filename = filepath.Base(mediaURL)
		}
		return mediaURL, strings.TrimSpace(mediaType), filename, nil
	}
}

func (c *ClawebChannel) downloadRemoteFile(
	messageID, rawURL, mediaType, mediaFilename string,
) (string, string, string, error) {
	req, err := http.NewRequestWithContext(c.ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, clawebMaxInboundBytes+1))
	if err != nil {
		return "", "", "", err
	}
	if len(data) > clawebMaxInboundBytes {
		return "", "", "", fmt.Errorf("media exceeds max size")
	}

	contentType := strings.TrimSpace(mediaType)
	if contentType == "" {
		contentType = strings.TrimSpace(resp.Header.Get("Content-Type"))
	}

	filename := strings.TrimSpace(mediaFilename)
	if filename == "" {
		filename = filenameFromResponse(resp.Header.Get("Content-Disposition"), rawURL)
	}
	return writeTempFile(messageID, data, contentType, filename)
}

func saveDataURLToTemp(
	messageID, dataURL, mediaType, mediaFilename string,
) (string, string, string, error) {
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return "", "", "", fmt.Errorf("invalid data url")
	}

	header := strings.TrimPrefix(parts[0], "data:")
	if idx := strings.Index(header, ";"); idx >= 0 {
		if strings.TrimSpace(mediaType) == "" {
			mediaType = header[:idx]
		}
	}

	data, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", "", "", err
	}

	return writeTempFile(messageID, data, mediaType, mediaFilename)
}

func writeTempFile(
	messageID string,
	data []byte,
	mediaType string,
	mediaFilename string,
) (string, string, string, error) {
	if len(data) == 0 {
		return "", "", "", fmt.Errorf("empty media payload")
	}

	mediaDir := filepath.Join(os.TempDir(), "picoclaw_media")
	if err := os.MkdirAll(mediaDir, 0o700); err != nil {
		return "", "", "", err
	}

	filename := strings.TrimSpace(mediaFilename)
	if filename == "" {
		filename = "attachment"
	}
	filename = utils.SanitizeFilename(filename)
	if filepath.Ext(filename) == "" {
		if ext := extFromMediaType(mediaType); ext != "" {
			filename += ext
		}
	}

	localPath := filepath.Join(mediaDir, uuid.NewString()[:8]+"_"+messageID+"_"+filename)
	if err := os.WriteFile(localPath, data, 0o600); err != nil {
		return "", "", "", err
	}

	if strings.TrimSpace(mediaType) == "" {
		mediaType = http.DetectContentType(data)
	}

	return localPath, mediaType, filename, nil
}

func resolveAuthToken(inlineToken, tokenFile string) (string, error) {
	if trimmed := strings.TrimSpace(inlineToken); trimmed != "" {
		return trimmed, nil
	}
	if strings.TrimSpace(tokenFile) == "" {
		return "", nil
	}

	raw, err := os.ReadFile(strings.TrimSpace(tokenFile))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

func buildChatID(userID, roomID, clientID string) string {
	parts := []string{clawebChannelName, "client", clientID, "user", userID}
	if roomID != "" {
		parts = append(parts, "room", roomID)
	}
	return strings.Join(parts, ":")
}

func buildInboundContent(content string, mediaTags []string) string {
	content = strings.TrimSpace(content)
	tagText := strings.TrimSpace(strings.Join(mediaTags, " "))
	switch {
	case content != "" && tagText != "":
		return strings.TrimSpace(content + "\n\n" + tagText)
	case content != "":
		return content
	default:
		return tagText
	}
}

func mediaTagForType(kind string) string {
	switch strings.TrimSpace(kind) {
	case "image":
		return clawebImageText
	case "video":
		return clawebVideoText
	default:
		return clawebAttachmentText
	}
}

func inferInboundMediaKind(mediaType, filename string) string {
	mt := strings.ToLower(strings.TrimSpace(mediaType))
	switch {
	case strings.HasPrefix(mt, "image/"):
		return "image"
	case strings.HasPrefix(mt, "video/"):
		return "video"
	}

	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg":
		return "image"
	case ".mp4", ".mov", ".webm", ".mkv", ".avi":
		return "video"
	default:
		return "file"
	}
}

func extFromMediaType(mediaType string) string {
	if strings.TrimSpace(mediaType) == "" {
		return ""
	}
	exts, err := mime.ExtensionsByType(strings.TrimSpace(mediaType))
	if err != nil || len(exts) == 0 {
		return ""
	}
	return exts[0]
}

func filenameFromResponse(contentDisposition, rawURL string) string {
	if strings.TrimSpace(contentDisposition) != "" {
		if _, params, err := mime.ParseMediaType(contentDisposition); err == nil {
			if name := strings.TrimSpace(params["filename"]); name != "" {
				return name
			}
			if name := strings.TrimSpace(params["filename*"]); name != "" {
				if strings.HasPrefix(strings.ToLower(name), "utf-8''") {
					name = name[7:]
				}
				if decoded, err := url.QueryUnescape(name); err == nil {
					return decoded
				}
				return name
			}
		}
	}

	if parsed, err := url.Parse(rawURL); err == nil {
		if base := path.Base(parsed.Path); base != "" && base != "." && base != "/" {
			return base
		}
	}
	return "attachment"
}
