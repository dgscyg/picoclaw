package qclaw

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/identity"
	"github.com/sipeed/picoclaw/pkg/logger"
)

const (
	channelName = "qclaw"

	// ResponseIdleTimeout is the duration after which a prompt response is finalized
	// if no new content is received.
	ResponseIdleTimeout = 250 * time.Millisecond

	// PromptTaskMaxAge is the maximum age of a prompt task before it's cleaned up.
	PromptTaskMaxAge = 10 * time.Minute
)

// QClawChannel implements the Channel interface for QClaw WeChat integration.
type QClawChannel struct {
	*channels.BaseChannel
	config    config.QClawConfig
	api       *QClawAPI
	authMgr   *AuthStateManager
	wsClient  *WebSocketClient

	ctx    context.Context
	cancel context.CancelFunc

	// Prompt tracking for response correlation
	promptMu sync.Mutex
	prompts  map[string]*PromptTask // prompt_id -> task

	// Processed message deduplication (in addition to WebSocket-level)
	processedMsgs *MessageDeduplicator
}

// MessageDeduplicator provides message deduplication.
type MessageDeduplicator struct {
	mu      sync.Mutex
	msgIDs  map[string]time.Time
	maxSize int
	ttl     time.Duration
}

// NewMessageDeduplicator creates a new message deduplicator.
func NewMessageDeduplicator(maxSize int, ttl time.Duration) *MessageDeduplicator {
	return &MessageDeduplicator{
		msgIDs:  make(map[string]time.Time),
		maxSize: maxSize,
		ttl:     ttl,
	}
}

// MarkMessageProcessed marks a message as processed and returns whether it was newly added.
func (d *MessageDeduplicator) MarkMessageProcessed(msgID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Clean up old entries
	now := time.Now()
	for id, t := range d.msgIDs {
		if now.Sub(t) > d.ttl {
			delete(d.msgIDs, id)
		}
	}

	// Check if over limit
	if len(d.msgIDs) >= d.maxSize {
		// Remove oldest entries
		for id := range d.msgIDs {
			delete(d.msgIDs, id)
			if len(d.msgIDs) < d.maxSize/2 {
				break
			}
		}
	}

	if _, exists := d.msgIDs[msgID]; exists {
		return false
	}
	d.msgIDs[msgID] = now
	return true
}

// NewQClawChannel creates a new QClaw channel instance.
func NewQClawChannel(cfg config.QClawConfig, messageBus *bus.MessageBus) (*QClawChannel, error) {
	// Validate configuration
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, fmt.Errorf("qclaw token is required")
	}

	// Set defaults
	if cfg.WebSocketURL == "" {
		cfg.WebSocketURL = DefaultWebSocketURL
	}
	if cfg.GUID == "" {
		cfg.GUID = GenerateDeviceGUID()
	}
	if cfg.HeartbeatInterval == 0 {
		cfg.HeartbeatInterval = int(DefaultHeartbeatInterval / time.Second)
	}
	if cfg.ReconnectInterval == 0 {
		cfg.ReconnectInterval = int(DefaultReconnectInterval / time.Second)
	}

	base := channels.NewBaseChannel(
		channelName,
		cfg,
		messageBus,
		cfg.AllowFrom,
		channels.WithGroupTrigger(cfg.GroupTrigger),
		channels.WithReasoningChannelID(cfg.ReasoningChannelID),
	)

	return &QClawChannel{
		BaseChannel:   base,
		config:        cfg,
		api:           NewQClawAPI(cfg.Environment),
		authMgr:       NewAuthStateManager(cfg.AuthStatePath),
		prompts:       make(map[string]*PromptTask),
		processedMsgs: NewMessageDeduplicator(MaxProcessedMessages, 10*time.Minute),
	}, nil
}

// Start starts the QClaw channel.
func (c *QClawChannel) Start(ctx context.Context) error {
	if c.IsRunning() {
		return nil
	}

	logger.InfoC("qclaw", "Starting QClaw channel...")

	// Load auth state if needed
	if c.config.UserID == "" || c.config.GUID == "" {
		state, err := c.authMgr.LoadState()
		if err != nil {
			logger.WarnCF("qclaw", "Failed to load auth state", map[string]any{
				"error": err.Error(),
			})
		} else if state != nil {
			if c.config.UserID == "" {
				c.config.UserID = state.UserID
			}
			if c.config.GUID == "" {
				c.config.GUID = state.GUID
			}
		}
	}

	c.ctx, c.cancel = context.WithCancel(ctx)

	// Create and start WebSocket client
	c.wsClient = NewWebSocketClient(
		c.config.WebSocketURL,
		c.config.Token,
		c.config.GUID,
		c.config.UserID,
		c, // QClawChannel implements WebSocketClientCallbacks
	)
	c.wsClient.heartbeatInterval = time.Duration(c.config.HeartbeatInterval) * time.Second
	c.wsClient.reconnectInterval = time.Duration(c.config.ReconnectInterval) * time.Second
	c.wsClient.maxReconnects = c.config.MaxReconnects

	if err := c.wsClient.Start(c.ctx); err != nil {
		c.cancel()
		return fmt.Errorf("start websocket client: %w", err)
	}

	c.SetRunning(true)
	logger.InfoC("qclaw", "QClaw channel started")

	return nil
}

