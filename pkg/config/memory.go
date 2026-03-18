package config

import (
	"fmt"
	"os"
	"strings"
)

const (
	MemoryProviderFile     = "file"
	MemoryProviderMuninnDB = "muninndb"
	DefaultMemoryTimeout   = "30s"
	DefaultMemoryVault     = "default"
	DefaultMuninnMCPName   = "muninn"
)

type MemoryConfig struct {
	Provider string            `json:"provider"`
	File     *FileMemoryConfig `json:"file,omitempty"`
	MuninnDB *MuninnDBConfig   `json:"muninndb,omitempty"`
}

type FileMemoryConfig struct {
	Workspace string `json:"workspace"`
}

type MuninnDBConfig struct {
	MCPEndpoint string `json:"mcp_endpoint"`
	Vault       string `json:"vault"`
	APIKey      string `json:"api_key"`
	Timeout     string `json:"timeout"`
}

func (c *MemoryConfig) ApplyDefaults() {
	if c.Provider == "" {
		c.Provider = MemoryProviderFile
	}
	if c.File == nil {
		c.File = &FileMemoryConfig{}
	}
	if c.MuninnDB == nil {
		c.MuninnDB = &MuninnDBConfig{}
	}
	if c.MuninnDB.Vault == "" {
		c.MuninnDB.Vault = DefaultMemoryVault
	}
	if c.MuninnDB.Timeout == "" {
		c.MuninnDB.Timeout = DefaultMemoryTimeout
	}
}

func (c *MemoryConfig) ExpandEnvVars() {
	if c == nil {
		return
	}
	if c.File != nil {
		c.File.Workspace = expandEnvVars(c.File.Workspace)
	}
	if c.MuninnDB != nil {
		c.MuninnDB.MCPEndpoint = expandEnvVars(c.MuninnDB.MCPEndpoint)
		c.MuninnDB.Vault = expandEnvVars(c.MuninnDB.Vault)
		c.MuninnDB.APIKey = expandEnvVars(c.MuninnDB.APIKey)
		c.MuninnDB.Timeout = expandEnvVars(c.MuninnDB.Timeout)
	}
}

func (c *MemoryConfig) Validate() error {
	if c == nil {
		return nil
	}
	switch c.Provider {
	case "", MemoryProviderFile:
		return nil
	case MemoryProviderMuninnDB:
		if c.MuninnDB == nil {
			return fmt.Errorf("memory.muninndb is required when memory.provider is %q", MemoryProviderMuninnDB)
		}
		if strings.TrimSpace(c.MuninnDB.MCPEndpoint) == "" {
			return fmt.Errorf("memory.muninndb.mcp_endpoint is required when memory.provider is %q", MemoryProviderMuninnDB)
		}
		if strings.TrimSpace(c.MuninnDB.Vault) == "" {
			return fmt.Errorf("memory.muninndb.vault is required when memory.provider is %q", MemoryProviderMuninnDB)
		}
		return nil
	default:
		return fmt.Errorf("memory.provider must be %q or %q", MemoryProviderFile, MemoryProviderMuninnDB)
	}
}

func EnsureMuninnMCPConfig(cfg *Config) {
	if cfg == nil || strings.TrimSpace(cfg.Memory.Provider) != MemoryProviderMuninnDB {
		return
	}
	if !cfg.Tools.MCP.Enabled {
		cfg.Tools.MCP.Enabled = true
	}
	if cfg.Tools.MCP.Servers == nil {
		cfg.Tools.MCP.Servers = map[string]MCPServerConfig{}
	}
	if _, exists := cfg.Tools.MCP.Servers[DefaultMuninnMCPName]; exists {
		return
	}
	server := MCPServerConfig{
		Enabled: true,
		Type:    "http",
		URL:     strings.TrimSpace(cfg.Memory.MuninnDB.MCPEndpoint),
	}
	if token := strings.TrimSpace(cfg.Memory.MuninnDB.APIKey); token != "" {
		server.Headers = map[string]string{"Authorization": "Bearer " + token}
	}
	cfg.Tools.MCP.Servers[DefaultMuninnMCPName] = server
}

func expandEnvVars(s string) string {
	if strings.HasPrefix(s, "${") && strings.HasSuffix(s, "}") {
		envVar := s[2 : len(s)-1]
		return os.Getenv(envVar)
	}
	return s
}
