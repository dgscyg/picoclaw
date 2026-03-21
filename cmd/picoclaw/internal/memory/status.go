package memory

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	internalcmd "github.com/sipeed/picoclaw/cmd/picoclaw/internal"
	"github.com/sipeed/picoclaw/pkg/config"
)

func newStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show memory backend status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := internalcmd.LoadConfig()
			if err != nil {
				return fmt.Errorf("error loading config: %w", err)
			}

			cmd.Printf("Provider: %s\n", cfg.Memory.Provider)
			switch strings.TrimSpace(cfg.Memory.Provider) {
			case "", config.MemoryProviderFile:
				cmd.Printf("Workspace: %s\n", cfg.WorkspacePath())
				cmd.Println("Status: ready")
				return nil
			case config.MemoryProviderMuninnDB:
				if cfg.Memory.MuninnDB == nil {
					return fmt.Errorf("memory.muninndb config is required")
				}
				cmd.Printf("MCP Endpoint: %s\n", cfg.Memory.MuninnDB.MCPEndpoint)
				cmd.Printf("Vault: %s\n", cfg.Memory.MuninnDB.Vault)
				cmd.Printf("MCP Enabled: %t\n", cfg.Tools.MCP.Enabled)
				if server, ok := cfg.Tools.MCP.Servers[config.DefaultMuninnMCPName]; ok {
					cmd.Printf("MCP Server URL: %s\n", server.URL)
				}
				cmd.Println("Status: configured for Muninn MCP mode")
				return nil
			default:
				return fmt.Errorf("unsupported memory provider %q", cfg.Memory.Provider)
			}
		},
	}

	return cmd
}
