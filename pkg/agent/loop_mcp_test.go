// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package agent

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sipeed/picoclaw/pkg/config"
)

func boolPtr(b bool) *bool { return &b }

func TestServerIsDeferred(t *testing.T) {
	tests := []struct {
		name             string
		discoveryEnabled bool
		serverDeferred   *bool
		want             bool
	}{
		{
			name:             "global false: per-server deferred=true is ignored",
			discoveryEnabled: false,
			serverDeferred:   boolPtr(true),
			want:             false,
		},
		{
			name:             "global false: per-server deferred=false stays false",
			discoveryEnabled: false,
			serverDeferred:   boolPtr(false),
			want:             false,
		},
		{
			name:             "global true: per-server deferred=false opts out",
			discoveryEnabled: true,
			serverDeferred:   boolPtr(false),
			want:             false,
		},
		{
			name:             "global true: per-server deferred=true stays true",
			discoveryEnabled: true,
			serverDeferred:   boolPtr(true),
			want:             true,
		},
		{
			name:             "no per-server field, global discovery enabled",
			discoveryEnabled: true,
			serverDeferred:   nil,
			want:             true,
		},
		{
			name:             "no per-server field, global discovery disabled",
			discoveryEnabled: false,
			serverDeferred:   nil,
			want:             false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serverCfg := config.MCPServerConfig{Deferred: tt.serverDeferred}
			got := serverIsDeferred(tt.discoveryEnabled, serverCfg)
			if got != tt.want {
				t.Errorf("serverIsDeferred(discoveryEnabled=%v, deferred=%v) = %v, want %v",
					tt.discoveryEnabled, tt.serverDeferred, got, tt.want)
			}
		})
	}
}

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
