package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestMuninnValidationDryRunConfig_ValidateConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Memory.Provider = config.MemoryProviderMuninnDB
	cfg.Memory.MuninnDB = &config.MuninnDBConfig{
		MCPEndpoint:  "http://127.0.0.1:8750",
		RESTEndpoint: "http://127.0.0.1:8475",
		Vault:        MuninnValidationVault,
	}
	cfg.Channels.Claweb.Enabled = true
	cfg.Channels.Claweb.ListenHost = "127.0.0.1"
	cfg.Channels.Claweb.ListenPort = 18999
	cfg.Channels.Claweb.AuthTokenFile = "tmp/claweb.token"

	if err := DefaultMuninnValidationDryRunConfig().ValidateConfig(cfg); err != nil {
		t.Fatalf("ValidateConfig() error = %v", err)
	}
}

func TestMuninnValidationDryRunConfig_RejectsUnexpectedVault(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Memory.Provider = config.MemoryProviderMuninnDB
	cfg.Memory.MuninnDB = &config.MuninnDBConfig{
		MCPEndpoint:  "http://127.0.0.1:8750",
		RESTEndpoint: "http://127.0.0.1:8475",
		Vault:        "other-vault",
	}
	cfg.Channels.Claweb.Enabled = true
	cfg.Channels.Claweb.ListenHost = "127.0.0.1"
	cfg.Channels.Claweb.ListenPort = 18999
	cfg.Channels.Claweb.AuthTokenFile = "tmp/claweb.token"

	err := DefaultMuninnValidationDryRunConfig().ValidateConfig(cfg)
	if err == nil || !strings.Contains(err.Error(), MuninnValidationVault) {
		t.Fatalf("ValidateConfig() error = %v, want mention of validation vault", err)
	}
}

func TestMuninnProxyLogFields_RedactsSensitiveContent(t *testing.T) {
	fields := muninnProxyLogFields(
		MuninnProxyLogEventAutoRecallHit,
		MuninnProxyPlan{
			Operation:  MuninnProxyOperationRecall,
			RoundID:    "round-1",
			SessionKey: "session-1",
			Channel:    MuninnValidationChannel,
			Vault:      MuninnValidationVault,
			Query:      "remember api_key=secret-token-value and password=123456",
			MaxItems:   3,
			Timeout:    5 * time.Second,
			DryRun:     true,
		},
		&MuninnProxyResult{
			Operation:      MuninnProxyOperationRecall,
			Status:         MuninnProxyStatusHit,
			Vault:          MuninnValidationVault,
			Reason:         "hit",
			Duration:       250 * time.Millisecond,
			CandidateCount: 4,
			InjectedCount:  2,
			Items: []MuninnProxyMemoryItem{{
				Content: "never log this raw memory body",
				Why:     "because it is sensitive",
			}},
		},
		timeoutError("dial tcp 127.0.0.1:8475: i/o timeout api_key=secret-token-value"),
		map[string]any{"custom": "value"},
	)

	if got := fields["event"]; got != string(MuninnProxyLogEventAutoRecallHit) {
		t.Fatalf("event = %v", got)
	}
	queryPreview := fields["query_preview"].(string)
	if strings.Contains(queryPreview, "secret-token-value") || strings.Contains(queryPreview, "123456") {
		t.Fatalf("query_preview leaked secret: %q", queryPreview)
	}
	if got := fields["error"].(string); strings.Contains(got, "secret-token-value") {
		t.Fatalf("error leaked secret: %q", got)
	}
	if _, exists := fields["items"]; exists {
		t.Fatalf("fields should not include raw items: %#v", fields)
	}
	if got := fields["duration_ms"]; got != int64(250) {
		t.Fatalf("duration_ms = %v, want 250", got)
	}
	if got := fields["custom"]; got != "value" {
		t.Fatalf("custom field = %v", got)
	}
}

type timeoutError string

func (e timeoutError) Error() string { return string(e) }
