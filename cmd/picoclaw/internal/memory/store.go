package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	internalcmd "github.com/sipeed/picoclaw/cmd/picoclaw/internal"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/muninndb"
)

func newStoreCommand() *cobra.Command {
	var tags string
	var longTerm bool

	cmd := &cobra.Command{
		Use:   "store <content>",
		Short: "Store content in the configured memory backend",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := internalcmd.LoadConfig()
			if err != nil {
				return fmt.Errorf("error loading config: %w", err)
			}

			content := strings.TrimSpace(args[0])
			if content == "" {
				return fmt.Errorf("content cannot be empty")
			}

			if err := storeMemory(context.Background(), cfg, content, tags, longTerm); err != nil {
				return err
			}

			cmd.Println("Memory stored successfully.")
			return nil
		},
	}

	cmd.Flags().StringVar(&tags, "tags", "", "Comma-separated tags to attach to stored content")
	cmd.Flags().BoolVar(&longTerm, "long-term", true, "Store as long-term memory")

	return cmd
}

func storeMemory(ctx context.Context, cfg *config.Config, content, tags string, longTerm bool) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	provider := strings.TrimSpace(cfg.Memory.Provider)
	if provider == "" {
		provider = config.MemoryProviderFile
	}
	if provider != config.MemoryProviderMuninnDB {
		return fmt.Errorf("memory store CLI currently supports only provider %q", config.MemoryProviderMuninnDB)
	}

	client, err := newMuninnDBClient(cfg)
	if err != nil {
		return err
	}

	concept := "Daily Note"
	if longTerm {
		concept = "Long-term Memory"
	}

	resp, err := client.WriteEngram(ctx, strings.TrimSpace(content), buildTags(tags, longTerm), concept)
	if err != nil {
		return fmt.Errorf("muninndb store: %w", err)
	}
	if resp != nil {
		fmt.Printf("Memory stored with ID: %s\n", resp.ID)
	}
	return nil
}

func newMuninnDBClient(cfg *config.Config) (*muninndb.Client, error) {
	if cfg == nil || cfg.Memory.MuninnDB == nil {
		return nil, fmt.Errorf("memory.muninndb config is required")
	}
	endpoint := strings.TrimSpace(cfg.Memory.MuninnDB.Endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("memory.muninndb.endpoint is required")
	}
	vault := strings.TrimSpace(cfg.Memory.MuninnDB.Vault)
	if vault == "" {
		vault = config.DefaultMemoryVault
	}
	return muninndb.NewClient(endpoint, vault, strings.TrimSpace(cfg.Memory.MuninnDB.APIKey)), nil
}

func buildTags(tags string, longTerm bool) []string {
	result := make([]string, 0, 4)
	if longTerm {
		result = append(result, "long-term")
	} else {
		result = append(result, "daily-note")
	}
	for _, part := range strings.Split(tags, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		result = append(result, part)
	}
	return result
}
