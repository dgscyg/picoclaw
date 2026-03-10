package memory

import (
	"context"
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
				cmd.Printf("Endpoint: %s\n", cfg.Memory.MuninnDB.Endpoint)
				cmd.Printf("Vault: %s\n", cfg.Memory.MuninnDB.Vault)
				cmd.Printf("Fallback to file: %t\n", cfg.Memory.MuninnDB.FallbackToFile)

				if _, err := recallMemories(context.Background(), cfg, "", 1); err != nil {
					cmd.Printf("Status: error (%v)\n", err)
					return err
				}
				cmd.Println("Status: connected")
				return nil
			default:
				return fmt.Errorf("unsupported memory provider %q", cfg.Memory.Provider)
			}
		},
	}

	return cmd
}
