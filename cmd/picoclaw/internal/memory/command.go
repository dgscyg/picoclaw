package memory

import (
	"github.com/spf13/cobra"
)

func NewMemoryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "memory",
		Short: "Memory management commands",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newRecallCommand())
	cmd.AddCommand(newStoreCommand())
	cmd.AddCommand(newStatusCommand())

	return cmd
}
