// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	picomcp "github.com/sipeed/picoclaw/pkg/mcp"
	"github.com/sipeed/picoclaw/pkg/tools"
)

type mcpRuntime struct {
	initOnce sync.Once
	mu       sync.Mutex
	manager  *picomcp.Manager
	initErr  error
}

func (r *mcpRuntime) setManager(manager *picomcp.Manager) {
	r.mu.Lock()
	r.manager = manager
	r.initErr = nil
	r.mu.Unlock()
}

func (r *mcpRuntime) setInitErr(err error) {
	r.mu.Lock()
	r.initErr = err
	r.mu.Unlock()
}

func (r *mcpRuntime) getInitErr() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.initErr
}

func (r *mcpRuntime) takeManager() *picomcp.Manager {
	r.mu.Lock()
	defer r.mu.Unlock()
	manager := r.manager
	r.manager = nil
	return manager
}

func (r *mcpRuntime) hasManager() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.manager != nil
}

// ensureMCPInitialized loads MCP servers/tools once so both Run() and direct
// agent mode share the same initialization path.
func (al *AgentLoop) ensureMCPInitialized(ctx context.Context) error {
	if !al.cfg.Tools.IsToolEnabled("mcp") {
		return nil
	}

	if al.cfg.Tools.MCP.Servers == nil || len(al.cfg.Tools.MCP.Servers) == 0 {
		logger.WarnCF("agent", "MCP is enabled but no servers are configured, skipping MCP initialization", nil)
		return nil
	}

	findValidServer := false
	for _, serverCfg := range al.cfg.Tools.MCP.Servers {
		if serverCfg.Enabled {
			findValidServer = true
		}
	}
	if !findValidServer {
		logger.WarnCF("agent", "MCP is enabled but no valid servers are configured, skipping MCP initialization", nil)
		return nil
	}

	al.mcp.initOnce.Do(func() {
		mcpManager := picomcp.NewManager()

		defaultAgent := al.registry.GetDefaultAgent()
		workspacePath := al.cfg.WorkspacePath()
		if defaultAgent != nil && defaultAgent.Workspace != "" {
			workspacePath = defaultAgent.Workspace
		}

		if err := mcpManager.LoadFromMCPConfig(ctx, al.cfg.Tools.MCP, workspacePath); err != nil {
			logger.WarnCF("agent", "Failed to load MCP servers, MCP tools will not be available",
				map[string]any{
					"error": err.Error(),
				})
			if closeErr := mcpManager.Close(); closeErr != nil {
				logger.ErrorCF("agent", "Failed to close MCP manager",
					map[string]any{
						"error": closeErr.Error(),
					})
			}
			return
		}

		// Register MCP tools for all agents
		servers := mcpManager.GetServers()
		uniqueTools := 0
		totalRegistrations := 0
		agentIDs := al.registry.ListAgentIDs()
		agentCount := len(agentIDs)

		for serverName, conn := range servers {
			uniqueTools += len(conn.Tools)
			for _, tool := range conn.Tools {
				for _, agentID := range agentIDs {
					agent, ok := al.registry.GetAgent(agentID)
					if !ok {
						continue
					}

					mcpTool := tools.NewMCPToolWithArgPolicy(
						mcpManager,
						serverName,
						tool,
						nil,
						al.muninnForcedMCPArgs(serverName, tool),
					)
					if forcedArgs := al.muninnForcedMCPArgs(serverName, tool); len(forcedArgs) > 0 {
						logger.DebugCF("agent", "Applying forced MCP tool arguments",
							map[string]any{
								"agent_id":    agentID,
								"server":      serverName,
								"tool":        tool.Name,
								"forced_args": forcedArgs,
							})
					}

					if al.cfg.Tools.MCP.Discovery.Enabled {
						agent.Tools.RegisterHidden(mcpTool)
					} else {
						agent.Tools.Register(mcpTool)
					}

					totalRegistrations++
					logger.DebugCF("agent", "Registered MCP tool",
						map[string]any{
							"agent_id": agentID,
							"server":   serverName,
							"tool":     tool.Name,
							"name":     mcpTool.Name(),
						})
				}
			}
		}
		logger.InfoCF("agent", "MCP tools registered successfully",
			map[string]any{
				"server_count":        len(servers),
				"unique_tools":        uniqueTools,
				"total_registrations": totalRegistrations,
				"agent_count":         agentCount,
			})

		// Initializes Discovery Tools only if enabled by configuration
		if al.cfg.Tools.MCP.Enabled && al.cfg.Tools.MCP.Discovery.Enabled {
			useBM25 := al.cfg.Tools.MCP.Discovery.UseBM25
			useRegex := al.cfg.Tools.MCP.Discovery.UseRegex

			// Fail fast: If discovery is enabled but no search method is turned on
			if !useBM25 && !useRegex {
				al.mcp.setInitErr(fmt.Errorf(
					"tool discovery is enabled but neither 'use_bm25' nor 'use_regex' is set to true in the configuration",
				))
				if closeErr := mcpManager.Close(); closeErr != nil {
					logger.ErrorCF("agent", "Failed to close MCP manager",
						map[string]any{
							"error": closeErr.Error(),
						})
				}
				return
			}

			ttl := al.cfg.Tools.MCP.Discovery.TTL
			if ttl <= 0 {
				ttl = 5 // Default value
			}

			maxSearchResults := al.cfg.Tools.MCP.Discovery.MaxSearchResults
			if maxSearchResults <= 0 {
				maxSearchResults = 5 // Default value
			}

			logger.InfoCF("agent", "Initializing tool discovery", map[string]any{
				"bm25": useBM25, "regex": useRegex, "ttl": ttl, "max_results": maxSearchResults,
			})

			for _, agentID := range agentIDs {
				agent, ok := al.registry.GetAgent(agentID)
				if !ok {
					continue
				}

				if useRegex {
					agent.Tools.Register(tools.NewRegexSearchTool(agent.Tools, ttl, maxSearchResults))
				}
				if useBM25 {
					agent.Tools.Register(tools.NewBM25SearchTool(agent.Tools, ttl, maxSearchResults))
				}
			}
		}

		al.mcp.setManager(mcpManager)
	})

	return al.mcp.getInitErr()
}

