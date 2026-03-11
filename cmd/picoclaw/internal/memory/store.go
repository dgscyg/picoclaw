package memory

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStoreCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "store",
		Short: "Store content into memory (not available in Muninn MCP-only mode)",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("memory store CLI is not available in Muninn MCP-only mode; use official Muninn MCP tools instead")
		},
	}
	return cmd
}
