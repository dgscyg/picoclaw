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

func newRecallCommand() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "recall <query>",
		Short: "Recall memories from the configured backend",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := internalcmd.LoadConfig()
			if err != nil {
				return fmt.Errorf("error loading config: %w", err)
			}

			result, err := recallMemories(context.Background(), cfg, strings.TrimSpace(args[0]), limit)
			if err != nil {
				return err
			}

			if len(result) == 0 {
				cmd.Println("No memories found.")
				return nil
			}

			for i, entry := range result {
				if i > 0 {
					cmd.Println("---")
				}
				cmd.Printf("[%d]\n%s\n", i+1, entry)
			}
			return nil
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 5, "Maximum number of memories to return")

	return cmd
}

func recallMemories(ctx context.Context, cfg *config.Config, query string, limit int) ([]string, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if limit <= 0 {
		limit = 5
	}

	provider := strings.TrimSpace(cfg.Memory.Provider)
	switch provider {
	case config.MemoryProviderMuninnDB:
		client, err := newMuninnDBClient(cfg)
		if err != nil {
			return nil, err
		}
		resp, err := client.Activate(ctx, strings.TrimSpace(query), limit)
		if err != nil {
			return nil, fmt.Errorf("muninndb recall: %w", err)
		}
		entries := make([]string, 0, len(resp.Activations))
		for _, item := range resp.Activations {
			content := formatActivation(item)
			if content == "" {
				continue
			}
			entries = append(entries, content)
		}
		return entries, nil
	case "", config.MemoryProviderFile:
		return nil, fmt.Errorf("memory recall CLI currently supports only provider %q", config.MemoryProviderMuninnDB)
	default:
		return nil, fmt.Errorf("unsupported memory provider %q", provider)
	}
}

func formatActivation(item muninndb.ActivationItem) string {
	content := strings.TrimSpace(item.Content)
	if content == "" {
		return ""
	}

	parts := []string{content}
	if item.Concept != "" {
		parts = append(parts, "Concept: "+item.Concept)
	}
	if item.Score > 0 {
		parts = append(parts, fmt.Sprintf("Relevance: %.2f", item.Score))
	}
	return strings.Join(parts, "\n")
}
