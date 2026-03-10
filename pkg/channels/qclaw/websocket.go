package qclaw

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/logger"
)

// WebSocketClientCallbacks defines callbacks for WebSocket events.
type WebSocketClientCallbacks interface {
	OnPrompt(payload PromptPayload)
	OnCancel(payload CancelPayload)
}

// WebSocketClient implements the AGP WebSocket client.
type WebSocketClient struct {
	url              string
	token            string
	guid             string
	userID           string
	heartbeatInterval time.Duration
	reconnectInterval time.Duration
	maxReconnects    int

	conn        *websocket.Conn
	connMu      sync.RWMutex
	running     bool
	runningMu   sync.Mutex

	callbacks WebSocketClientCallbacks

	// Message deduplication
	processedMsgIds *lru.Cache[string, struct{}]
	msgIdCleanup    *time.Ticker

	// Reconnection state
	reconnectAttempts int
	reconnectTimer    *time.Timer

	// Wakeup detection
	lastTickTime time.Time
	wakeupTicker *time.Ticker

	// Context management
	ctx    context.Context
	cancel context.CancelFunc

	// Write mutex for thread-safe writes
	writeMu sync.Mutex
}

// NewWebSocketClient creates a new WebSocket client.
func NewWebSocketClient(url, token, guid, userID string, callbacks WebSocketClientCallbacks) *WebSocketClient {
	processedMsgIds, _ := lru.New[string, struct{}](MaxProcessedMessages)

	return &WebSocketClient{
		url:               url,
		token:             token,
		guid:              guid,
		userID:            userID,
		heartbeatInterval: DefaultHeartbeatInterval,
		reconnectInterval: DefaultReconnectInterval,
		maxReconnects:     0, // 0 means infinite reconnects
		callbacks:         callbacks,
		processedMsgIds:   processedMsgIds,
	}
}

// Start connects to the WebSocket server and starts message handling.
func (c *WebSocketClient) Start(ctx context.Context) error {
	c.runningMu.Lock()
	if c.running {
		c.runningMu.Unlock()
		return nil
	}
	c.running = true
	c.runningMu.Unlock()

	c.ctx, c.cancel = context.WithCancel(ctx)
	c.reconnectAttempts = 0

	return c.connect()
}

// Stop disconnects the WebSocket client.
func (c *WebSocketClient) Stop() error {
	c.runningMu.Lock()
	c.running = false
	c.runningMu.Unlock()

	if c.cancel != nil {
		c.cancel()
	}

	c.clearTimers()
	c.closeConn(nil)

	return nil
}

// IsRunning returns whether the client is currently running.
func (c *WebSocketClient) IsRunning() bool {
	c.runningMu.Lock()
	defer c.runningMu.Unlock()
	return c.running
}

// connect establishes a WebSocket connection.
func (c *WebSocketClient) connect() error {
	connectionURL := c.buildConnectionURL()

	dialer := websocket.Dialer{
		HandshakeTimeout: 15 * time.Second,
	}

	conn, _, err := dialer.DialContext(c.ctx, connectionURL, nil)
	if err != nil {
		logger.ErrorCF("qclaw", "WebSocket dial failed", map[string]any{
			"error": err.Error(),
		})
		return channels.ClassifyNetError(err)
	}

	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()

	logger.InfoCF("qclaw", "WebSocket connected", map[string]any{
		"url": c.url,
	})

	// Start message handling
	go c.readLoop()
	go c.heartbeatLoop()
	go c.wakeupDetectionLoop()
	go c.msgIdCleanupLoop()

	return nil
}

// buildConnectionURL constructs the WebSocket URL with authentication parameters.
func (c *WebSocketClient) buildConnectionURL() string {
	params := fmt.Sprintf("?guid=%s&user_id=%s", c.guid, c.userID)
	if c.token != "" {
		params += fmt.Sprintf("&token=%s", c.token)
	}
	return c.url + params
}

// readLoop handles incoming WebSocket messages.
func (c *WebSocketClient) readLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		c.connMu.RLock()
		conn := c.conn
		c.connMu.RUnlock()

		if conn == nil {
			return
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			if c.IsRunning() {
				logger.WarnCF("qclaw", "WebSocket read error", map[string]any{
					"error": err.Error(),
				})
				c.handleDisconnect()
			}
			return
		}

		c.handleRawMessage(data)
	}
}