// Stop stops the QClaw channel.
func (c *QClawChannel) Stop(ctx context.Context) error {
	if !c.IsRunning() {
		return nil
	}

	logger.InfoC("qclaw", "Stopping QClaw channel...")
	c.SetRunning(false)

	if c.wsClient != nil {
		c.wsClient.Stop()
	}

	if c.cancel != nil {
		c.cancel()
	}

	logger.InfoC("qclaw", "QClaw channel stopped")
	return nil
}

// Send sends an outbound message.
// QClaw is a reactive channel - it can only send responses to active prompts.
// The message content is sent as a session.update and a timer is started to
// automatically send session.promptResponse when the response stream ends.
func (c *QClawChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return channels.ErrNotRunning
	}

	// Find active prompt task for this chat
	task := c.getActivePromptTask(msg.ChatID, msg.ReplyTo)
	if task == nil {
		// No active prompt, cannot send (QClaw is reactive only)
		logger.WarnCF("qclaw", "No active prompt task for chat", map[string]any{
			"chat_id": msg.ChatID,
		})
		return fmt.Errorf("no active prompt for chat %s", msg.ChatID)
	}

	task.mu.Lock()
	if task.Finalized {
		task.mu.Unlock()
		c.removePromptTask(task)
		return nil
	}

	// Stop any existing timer
	if task.timer != nil {
		task.timer.Stop()
		task.timer = nil
	}

	// Accumulate content
	task.Accumulated += msg.Content
	task.sequence++
	seq := task.sequence
	task.mu.Unlock()

	// Send as session.update
	if err := c.wsClient.SendMessageChunk(task.SessionID, task.PromptID, msg.Content); err != nil {
		return fmt.Errorf("send message chunk: %w", err)
	}

	// Schedule auto-finalization after idle timeout
	task.mu.Lock()
	task.timer = time.AfterFunc(ResponseIdleTimeout, func() {
		c.finalizePromptResponse(task, seq)
	})
	task.mu.Unlock()

	return nil
}

// OnPrompt handles session.prompt from WebSocket.
func (c *QClawChannel) OnPrompt(payload PromptPayload) {
	// Deduplicate
	if !c.processedMsgs.MarkMessageProcessed(payload.PromptID) {
		logger.DebugCF("qclaw", "Skipping duplicate prompt", map[string]any{
			"prompt_id": payload.PromptID,
		})
		return
	}

	// Extract content
	var content string
	for _, block := range payload.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}

	// Build sender info
	userID := c.config.UserID
	if userID == "" {
		userID = "qclaw_user"
	}
	sender := bus.SenderInfo{
		Platform:    channelName,
		PlatformID:  userID,
		CanonicalID: identity.BuildCanonicalID(channelName, userID),
		DisplayName: userID,
	}

	// Check allowlist
	if !c.IsAllowedSender(sender) {
		return
	}

	// Create prompt task for response correlation
	chatID := payload.SessionID
	task := &PromptTask{
		SessionID: payload.SessionID,
		PromptID:  payload.PromptID,
		ChatID:    chatID,
		MessageID: payload.PromptID,
		CreatedAt: time.Now(),
	}
	c.registerPromptTask(task)

	// Build metadata
	metadata := map[string]string{
		"session_id": payload.SessionID,
		"prompt_id":  payload.PromptID,
		"agent_app":  payload.AgentApp,
	}

	logger.InfoCF("qclaw", "Received prompt", map[string]any{
		"session_id": payload.SessionID,
		"prompt_id":  payload.PromptID,
		"preview":    truncate(content, 100),
	})

	// Handle message through base channel
	peer := bus.Peer{Kind: "direct", ID: chatID}
	c.HandleMessage(c.ctx, peer, payload.PromptID, userID, chatID, content, nil, metadata, sender)
}

