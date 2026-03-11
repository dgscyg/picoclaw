package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMemoryConfig_Defaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Memory.Provider != MemoryProviderFile {
		t.Fatalf("Memory.Provider = %q, want %q", cfg.Memory.Provider, MemoryProviderFile)
	}
	if cfg.Memory.File == nil {
		t.Fatal("Memory.File should not be nil")
	}
	if cfg.Memory.File.Workspace == "" {
		t.Fatal("Memory.File.Workspace should not be empty")
	}
	if cfg.Memory.MuninnDB == nil {
		t.Fatal("Memory.MuninnDB should not be nil")
	}
	if cfg.Memory.MuninnDB.Vault != DefaultMemoryVault {
		t.Fatalf("Memory.MuninnDB.Vault = %q, want %q", cfg.Memory.MuninnDB.Vault, DefaultMemoryVault)
	}
	if cfg.Memory.MuninnDB.Timeout != DefaultMemoryTimeout {
		t.Fatalf("Memory.MuninnDB.Timeout = %q, want %q", cfg.Memory.MuninnDB.Timeout, DefaultMemoryTimeout)
	}
}

func TestLoadConfig_MemoryEnvExpansion(t *testing.T) {
	t.Setenv("MUNINNDB_MCP_ENDPOINT", "http://localhost:8750/mcp")
	t.Setenv("MUNINNDB_VAULT", "team")
	t.Setenv("MUNINNDB_API_KEY", "secret-key")
	jsonData := `{
		"memory": {
			"provider": "muninndb",
			"muninndb": {
				"mcp_endpoint": "${MUNINNDB_MCP_ENDPOINT}",
				"vault": "${MUNINNDB_VAULT}",
				"api_key": "${MUNINNDB_API_KEY}"
			}
		}
	}`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(jsonData), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Memory.Provider != MemoryProviderMuninnDB {
		t.Fatalf("Memory.Provider = %q, want %q", cfg.Memory.Provider, MemoryProviderMuninnDB)
	}
	if cfg.Memory.MuninnDB.MCPEndpoint != "http://localhost:8750/mcp" {
		t.Fatalf("MCPEndpoint = %q", cfg.Memory.MuninnDB.MCPEndpoint)
	}
	if cfg.Memory.MuninnDB.Vault != "team" {
		t.Fatalf("Vault = %q", cfg.Memory.MuninnDB.Vault)
	}
	if cfg.Memory.MuninnDB.APIKey != "secret-key" {
		t.Fatalf("APIKey = %q", cfg.Memory.MuninnDB.APIKey)
	}
	server, ok := cfg.Tools.MCP.Servers[DefaultMuninnMCPName]
	if !ok {
		t.Fatal("expected default muninn MCP server config")
	}
	if server.URL != "http://localhost:8750/mcp" {
		t.Fatalf("muninn mcp url = %q", server.URL)
	}
	if server.Headers["Authorization"] != "Bearer secret-key" {
		t.Fatalf("muninn mcp auth header missing")
	}
}

func TestLoadConfig_MemoryValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	jsonData := `{
		"memory": {
			"provider": "muninndb",
			"muninndb": {
				"vault": "default"
			}
		}
	}`
	if err := os.WriteFile(path, []byte(jsonData), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("LoadConfig should fail when mcp_endpoint is missing")
	}
	if !strings.Contains(err.Error(), "memory.muninndb.mcp_endpoint") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureMuninnMCPConfigUsesOnlyAPIKey(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Memory.Provider = MemoryProviderMuninnDB
	cfg.Memory.MuninnDB = &MuninnDBConfig{MCPEndpoint: "http://127.0.0.1:8750/mcp", Vault: "default", APIKey: "mdb_test_token"}
	EnsureMuninnMCPConfig(cfg)
	if got := cfg.Tools.MCP.Servers[DefaultMuninnMCPName].Headers["Authorization"]; got != "Bearer mdb_test_token" {
		t.Fatalf("Authorization header = %q", got)
	}
}

func TestAgentModelConfig_UnmarshalString(t *testing.T) {
	var m AgentModelConfig
	if err := json.Unmarshal([]byte(`"gpt-4"`), &m); err != nil {
		t.Fatalf("unmarshal string: %v", err)
	}
	if m.Primary != "gpt-4" {
		t.Errorf("Primary = %q, want 'gpt-4'", m.Primary)
	}
	if m.Fallbacks != nil {
		t.Errorf("Fallbacks = %v, want nil", m.Fallbacks)
	}
}

func TestAgentModelConfig_UnmarshalObject(t *testing.T) {
	var m AgentModelConfig
	data := `{"primary": "claude-opus", "fallbacks": ["gpt-4o-mini", "haiku"]}`
	if err := json.Unmarshal([]byte(data), &m); err != nil {
		t.Fatalf("unmarshal object: %v", err)
	}
	if m.Primary != "claude-opus" {
		t.Errorf("Primary = %q, want 'claude-opus'", m.Primary)
	}
	if len(m.Fallbacks) != 2 {
		t.Fatalf("Fallbacks len = %d, want 2", len(m.Fallbacks))
	}
}

func TestAgentModelConfig_MarshalString(t *testing.T) {
	m := AgentModelConfig{Primary: "gpt-4"}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `"gpt-4"` {
		t.Errorf("marshal = %s, want string form", string(data))
	}
}

func TestAgentModelConfig_MarshalObject(t *testing.T) {
	m := AgentModelConfig{Primary: "claude-opus", Fallbacks: []string{"haiku"}}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var result map[string]any
	json.Unmarshal(data, &result)
	if result["primary"] != "claude-opus" {
		t.Errorf("primary = %v", result["primary"])
	}
}

func TestSaveConfig_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not enforced on Windows")
	}
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	cfg := DefaultConfig()
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config file has permission %04o, want 0600", info.Mode().Perm())
	}
}

func TestSaveConfig_IncludesEmptyLegacyModelField(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")
	cfg := DefaultConfig()
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !strings.Contains(string(data), `"model": ""`) {
		t.Fatalf("saved config should include empty legacy model field, got: %s", string(data))
	}
}
