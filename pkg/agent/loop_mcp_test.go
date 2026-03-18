package agent

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestMuninnForcedMCPArgs_UsesConfiguredVault(t *testing.T) {
	al := &AgentLoop{
		cfg: &config.Config{
			Memory: config.MemoryConfig{
				Provider: config.MemoryProviderMuninnDB,
				MuninnDB: &config.MuninnDBConfig{
					Vault: "picoclaw",
				},
			},
		},
	}

	tool := &mcp.Tool{
		Name: "muninn_status",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"vault": map[string]any{"type": "string"},
			},
		},
	}

	args := al.muninnForcedMCPArgs(config.DefaultMuninnMCPName, tool)
	if got := args["vault"]; got != "picoclaw" {
		t.Fatalf("vault = %v, want picoclaw", got)
	}
}

func TestMuninnForcedMCPArgs_UsesConfiguredVaultForMuninnPrefixedToolWithoutSchemaVault(t *testing.T) {
	al := &AgentLoop{
		cfg: &config.Config{
			Memory: config.MemoryConfig{
				Provider: config.MemoryProviderMuninnDB,
				MuninnDB: &config.MuninnDBConfig{
					Vault: "picoclaw",
				},
			},
		},
	}

	tool := &mcp.Tool{
		Name: "muninn_recall",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"context": map[string]any{"type": "array"},
			},
		},
	}

	args := al.muninnForcedMCPArgs(config.DefaultMuninnMCPName, tool)
	if got := args["vault"]; got != "picoclaw" {
		t.Fatalf("vault = %v, want picoclaw", got)
	}
}

func TestMuninnForcedMCPArgs_IgnoresNonMuninnOrToolsWithoutVault(t *testing.T) {
	al := &AgentLoop{
		cfg: &config.Config{
			Memory: config.MemoryConfig{
				Provider: config.MemoryProviderMuninnDB,
				MuninnDB: &config.MuninnDBConfig{
					Vault: "picoclaw",
				},
			},
		},
	}

	noVaultTool := &mcp.Tool{
		Name: "health",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"limit": map[string]any{"type": "integer"},
			},
		},
	}
	if args := al.muninnForcedMCPArgs(config.DefaultMuninnMCPName, noVaultTool); args != nil {
		t.Fatalf("expected nil args for tool without vault, got %#v", args)
	}
	if args := al.muninnForcedMCPArgs("context7", noVaultTool); args != nil {
		t.Fatalf("expected nil args for non-muninn server, got %#v", args)
	}
}