// OnCancel handles session.cancel from WebSocket.
func (c *QClawChannel) OnCancel(payload CancelPayload) {
	logger.InfoCF("qclaw", "Received cancel", map[string]any{
		"session_id": payload.SessionID,
		"prompt_id":  payload.PromptID,
	})

	// Finalize and remove the prompt task
	c.finalizePromptTask(payload.PromptID)

	// Send cancelled response
	if c.wsClient != nil {
		_ = c.wsClient.SendPromptResponse(
			payload.SessionID,
			payload.PromptID,
			StopReasonCancelled,
			nil,
		)
	}
}

// FinalizePrompt finalizes a prompt task and sends the final response.
func (c *QClawChannel) FinalizePrompt(promptID, stopReason string, content []ContentBlock) error {
	task := c.getPromptTask(promptID)
	if task == nil {
		return fmt.Errorf("no prompt task for %s", promptID)
	}

	if c.wsClient == nil {
		return channels.ErrNotRunning
	}

	err := c.wsClient.SendPromptResponse(task.SessionID, promptID, stopReason, content)
	c.finalizePromptTask(promptID)

	return err
}

// registerPromptTask registers a new prompt task.
func (c *QClawChannel) registerPromptTask(task *PromptTask) {
	c.promptMu.Lock()
	defer c.promptMu.Unlock()

	// Clean up old tasks
	now := time.Now()
	for id, t := range c.prompts {
		if now.Sub(t.CreatedAt) > 10*time.Minute {
			delete(c.prompts, id)
		}
	}

	c.prompts[task.PromptID] = task
}

// getPromptTask retrieves a prompt task by ID.
func (c *QClawChannel) getPromptTask(promptID string) *PromptTask {
	c.promptMu.Lock()
	defer c.promptMu.Unlock()
	return c.prompts[promptID]
}

// getActivePromptTask gets the most recent active prompt for a chat.
func (c *QClawChannel) getActivePromptTask(chatID, replyTo string) *PromptTask {
	c.promptMu.Lock()
	defer c.promptMu.Unlock()

	// If replyTo is provided, look for specific prompt
	if replyTo != "" {
		if task, ok := c.prompts[replyTo]; ok && !task.Finalized {
			return task
		}
		return nil
	}

	// Find most recent active prompt for the chat
	var newest *PromptTask
	for _, task := range c.prompts {
		if task.ChatID == chatID && !task.Finalized {
			if newest == nil || task.CreatedAt.After(newest.CreatedAt) {
				newest = task
			}
		}
	}
	return newest
}

// finalizePromptTask marks a prompt task as finalized.
func (c *QClawChannel) finalizePromptTask(promptID string) {
	c.promptMu.Lock()
	defer c.promptMu.Unlock()

	if task, ok := c.prompts[promptID]; ok {
		task.mu.Lock()
		task.Finalized = true
		if task.timer != nil {
			task.timer.Stop()
			task.timer = nil
		}
		task.mu.Unlock()
		delete(c.prompts, promptID)
	}
}

// removePromptTask removes a prompt task from tracking.
func (c *QClawChannel) removePromptTask(task *PromptTask) {
	if task == nil {
		return
	}

	c.promptMu.Lock()
	defer c.promptMu.Unlock()

	// Clean up timer
	task.mu.Lock()
	if task.timer != nil {
		task.timer.Stop()
		task.timer = nil
	}
	task.mu.Unlock()

	// Remove from map if it's still there
	if t, ok := c.prompts[task.PromptID]; ok && t == task {
		delete(c.prompts, task.PromptID)
	}
}

// finalizePromptResponse sends the final promptResponse when the stream ends.
func (c *QClawChannel) finalizePromptResponse(task *PromptTask, seq int) {
	if task == nil {
		return
	}

	task.mu.Lock()
	if task.Finalized || seq != task.sequence {
		task.mu.Unlock()
		return
	}
	accumulated := task.Accumulated
	task.Finalized = true
	task.timer = nil
	task.mu.Unlock()

	c.removePromptTask(task)

	if c.wsClient == nil {
		return
	}

	// Build content blocks
	var content []ContentBlock
	if accumulated != "" {
		content = []ContentBlock{{Type: "text", Text: accumulated}}
	}

	if err := c.wsClient.SendPromptResponse(task.SessionID, task.PromptID, StopReasonEndTurn, content); err != nil {
		logger.WarnCF("qclaw", "Failed to send prompt response", map[string]any{
			"session_id": task.SessionID,
			"prompt_id":  task.PromptID,
			"error":      err.Error(),
		})
	}
}

// truncate truncates a string to maxLen runes.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}
