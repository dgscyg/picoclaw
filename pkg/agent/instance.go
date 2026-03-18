package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/media"
	"github.com/sipeed/picoclaw/pkg/memory"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/routing"
	"github.com/sipeed/picoclaw/pkg/session"
	"github.com/sipeed/picoclaw/pkg/tools"
)

type AgentInstance struct {
	ID                        string
	Name                      string
	Model                     string
	Fallbacks                 []string
	Workspace                 string
	MaxIterations             int
	MaxTokens                 int
	Temperature               float64
	ThinkingLevel             ThinkingLevel
	ContextWindow             int
	SummarizeMessageThreshold int
	SummarizeTokenPercent     int
	Provider                  providers.LLMProvider
	Sessions                  session.SessionStore
	ContextBuilder            *ContextBuilder
	Tools                     *tools.ToolRegistry
	Subagents                 *config.SubagentsConfig
	SkillsFilter              []string
	Candidates                []providers.FallbackCandidate
	Router                    *routing.Router
	LightCandidates           []providers.FallbackCandidate
}

func NewAgentInstance(agentCfg *config.AgentConfig, defaults *config.AgentDefaults, cfg *config.Config, provider providers.LLMProvider) *AgentInstance {
	workspace := resolveAgentWorkspace(agentCfg, defaults)
	os.MkdirAll(workspace, 0o755)

	model := resolveAgentModel(agentCfg, defaults)
	fallbacks := resolveAgentFallbacks(agentCfg, defaults)
	restrict := defaults.RestrictToWorkspace
	readRestrict := restrict && !defaults.AllowReadOutsideWorkspace

	// Compile path whitelist patterns from config.
	allowReadPaths := buildAllowReadPatterns(cfg)
	allowWritePaths := compilePatterns(cfg.Tools.AllowWritePaths)
	var muninnMemoryDenyPaths []*regexp.Regexp

	toolsRegistry := tools.NewToolRegistry()
	memoryProvider := newMemoryProvider(cfg, workspace)
	isMuninnMode := cfg != nil && strings.TrimSpace(cfg.Memory.Provider) == config.MemoryProviderMuninnDB
	if isMuninnMode {
		memoryDir := regexp.QuoteMeta(filepath.Join(workspace, "memory"))
		muninnMemoryDenyPaths = []*regexp.Regexp{
			regexp.MustCompile(`(?i)^` + memoryDir + `(?:$|[\\/])`),
			regexp.MustCompile(`(?i)(?:^|[\\/])memory(?:$|[\\/])`),
		}
	}

	if cfg.Tools.IsToolEnabled("read_file") {
		maxReadFileSize := cfg.Tools.ReadFile.MaxReadFileSize
		if isMuninnMode {
			toolsRegistry.Register(tools.NewReadFileToolWithDeny(workspace, readRestrict, maxReadFileSize, allowReadPaths, muninnMemoryDenyPaths))
		} else {
			toolsRegistry.Register(tools.NewReadFileTool(workspace, readRestrict, maxReadFileSize, allowReadPaths))
		}
	}
	if cfg.Tools.IsToolEnabled("write_file") {
		if isMuninnMode {
			toolsRegistry.Register(tools.NewWriteFileToolWithDeny(workspace, restrict, allowWritePaths, muninnMemoryDenyPaths))
		} else {
			toolsRegistry.Register(tools.NewWriteFileTool(workspace, restrict, allowWritePaths))
		}
	}
	if cfg.Tools.IsToolEnabled("list_dir") {
		if isMuninnMode {
			toolsRegistry.Register(tools.NewListDirToolWithDeny(workspace, readRestrict, allowReadPaths, muninnMemoryDenyPaths))
		} else {
			toolsRegistry.Register(tools.NewListDirTool(workspace, readRestrict, allowReadPaths))
		}
	}
	if cfg.Tools.IsToolEnabled("exec") {
		execTool, err := tools.NewExecToolWithConfig(workspace, restrict, cfg, allowReadPaths)
		if err != nil {
			log.Fatalf("Critical error: unable to initialize exec tool: %v", err)
		}
		if isMuninnMode {
			if err := execTool.SetDenyPathPatterns([]string{`(?i)(?:^|[\\/])memory(?:$|[\\/])`, regexp.QuoteMeta(filepath.Join(workspace, "memory"))}); err != nil {
				log.Fatalf("Critical error: unable to configure exec deny paths: %v", err)
			}
		}
		toolsRegistry.Register(execTool)
	}
	if cfg.Tools.IsToolEnabled("edit_file") {
		if isMuninnMode {
			toolsRegistry.Register(tools.NewEditFileToolWithDeny(workspace, restrict, allowWritePaths, muninnMemoryDenyPaths))
		} else {
			toolsRegistry.Register(tools.NewEditFileTool(workspace, restrict, allowWritePaths))
		}
	}
	if cfg.Tools.IsToolEnabled("append_file") {
		if isMuninnMode {
			toolsRegistry.Register(tools.NewAppendFileToolWithDeny(workspace, restrict, allowWritePaths, muninnMemoryDenyPaths))
		} else {
			toolsRegistry.Register(tools.NewAppendFileTool(workspace, restrict, allowWritePaths))
		}
	}

	if !isMuninnMode {
		if cfg.Tools.IsToolEnabled("memory_store") {
			toolsRegistry.Register(tools.NewMemoryStoreTool(memoryProvider))
		}
		if cfg.Tools.IsToolEnabled("memory_recall") {
			toolsRegistry.Register(tools.NewMemoryRecallTool(memoryProvider))
		}
	}

	sessionsDir := filepath.Join(workspace, "sessions")
	sessions := initSessionStore(sessionsDir)

	mcpDiscoveryActive := cfg.Tools.MCP.Enabled && cfg.Tools.MCP.Discovery.Enabled
	muninnVault := ""
	if cfg != nil && cfg.Memory.MuninnDB != nil {
		muninnVault = cfg.Memory.MuninnDB.Vault
	}
	contextBuilder := NewContextBuilderWithMemoryMode(workspace, memoryProvider, isMuninnMode).WithMuninnVault(muninnVault).WithToolDiscovery(
		mcpDiscoveryActive && cfg.Tools.MCP.Discovery.UseBM25,
		mcpDiscoveryActive && cfg.Tools.MCP.Discovery.UseRegex,
	)

	agentID := routing.DefaultAgentID
	agentName := ""
	var subagents *config.SubagentsConfig
	var skillsFilter []string
	if agentCfg != nil {
		agentID = routing.NormalizeAgentID(agentCfg.ID)
		agentName = agentCfg.Name
		subagents = agentCfg.Subagents
		skillsFilter = agentCfg.Skills
	}

	maxIter := defaults.MaxToolIterations
	if maxIter == 0 {
		maxIter = 20
	}
	maxTokens := defaults.MaxTokens
	if maxTokens == 0 {
		maxTokens = 8192
	}
	temperature := 0.7
	if defaults.Temperature != nil {
		temperature = *defaults.Temperature
	}
	var thinkingLevelStr string
	if mc, err := cfg.GetModelConfig(model); err == nil {
		thinkingLevelStr = mc.ThinkingLevel
	}
	thinkingLevel := parseThinkingLevel(thinkingLevelStr)
	SummarizeMessageThreshold := defaults.SummarizeMessageThreshold
	if SummarizeMessageThreshold == 0 {
		SummarizeMessageThreshold = 20
	}
	SummarizeTokenPercent := defaults.SummarizeTokenPercent
	if SummarizeTokenPercent == 0 {
		SummarizeTokenPercent = 75
	}

	modelCfg := providers.ModelConfig{Primary: model, Fallbacks: fallbacks}
	resolveFromModelList := func(raw string) (string, bool) {
		ensureProtocol := func(model string) string {
			model = strings.TrimSpace(model)
			if model == "" {
				return ""
			}
			if strings.Contains(model, "/") {
				return model
			}
			return "openai/" + model
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return "", false
		}
		if cfg != nil {
			if mc, err := cfg.GetModelConfig(raw); err == nil && mc != nil && strings.TrimSpace(mc.Model) != "" {
				return ensureProtocol(mc.Model), true
			}
			for i := range cfg.ModelList {
				fullModel := strings.TrimSpace(cfg.ModelList[i].Model)
				if fullModel == "" {
					continue
				}
				if fullModel == raw {
					return ensureProtocol(fullModel), true
				}
				_, modelID := providers.ExtractProtocol(fullModel)
				if modelID == raw {
					return ensureProtocol(fullModel), true
				}
			}
		}
		return "", false
	}
	candidates := providers.ResolveCandidatesWithLookup(modelCfg, defaults.Provider, resolveFromModelList)
	var router *routing.Router
	var lightCandidates []providers.FallbackCandidate
	if rc := defaults.Routing; rc != nil && rc.Enabled && rc.LightModel != "" {
		lightModelCfg := providers.ModelConfig{Primary: rc.LightModel}
		resolved := providers.ResolveCandidatesWithLookup(lightModelCfg, defaults.Provider, resolveFromModelList)
		if len(resolved) > 0 {
			router = routing.New(routing.RouterConfig{LightModel: rc.LightModel, Threshold: rc.Threshold})
			lightCandidates = resolved
		} else {
			log.Printf("routing: light_model %q not found in model_list - routing disabled for agent %q", rc.LightModel, agentID)
		}
	}

	return &AgentInstance{
		ID:                        agentID,
		Name:                      agentName,
		Model:                     model,
		Fallbacks:                 fallbacks,
		Workspace:                 workspace,
		MaxIterations:             maxIter,
		MaxTokens:                 maxTokens,
		Temperature:               temperature,
		ThinkingLevel:             thinkingLevel,
		ContextWindow:             maxTokens,
		SummarizeMessageThreshold: SummarizeMessageThreshold,
		SummarizeTokenPercent:     SummarizeTokenPercent,
		Provider:                  provider,
		Sessions:                  sessions,
		ContextBuilder:            contextBuilder,
		Tools:                     toolsRegistry,
		Subagents:                 subagents,
		SkillsFilter:              skillsFilter,
		Candidates:                candidates,
		Router:                    router,
		LightCandidates:           lightCandidates,
	}
}