func (al *AgentLoop) muninnForcedMCPArgs(serverName string, tool *mcp.Tool) map[string]any {
	if al == nil || al.cfg == nil || serverName != config.DefaultMuninnMCPName {
		return nil
	}
	if strings.TrimSpace(al.cfg.Memory.Provider) != config.MemoryProviderMuninnDB || al.cfg.Memory.MuninnDB == nil {
		return nil
	}
	vault := strings.TrimSpace(al.cfg.Memory.MuninnDB.Vault)
	if vault == "" || tool == nil {
		return nil
	}
	toolName := strings.TrimSpace(tool.Name)
	if !strings.HasPrefix(toolName, "muninn_") && !mcpToolHasInputProperty(tool, "vault") {
		return nil
	}
	return map[string]any{"vault": vault}
}

func mcpToolHasInputProperty(tool *mcp.Tool, property string) bool {
	if tool == nil || property == "" || tool.InputSchema == nil {
		return false
	}
	schemaMap, ok := normalizeMCPSchema(tool.InputSchema)
	if !ok {
		return false
	}
	props, ok := schemaMap["properties"].(map[string]any)
	if !ok {
		return false
	}
	_, exists := props[property]
	return exists
}

func normalizeMCPSchema(schema any) (map[string]any, bool) {
	if schema == nil {
		return nil, false
	}
	if schemaMap, ok := schema.(map[string]any); ok {
		return schemaMap, true
	}
	var jsonData []byte
	switch v := schema.(type) {
	case json.RawMessage:
		jsonData = v
	case []byte:
		jsonData = v
	default:
		var err error
		jsonData, err = json.Marshal(schema)
		if err != nil {
			return nil, false
		}
	}
	var out map[string]any
	if err := json.Unmarshal(jsonData, &out); err != nil {
		return nil, false
	}
	return out, true
}