// handleRawMessage parses and dispatches an incoming WebSocket message.
func (c *WebSocketClient) handleRawMessage(data []byte) {
	var envelope AGPEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		logger.WarnCF("qclaw", "Failed to parse AGP envelope", map[string]any{
			"error": err.Error(),
			"body":  string(data),
		})
		return
	}

	// Deduplicate by msg_id
	if envelope.MsgID != "" {
		if c.processedMsgIds.Contains(envelope.MsgID) {
			logger.DebugCF("qclaw", "Skipping duplicate message", map[string]any{
				"msg_id": envelope.MsgID,
			})
			return
		}
		c.processedMsgIds.Add(envelope.MsgID, struct{}{})
	}

	logger.DebugCF("qclaw", "Received AGP message", map[string]any{
		"msg_id": envelope.MsgID,
		"method": envelope.Method,
	})

	switch envelope.Method {
	case AGPMethodPrompt:
		var payload PromptPayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			logger.ErrorCF("qclaw", "Failed to parse prompt payload", map[string]any{
				"error": err.Error(),
			})
			return
		}
		c.callbacks.OnPrompt(payload)

	case AGPMethodCancel:
		var payload CancelPayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			logger.ErrorCF("qclaw", "Failed to parse cancel payload", map[string]any{
				"error": err.Error(),
			})
			return
		}
		c.callbacks.OnCancel(payload)

	default:
		logger.WarnCF("qclaw", "Unknown AGP method", map[string]any{
			"method": envelope.Method,
		})
	}
}

// heartbeatLoop sends periodic ping messages.
func (c *WebSocketClient) heartbeatLoop() {
	ticker := time.NewTicker(c.heartbeatInterval)
	defer ticker.Stop()

	lastPong := time.Now()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			// Check for pong timeout (2x heartbeat interval)
			if time.Since(lastPong) > 2*c.heartbeatInterval {
				logger.WarnC("qclaw", "Pong timeout, reconnecting")
				c.handleDisconnect()
				return
			}

			c.connMu.RLock()
			conn := c.conn
			c.connMu.RUnlock()

			if conn == nil {
				return
			}

			c.writeMu.Lock()
			err := conn.WriteMessage(websocket.PingMessage, nil)
			c.writeMu.Unlock()

			if err != nil {
				logger.WarnCF("qclaw", "Ping failed", map[string]any{
					"error": err.Error(),
				})
				c.handleDisconnect()
				return
			}

			// Update pong time when we receive pong
			conn.SetPongHandler(func(appData string) error {
				lastPong = time.Now()
				return nil
			})
		}
	}
}

// wakeupDetectionLoop detects system sleep/wake events.
func (c *WebSocketClient) wakeupDetectionLoop() {
	c.wakeupTicker = time.NewTicker(DefaultWakeupCheckInterval)
	c.lastTickTime = time.Now()

	for {
		select {
		case <-c.ctx.Done():
			return
		case now := <-c.wakeupTicker.C:
			elapsed := now.Sub(c.lastTickTime)
			c.lastTickTime = now

			// If more than 15 seconds passed between ticks, system likely woke up
			if elapsed > DefaultWakeupThreshold {
				logger.InfoC("qclaw", "System wakeup detected, reconnecting")
				c.reconnectAttempts = 0
				c.handleDisconnect()
			}
		}
	}
}

// msgIdCleanupLoop periodically cleans up old message IDs.
func (c *WebSocketClient) msgIdCleanupLoop() {
	c.msgIdCleanup = time.NewTicker(MsgIdCleanupInterval)
	defer c.msgIdCleanup.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.msgIdCleanup.C:
			// LRU cache automatically evicts old entries
		}
	}
}

// handleDisconnect handles a WebSocket disconnection.
func (c *WebSocketClient) handleDisconnect() {
	c.closeConn(nil)

	if !c.IsRunning() {
		return
	}

	c.scheduleReconnect()
}

// scheduleReconnect schedules a reconnection attempt.
func (c *WebSocketClient) scheduleReconnect() {
	c.reconnectAttempts++

	if c.maxReconnects > 0 && c.reconnectAttempts > c.maxReconnects {
		logger.ErrorCF("qclaw", "Max reconnect attempts reached", map[string]any{
			"attempts": c.maxReconnects,
		})
		return
	}

	delay := c.calculateReconnectDelay()
	logger.InfoCF("qclaw", "Scheduling reconnect", map[string]any{
		"attempt": c.reconnectAttempts,
		"delay":   delay.String(),
	})

	c.reconnectTimer = time.AfterFunc(delay, func() {
		if !c.IsRunning() {
			return
		}
		if err := c.connect(); err != nil {
			logger.ErrorCF("qclaw", "Reconnect failed", map[string]any{
				"error": err.Error(),
			})
			c.scheduleReconnect()
		} else {
			c.reconnectAttempts = 0
		}
	})
}

