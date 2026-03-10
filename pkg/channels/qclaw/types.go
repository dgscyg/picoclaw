// Package qclaw implements the QClaw channel for WeChat service account integration.
// It uses the AGP (Agent Gateway Protocol) over WebSocket for bidirectional communication.
package qclaw

import (
	"encoding/json"
	"sync"
	"time"
)

// AGP method constants
const (
	AGPMethodPrompt         = "session.prompt"
	AGPMethodCancel         = "session.cancel"
	AGPMethodUpdate         = "session.update"
	AGPMethodPromptResponse = "session.promptResponse"
)

// Update type constants for session.update
const (
	UpdateTypeMessageChunk  = "message_chunk"
	UpdateTypeToolCall      = "tool_call"
	UpdateTypeToolCallUpdate = "tool_call_update"
)

// Stop reason constants
const (
	StopReasonEndTurn   = "end_turn"
	StopReasonCancelled = "cancelled"
	StopReasonRefusal   = "refusal"
	StopReasonError     = "error"
)

// Tool call kind constants
const (
	ToolCallKindRead    = "read"
	ToolCallKindEdit    = "edit"
	ToolCallKindDelete  = "delete"
	ToolCallKindExecute = "execute"
	ToolCallKindSearch  = "search"
	ToolCallKindFetch   = "fetch"
	ToolCallKindThink   = "think"
	ToolCallKindOther   = "other"
)

// Tool call status constants
const (
	ToolCallStatusPending    = "pending"
	ToolCallStatusInProgress = "in_progress"
	ToolCallStatusCompleted  = "completed"
	ToolCallStatusFailed     = "failed"
)

// QClaw environment constants
const (
	EnvironmentProd = "production"
	EnvironmentTest = "test"
)

// QClaw API command IDs
const (
	CmdGetWxLoginState     = 4050
	CmdWxLogin             = 4026
	CmdCreateApiKey        = 4055
	CmdCheckInviteCode     = 4056
	CmdSubmitInviteCode    = 4057
	CmdRefreshChannelToken = 4058
	CmdGenerateContactLink = 4018
	CmdQueryDeviceByGuid   = 4019
	CmdDisconnectDevice    = 4020
)

// JPRX Gateway URLs
const (
	JPRXGatewayProd = "https://jprx.m.qq.com/"
	JPRXGatewayTest = "https://jprx.sparta.html5.qq.com/"
)

// Default WebSocket URL
const (
	DefaultWebSocketURL = "wss://mmgrcalltoken.3g.qq.com/agentwss"
)

// Timing constants
const (
	DefaultHeartbeatInterval  = 20 * time.Second
	DefaultReconnectInterval  = 3 * time.Second
	DefaultReconnectMaxDelay  = 25 * time.Second
	DefaultAuthTimeout        = 10 * time.Second
	DefaultSendTimeout        = 15 * time.Second
	DefaultWakeupCheckInterval = 5 * time.Second
	DefaultWakeupThreshold    = 15 * time.Second
	MaxProcessedMessages      = 1000
	MsgIdCleanupInterval      = 5 * time.Minute
)

// AGPEnvelope represents the unified message envelope for AGP protocol.
type AGPEnvelope struct {
	MsgID   string          `json:"msg_id"`
	GUID    string          `json:"guid"`
	UserID  string          `json:"user_id"`
	Method  string          `json:"method"`
	Payload json.RawMessage `json:"payload"`
}

// PromptPayload represents session.prompt payload.
type PromptPayload struct {
	SessionID string         `json:"session_id"`
	PromptID  string         `json:"prompt_id"`
	AgentApp  string         `json:"agent_app"`
	Content   []ContentBlock `json:"content"`
}

// CancelPayload represents session.cancel payload.
type CancelPayload struct {
	SessionID string `json:"session_id"`
	PromptID  string `json:"prompt_id"`
	AgentApp  string `json:"agent_app"`
}

// UpdatePayload represents session.update payload.
type UpdatePayload struct {
	SessionID  string        `json:"session_id"`
	PromptID   string        `json:"prompt_id"`
	UpdateType string        `json:"update_type"`
	Content    *ContentBlock `json:"content,omitempty"`
	ToolCall   *ToolCall     `json:"tool_call,omitempty"`
}

// PromptResponsePayload represents session.promptResponse payload.
type PromptResponsePayload struct {
	SessionID  string         `json:"session_id"`
	PromptID   string         `json:"prompt_id"`
	StopReason string         `json:"stop_reason"`
	Content    []ContentBlock `json:"content,omitempty"`
	Error      string         `json:"error,omitempty"`
}

// ContentBlock represents a content block in AGP messages.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// ToolCall represents a tool call in AGP messages.
type ToolCall struct {
	ToolCallID string         `json:"tool_call_id"`
	Title      string         `json:"title,omitempty"`
	Kind       string         `json:"kind,omitempty"`
	Status     string         `json:"status"`
	Content    []ContentBlock `json:"content,omitempty"`
	Locations  []Location     `json:"locations,omitempty"`
}

// Location represents a file path location.
type Location struct {
	Path string `json:"path"`
}

// AuthState represents the persisted authentication state.
type AuthState struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	GUID         string `json:"guid"`
	UserID       string `json:"user_id"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
	ChannelToken string `json:"channel_token,omitempty"`
}

// QClawAPIResponse represents a generic QClaw API response.
type QClawAPIResponse struct {
	CommonCode int             `json:"commonCode"`
	Data       json.RawMessage `json:"data,omitempty"`
	Message    string          `json:"message,omitempty"`
}

// WxLoginStateResponse represents getWxLoginState response.
type WxLoginStateResponse struct {
	State   string `json:"state"`
	LoginKey string `json:"login_key"`
}

// WxLoginResponse represents wxLogin response.
type WxLoginResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	UserID       string `json:"user_id"`
	ExpiresAt    int64  `json:"expires_at"`
	ChannelToken string `json:"channel_token"`
}

// PromptTask tracks an active prompt for response correlation.
type PromptTask struct {
	SessionID   string
	PromptID    string
	ChatID      string
	MessageID   string
	CreatedAt   time.Time
	Accumulated string
	Finalized   bool
	mu          sync.Mutex
	timer       *time.Timer
	sequence    int
}
