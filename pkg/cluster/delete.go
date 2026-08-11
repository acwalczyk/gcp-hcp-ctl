package cluster

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newDeleteCmd() *cobra.Command {
	var confirm bool

	cmd := &cobra.Command{
		Use:   "delete <cluster-name>",
		Short: "Delete a cluster",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("cluster name is required\n\nUsage: %s", cmd.UseLine())
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if !confirm {
				return fmt.Errorf("--confirm is required to delete a cluster")
			}

			client := clientFromCmd(cmd)
			name := args[0]

			cluster, err := client.ResolveCluster(cmd.Context(), name)
			if err != nil {
				return err
			}

			if err := client.Clusters().Delete(cmd.Context(), cluster.Namespace, cluster.Name); err != nil {
				return fmt.Errorf("deleting cluster %s: %w", name, err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Cluster %s (project: %s) deletion initiated.\n", name, cluster.Namespace)
			return nil
		},
	}

	cmd.Flags().BoolVar(&confirm, "confirm", false, "Confirm deletion (required)")
	return cmd
}
