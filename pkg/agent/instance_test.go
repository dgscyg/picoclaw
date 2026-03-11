package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestNewAgentInstance_UsesDefaultsTemperatureAndMaxTokens(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-instance-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{Agents: config.AgentsConfig{Defaults: config.AgentDefaults{Workspace: tmpDir, Model: "test-model", MaxTokens: 1234, MaxToolIterations: 5}}}
	configuredTemp := 1.0
	cfg.Agents.Defaults.Temperature = &configuredTemp
	provider := &mockProvider{}
	agent := NewAgentInstance(nil, &cfg.Agents.Defaults, cfg, provider)
	if agent.MaxTokens != 1234 {
		t.Fatalf("MaxTokens = %d, want %d", agent.MaxTokens, 1234)
	}
	if agent.Temperature != 1.0 {
		t.Fatalf("Temperature = %f, want %f", agent.Temperature, 1.0)
	}
}

func TestNewAgentInstance_DefaultsTemperatureWhenZero(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-instance-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	cfg := &config.Config{Agents: config.AgentsConfig{Defaults: config.AgentDefaults{Workspace: tmpDir, Model: "test-model", MaxTokens: 1234, MaxToolIterations: 5}}}
	configuredTemp := 0.0
	cfg.Agents.Defaults.Temperature = &configuredTemp
	provider := &mockProvider{}
	agent := NewAgentInstance(nil, &cfg.Agents.Defaults, cfg, provider)
	if agent.Temperature != 0.0 {
		t.Fatalf("Temperature = %f, want %f", agent.Temperature, 0.0)
	}
}

func TestNewAgentInstance_DefaultsTemperatureWhenUnset(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-instance-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)
	cfg := &config.Config{Agents: config.AgentsConfig{Defaults: config.AgentDefaults{Workspace: tmpDir, Model: "test-model", MaxTokens: 1234, MaxToolIterations: 5}}}
	provider := &mockProvider{}
	agent := NewAgentInstance(nil, &cfg.Agents.Defaults, cfg, provider)
	if agent.Temperature != 0.7 {
		t.Fatalf("Temperature = %f, want %f", agent.Temperature, 0.7)
	}
}

func TestNewAgentInstance_DoesNotRegisterLocalMemoryToolsInMuninnMode(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-instance-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{Defaults: config.AgentDefaults{Workspace: tmpDir, Model: "test-model", MaxToolIterations: 5}},
		Memory: config.MemoryConfig{Provider: config.MemoryProviderMuninnDB, MuninnDB: &config.MuninnDBConfig{MCPEndpoint: "http://127.0.0.1:8750/mcp", Vault: "default"}},
	}
	provider := &mockProvider{}
	agent := NewAgentInstance(nil, &cfg.Agents.Defaults, cfg, provider)
	tools := agent.Tools.List()
	for _, unwanted := range []string{"memory_store", "memory_recall", "muninn_link", "muninn_traverse", "muninn_explain", "muninn_contradictions"} {
		for _, got := range tools {
			if got == unwanted {
				t.Fatalf("tool %q should not be registered in MCP-only Muninn mode, got %v", unwanted, tools)
			}
		}
	}
}

func TestNewAgentInstance_ResolveCandidatesFromModelListAlias(t *testing.T) {
	tests := []struct {
		name         string
		aliasName    string
		modelName    string
		apiBase      string
		wantProvider string
		wantModel    string
	}{
		{name: "alias with provider prefix", aliasName: "step-3.5-flash", modelName: "openrouter/stepfun/step-3.5-flash:free", apiBase: "https://openrouter.ai/api/v1", wantProvider: "openrouter", wantModel: "stepfun/step-3.5-flash:free"},
		{name: "alias without provider prefix", aliasName: "glm-5", modelName: "glm-5", apiBase: "https://api.z.ai/api/coding/paas/v4", wantProvider: "openai", wantModel: "glm-5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "agent-instance-test-*")
			if err != nil {
				t.Fatalf("Failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)
			cfg := &config.Config{Agents: config.AgentsConfig{Defaults: config.AgentDefaults{Workspace: tmpDir, Model: tt.aliasName}}, ModelList: []config.ModelConfig{{ModelName: tt.aliasName, Model: tt.modelName, APIBase: tt.apiBase}}}
			provider := &mockProvider{}
			agent := NewAgentInstance(nil, &cfg.Agents.Defaults, cfg, provider)
			if len(agent.Candidates) != 1 {
				t.Fatalf("len(Candidates) = %d, want 1", len(agent.Candidates))
			}
			if agent.Candidates[0].Provider != tt.wantProvider {
				t.Fatalf("candidate provider = %q, want %q", agent.Candidates[0].Provider, tt.wantProvider)
			}
			if agent.Candidates[0].Model != tt.wantModel {
				t.Fatalf("candidate model = %q, want %q", agent.Candidates[0].Model, tt.wantModel)
			}
		})
	}
}

func TestNewMemoryProvider_UsesNoopStoreInMuninnMode(t *testing.T) {
	workspace := t.TempDir()
	cfg := &config.Config{
		Memory: config.MemoryConfig{
			Provider: config.MemoryProviderMuninnDB,
			MuninnDB: &config.MuninnDBConfig{MCPEndpoint: "http://127.0.0.1:8750/mcp", Vault: "default"},
		},
	}
	provider := newMemoryProvider(cfg, workspace)
	if _, ok := provider.(*NoopMemoryStore); !ok {
		t.Fatalf("provider type = %T, want *NoopMemoryStore", provider)
	}
}
func TestNewAgentInstance_BlocksWorkspaceMemoryFilesInMuninnMode(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.Config{
		Agents: config.AgentsConfig{Defaults: config.AgentDefaults{Workspace: tmpDir, Model: "test-model", MaxToolIterations: 5, RestrictToWorkspace: true}},
		Tools:  config.ToolsConfig{ReadFile: config.ToolConfig{Enabled: true}, Exec: config.ExecConfig{ToolConfig: config.ToolConfig{Enabled: true}}},
		Memory: config.MemoryConfig{Provider: config.MemoryProviderMuninnDB, MuninnDB: &config.MuninnDBConfig{MCPEndpoint: "http://127.0.0.1:8750/mcp", Vault: "default"}},
	}
	provider := &mockProvider{}
	agent := NewAgentInstance(nil, &cfg.Agents.Defaults, cfg, provider)
	readResult := agent.Tools.ExecuteWithContext(context.Background(), "read_file", map[string]any{"path": filepath.Join(tmpDir, "memory", "MEMORY.md")}, "", "", "", nil)
	if !readResult.IsError {
		t.Fatalf("expected read_file on workspace memory to be blocked")
	}
	if !strings.Contains(readResult.ForLLM, "disabled in Muninn MCP-only mode") {
		t.Fatalf("unexpected read_file error: %s", readResult.ForLLM)
	}
	execResult := agent.Tools.ExecuteWithContext(context.Background(), "exec", map[string]any{"command": "Get-Content .\\memory\\MEMORY.md"}, "", "", "", nil)
	if !execResult.IsError {
		t.Fatalf("expected exec on workspace memory to be blocked")
	}
	if !strings.Contains(execResult.ForLLM, "disabled in Muninn MCP-only mode") {
		t.Fatalf("unexpected exec error: %s", execResult.ForLLM)
	}
}






