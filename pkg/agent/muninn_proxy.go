package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
)

const (
	MuninnAutoRecallBlockTitle = "Relevant Memory (Muninn Auto-Recall)"
	MuninnValidationVault      = config.DefaultMemoryVault
	MuninnValidationChannel    = "claweb"
)

type MuninnProxyOperation string

const (
	MuninnProxyOperationRecall        MuninnProxyOperation = "recall"
	MuninnProxyOperationCapture       MuninnProxyOperation = "capture"
	MuninnProxyOperationConsolidation MuninnProxyOperation = "consolidation"
)

type MuninnProxyStatus string

const (
	MuninnProxyStatusPending    MuninnProxyStatus = "pending"
	MuninnProxyStatusHit        MuninnProxyStatus = "hit"
	MuninnProxyStatusMiss       MuninnProxyStatus = "miss"
	MuninnProxyStatusSkipped    MuninnProxyStatus = "skipped"
	MuninnProxyStatusTimeout    MuninnProxyStatus = "timeout"
	MuninnProxyStatusError      MuninnProxyStatus = "error"
	MuninnProxyStatusWritten    MuninnProxyStatus = "written"
	MuninnProxyStatusDropped    MuninnProxyStatus = "dropped"
	MuninnProxyStatusCacheHit   MuninnProxyStatus = "cache_hit"
	MuninnProxyStatusCacheMiss  MuninnProxyStatus = "cache_miss"
	MuninnProxyStatusConflicted MuninnProxyStatus = "conflicted"
)

// MuninnProxyPlan captures the future pre/post-turn inputs used by transparent recall,
// capture, and consolidation flows without wiring the behavior in yet.
type MuninnProxyPlan struct {
	Operation  MuninnProxyOperation
	RoundID    string
	SessionKey string
	Channel    string
	Vault      string
	Query      string
	MaxItems   int
	Timeout    time.Duration
	DryRun     bool
}

func (p MuninnProxyPlan) Normalized() MuninnProxyPlan {
	p.RoundID = strings.TrimSpace(p.RoundID)
	p.SessionKey = strings.TrimSpace(p.SessionKey)
	p.Channel = strings.TrimSpace(p.Channel)
	p.Vault = strings.TrimSpace(p.Vault)
	p.Query = strings.TrimSpace(p.Query)
	if p.MaxItems < 0 {
		p.MaxItems = 0
	}
	if p.Timeout < 0 {
		p.Timeout = 0
	}
	return p
}

// MuninnProxyPromptBlock represents a bounded dynamic prompt block that can be
// injected without persisting it into conversation history.
type MuninnProxyPromptBlock struct {
	Title   string
	Body    string
	Persist bool
}

func (b MuninnProxyPromptBlock) Enabled() bool {
	return strings.TrimSpace(b.Body) != ""
}

// MuninnProxyMemoryItem is the shared normalized shape for recalled or newly
// captured memory items across upcoming leaf features.
type MuninnProxyMemoryItem struct {
	ID      string
	Concept string
	Content string
	Why     string
	Score   float64
	Tags    []string
}

// MuninnProxyResult is the reusable outcome shape for transparent recall,
// capture, and consolidation planning stages.
type MuninnProxyResult struct {
	Operation      MuninnProxyOperation
	Status         MuninnProxyStatus
	Vault          string
	Reason         string
	Duration       time.Duration
	CandidateCount int
	InjectedCount  int
	Items          []MuninnProxyMemoryItem
	PromptBlock    MuninnProxyPromptBlock
}

// MuninnValidationDryRunConfig describes the claweb validation shape required by
// this mission without touching runtime behavior.
type MuninnValidationDryRunConfig struct {
	Vault                   string
	Channel                 string
	RequireSeparateRESTPath bool
	RequireAuthToken        bool
}

func DefaultMuninnValidationDryRunConfig() MuninnValidationDryRunConfig {
	return MuninnValidationDryRunConfig{
		Vault:                   MuninnValidationVault,
		Channel:                 MuninnValidationChannel,
		RequireSeparateRESTPath: true,
		RequireAuthToken:        true,
	}
}

func (v MuninnValidationDryRunConfig) ValidateConfig(cfg *config.Config) error {
	if cfg == nil {
		return fmt.Errorf("muninn validation config is required")
	}
	if strings.TrimSpace(cfg.Memory.Provider) != config.MemoryProviderMuninnDB {
		return fmt.Errorf("muninn validation requires memory.provider=%q", config.MemoryProviderMuninnDB)
	}
	if cfg.Memory.MuninnDB == nil {
		return fmt.Errorf("muninn validation requires memory.muninndb configuration")
	}
	muninnCfg := cfg.Memory.MuninnDB
	wantVault := strings.TrimSpace(v.Vault)
	if wantVault == "" {
		wantVault = MuninnValidationVault
	}
	if got := strings.TrimSpace(muninnCfg.Vault); got != wantVault {
		return fmt.Errorf("muninn validation requires vault %q, got %q", wantVault, got)
	}
	if muninnCfg.ResolvedMCPEndpoint() == "" {
		return fmt.Errorf("muninn validation requires memory.muninndb.mcp_endpoint")
	}
	if v.RequireSeparateRESTPath {
		if muninnCfg.ResolvedRESTEndpoint() == "" {
			return fmt.Errorf("muninn validation requires memory.muninndb.rest_endpoint")
		}
		if !muninnCfg.HasSeparateRESTEndpoint() {
			return fmt.Errorf("muninn validation requires separate MCP and REST endpoints")
		}
	}

	channel := strings.TrimSpace(v.Channel)
	if channel == "" {
		channel = MuninnValidationChannel
	}
	switch channel {
	case MuninnValidationChannel:
		if !cfg.Channels.Claweb.Enabled {
			return fmt.Errorf("muninn validation requires claweb to be enabled")
		}
		if strings.TrimSpace(cfg.Channels.Claweb.ListenHost) == "" {
			return fmt.Errorf("muninn validation requires claweb listen_host")
		}
		if cfg.Channels.Claweb.ListenPort <= 0 {
			return fmt.Errorf("muninn validation requires claweb listen_port")
		}
		if v.RequireAuthToken &&
			strings.TrimSpace(cfg.Channels.Claweb.AuthToken) == "" &&
			strings.TrimSpace(cfg.Channels.Claweb.AuthTokenFile) == "" {
			return fmt.Errorf("muninn validation requires claweb auth token or auth token file")
		}
	default:
		return fmt.Errorf("unsupported muninn validation channel %q", channel)
	}

	return nil
}