func resolveAgentWorkspace(agentCfg *config.AgentConfig, defaults *config.AgentDefaults) string {
	if agentCfg != nil && strings.TrimSpace(agentCfg.Workspace) != "" {
		return expandHome(strings.TrimSpace(agentCfg.Workspace))
	}
	if agentCfg == nil || agentCfg.Default || agentCfg.ID == "" || routing.NormalizeAgentID(agentCfg.ID) == "main" {
		return expandHome(defaults.Workspace)
	}
	id := routing.NormalizeAgentID(agentCfg.ID)
	return filepath.Join(expandHome(defaults.Workspace), "..", "workspace-"+id)
}

func resolveAgentModel(agentCfg *config.AgentConfig, defaults *config.AgentDefaults) string {
	if agentCfg != nil && agentCfg.Model != nil && strings.TrimSpace(agentCfg.Model.Primary) != "" {
		return strings.TrimSpace(agentCfg.Model.Primary)
	}
	return defaults.GetModelName()
}

func resolveAgentFallbacks(agentCfg *config.AgentConfig, defaults *config.AgentDefaults) []string {
	if agentCfg != nil && agentCfg.Model != nil && agentCfg.Model.Fallbacks != nil {
		return agentCfg.Model.Fallbacks
	}
	return defaults.ModelFallbacks
}

