package qclaw

import (
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestNewQClawChannel(t *testing.T) {
	tests := []struct {
		name    string
		config  config.QClawConfig
		wantErr bool
	}{
		{
			name: "valid config with token",
			config: config.QClawConfig{
				Token:        "test-token",
				UserID:       "test-user",
				GUID:         "test-guid",
				WebSocketURL: "wss://test.example.com/ws",
			},
			wantErr: false,
		},
		{
			name: "missing token",
			config: config.QClawConfig{
				UserID: "test-user",
			},
			wantErr: true,
		},
		{
			name: "default values",
			config: config.QClawConfig{
				Token: "test-token",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewQClawChannel(tt.config, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewQClawChannel() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGenerateDeviceGUID(t *testing.T) {
	guid1 := GenerateDeviceGUID()
	guid2 := GenerateDeviceGUID()

	// Should be different
	if guid1 == guid2 {
		t.Error("GenerateDeviceGUID should produce unique GUIDs")
	}

	// Should be 32 characters (MD5 hex)
	if len(guid1) != 32 {
		t.Errorf("GenerateDeviceGUID() = %v, want length 32", len(guid1))
	}
}

func TestMessageDeduplicator(t *testing.T) {
	d := NewMessageDeduplicator(100, 10*time.Minute)

	// First message should be new
	if !d.MarkMessageProcessed("msg1") {
		t.Error("First message should be marked as processed")
	}

	// Duplicate should be rejected
	if d.MarkMessageProcessed("msg1") {
		t.Error("Duplicate message should not be marked as processed")
	}

	// New message should be accepted
	if !d.MarkMessageProcessed("msg2") {
		t.Error("New message should be marked as processed")
	}
}

func TestBuildOAuthURL(t *testing.T) {
	tests := []struct {
		name      string
		state     string
		appID     string
		redirect  string
		isTest    bool
		wantContain string
	}{
		{
			name:      "production",
			state:     "test-state",
			isTest:    false,
			wantContain: "wx9d11056dd75b7240",
		},
		{
			name:      "test environment",
			state:     "test-state",
			isTest:    true,
			wantContain: "wx3dd49afb7e2cf957",
		},
		{
			name:      "custom appID",
			state:     "test-state",
			appID:     "custom-app-id",
			isTest:    false,
			wantContain: "custom-app-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := BuildOAuthURL(tt.state, tt.appID, tt.redirect, tt.isTest)
			if !contains(url, tt.wantContain) {
				t.Errorf("BuildOAuthURL() = %v, want to contain %v", url, tt.wantContain)
			}
		})
	}
}

func TestAGPEnvelope(t *testing.T) {
	envelope := AGPEnvelope{
		MsgID:  "test-msg-id",
		GUID:   "test-guid",
		UserID: "test-user",
		Method: AGPMethodPrompt,
		Payload: []byte(`{"session_id":"sess-1","prompt_id":"prompt-1","agent_app":"picoclaw","content":[{"type":"text","text":"hello"}]}`),
	}

	if envelope.Method != AGPMethodPrompt {
		t.Errorf("Expected method %s, got %s", AGPMethodPrompt, envelope.Method)
	}
}

func TestPromptPayloadParsing(t *testing.T) {
	payload := PromptPayload{
		SessionID: "test-session",
		PromptID:  "test-prompt",
		AgentApp:  "picoclaw",
		Content: []ContentBlock{
			{Type: "text", Text: "Hello"},
		},
	}

	if payload.SessionID != "test-session" {
		t.Errorf("Expected session_id test-session, got %s", payload.SessionID)
	}

	if len(payload.Content) != 1 {
		t.Errorf("Expected 1 content block, got %d", len(payload.Content))
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input   string
		maxLen  int
		wantLen int
	}{
		{"short", 10, 5},
		{"exactly 10 chars", 10, 10},
		{"this is a longer string", 10, 13}, // includes "..."
		{"", 5, 0},
	}

	for _, tt := range tests {
		result := truncate(tt.input, tt.maxLen)
		if len(result) > tt.maxLen+3 { // Allow for "..."
			t.Errorf("truncate(%q, %d) = %q (len=%d), expected at most %d",
				tt.input, tt.maxLen, result, len(result), tt.maxLen+3)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