// calculateReconnectDelay calculates the reconnect delay with exponential backoff.
func (c *WebSocketClient) calculateReconnectDelay() time.Duration {
	// base * 1.5^(n-1), max 25 seconds
	delay := float64(c.reconnectInterval)
	for i := 1; i < c.reconnectAttempts; i++ {
		delay *= 1.5
		if delay > float64(DefaultReconnectMaxDelay) {
			delay = float64(DefaultReconnectMaxDelay)
			break
		}
	}
	return time.Duration(delay)
}

// closeConn closes the WebSocket connection.
func (c *WebSocketClient) closeConn(expected *websocket.Conn) {
	c.connMu.Lock()
	conn := c.conn
	if expected != nil && conn != expected {
		c.connMu.Unlock()
		return
	}
	c.conn = nil
	c.connMu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}
}

// clearTimers stops all timers.
func (c *WebSocketClient) clearTimers() {
	if c.reconnectTimer != nil {
		c.reconnectTimer.Stop()
	}
	if c.wakeupTicker != nil {
		c.wakeupTicker.Stop()
	}
	if c.msgIdCleanup != nil {
		c.msgIdCleanup.Stop()
	}
}

// SendMessageChunk sends a message_chunk update.
func (c *WebSocketClient) SendMessageChunk(sessionID, promptID, text string) error {
	return c.sendUpdate(UpdatePayload{
		SessionID:  sessionID,
		PromptID:   promptID,
		UpdateType: UpdateTypeMessageChunk,
		Content:    &ContentBlock{Type: "text", Text: text},
	})
}

// SendToolCall sends a tool_call update.
func (c *WebSocketClient) SendToolCall(sessionID, promptID string, toolCall ToolCall) error {
	return c.sendUpdate(UpdatePayload{
		SessionID:  sessionID,
		PromptID:   promptID,
		UpdateType: UpdateTypeToolCall,
		ToolCall:   &toolCall,
	})
}

// SendToolCallUpdate sends a tool_call_update update.
func (c *WebSocketClient) SendToolCallUpdate(sessionID, promptID string, toolCall ToolCall) error {
	return c.sendUpdate(UpdatePayload{
		SessionID:  sessionID,
		PromptID:   promptID,
		UpdateType: UpdateTypeToolCallUpdate,
		ToolCall:   &toolCall,
	})
}

// SendPromptResponse sends the final response.
func (c *WebSocketClient) SendPromptResponse(sessionID, promptID, stopReason string, content []ContentBlock) error {
	envelope := AGPEnvelope{
		MsgID:  generateMsgID(),
		GUID:   c.guid,
		UserID: c.userID,
		Method: AGPMethodPromptResponse,
	}

	payload := PromptResponsePayload{
		SessionID:  sessionID,
		PromptID:   promptID,
		StopReason: stopReason,
		Content:    content,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal prompt response: %w", err)
	}
	envelope.Payload = payloadBytes

	return c.sendEnvelope(envelope)
}

// sendUpdate sends a session.update message.
func (c *WebSocketClient) sendUpdate(payload UpdatePayload) error {
	envelope := AGPEnvelope{
		MsgID:  generateMsgID(),
		GUID:   c.guid,
		UserID: c.userID,
		Method: AGPMethodUpdate,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal update payload: %w", err)
	}
	envelope.Payload = payloadBytes

	return c.sendEnvelope(envelope)
}

// sendEnvelope sends an AGP envelope over WebSocket.
func (c *WebSocketClient) sendEnvelope(envelope AGPEnvelope) error {
	c.connMu.RLock()
	conn := c.conn
	c.connMu.RUnlock()

	if conn == nil {
		return fmt.Errorf("websocket not connected")
	}

	data, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal envelope: %w", err)
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if err := conn.SetWriteDeadline(time.Now().Add(DefaultSendTimeout)); err != nil {
		return fmt.Errorf("set write deadline: %w", err)
	}

	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return channels.ClassifyNetError(err)
	}

	return nil
}

// generateMsgID generates a unique message ID.
func generateMsgID() string {
	return uuid.NewString()
}