func newMemoryProvider(cfg *config.Config, workspace string) MemoryProvider {
	if cfg == nil {
		logger.InfoCF("agent", "Initialized memory backend", map[string]any{"provider": config.MemoryProviderFile, "workspace": workspace})
		return NewFileMemoryStore(workspace)
	}
	providerName := strings.TrimSpace(cfg.Memory.Provider)
	if providerName == "" {
		providerName = config.MemoryProviderFile
	}
	switch providerName {
	case config.MemoryProviderMuninnDB:
		logger.InfoCF("agent", "Initialized Muninn memory mode via MCP", map[string]any{"provider": providerName, "workspace": workspace})
		return NewNoopMemoryStore()
	case "", config.MemoryProviderFile:
		fallthrough
	default:
		if providerName != config.MemoryProviderFile {
			logger.WarnCF("agent", "Unknown memory backend, falling back to file memory", map[string]any{"provider": providerName, "workspace": workspace})
		}
		logger.InfoCF("agent", "Initialized memory backend", map[string]any{"provider": config.MemoryProviderFile, "workspace": workspace})
		return NewFileMemoryStore(workspace)
	}
}

func compilePatterns(patterns []string) []*regexp.Regexp {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			fmt.Printf("Warning: invalid path pattern %q: %v\n", p, err)
			continue
		}
		compiled = append(compiled, re)
	}
	return compiled
}

func buildAllowReadPatterns(cfg *config.Config) []*regexp.Regexp {
	var configured []string
	if cfg != nil {
		configured = cfg.Tools.AllowReadPaths
	}

	compiled := compilePatterns(configured)
	mediaDirPattern := regexp.MustCompile(mediaTempDirPattern())
	for _, pattern := range compiled {
		if pattern.String() == mediaDirPattern.String() {
			return compiled
		}
	}

	return append(compiled, mediaDirPattern)
}

func mediaTempDirPattern() string {
	sep := regexp.QuoteMeta(string(os.PathSeparator))
	return "^" + regexp.QuoteMeta(filepath.Clean(media.TempDir())) + "(?:" + sep + "|$)"
}

// Close releases resources held by the agent's session store.
func (a *AgentInstance) Close() error {
	if a.Sessions != nil {
		return a.Sessions.Close()
	}
	return nil
}

// initSessionStore creates the session persistence backend.
// It uses the JSONL store by default and auto-migrates legacy JSON sessions.
// Falls back to SessionManager if the JSONL store cannot be initialized or
// if migration fails (which indicates the store cannot write reliably).
func initSessionStore(dir string) session.SessionStore {
	store, err := memory.NewJSONLStore(dir)
	if err != nil {
		log.Printf("memory: init store: %v; using json sessions", err)
		return session.NewSessionManager(dir)
	}

	if n, merr := memory.MigrateFromJSON(context.Background(), dir, store); merr != nil {
		// Migration failure means the store could not write data.
		// Fall back to SessionManager to avoid a split state where
		// some sessions are in JSONL and others remain in JSON.
		log.Printf("memory: migration failed: %v; falling back to json sessions", merr)
		store.Close()
		return session.NewSessionManager(dir)
	} else if n > 0 {
		log.Printf("memory: migrated %d session(s) to jsonl", n)
	}

	return session.NewJSONLBackend(store)
}

func expandHome(path string) string {
	if path == "" {
		return path
	}
	if path[0] == '~' {
		home, _ := os.UserHomeDir()
		if len(path) > 1 && path[1] == '/' {
			return home + path[1:]
		}
		return home
	}
	return path
}
