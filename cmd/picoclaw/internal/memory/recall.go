package memory

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRecallCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recall",
		Short: "Recall memories (not available in Muninn MCP-only mode)",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("memory recall CLI is not available in Muninn MCP-only mode; use official Muninn MCP tools instead")
		},
	}
	return cmd
}
