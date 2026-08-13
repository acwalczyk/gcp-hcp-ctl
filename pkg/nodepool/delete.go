package nodepool

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newDeleteCmd() *cobra.Command {
	var confirm bool

	cmd := &cobra.Command{
		Use:   "delete <nodepool-name>",
		Short: "Delete a nodepool",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("nodepool name is required\n\nUsage: %s", cmd.UseLine())
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirm {
				return fmt.Errorf("--confirm is required to delete a nodepool")
			}

			client := clientFromCmd(cmd)
			ctx := cmd.Context()
			npName := args[0]

			if err := client.NodePools().Delete(ctx, client.Namespace(), npName); err != nil {
				return fmt.Errorf("deleting nodepool %s: %w", npName, err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Nodepool %s (project: %s) deletion initiated.\n", npName, client.Project())
			return nil
		},
	}

	cmd.Flags().BoolVar(&confirm, "confirm", false, "Confirm deletion (required)")
	return cmd
}
