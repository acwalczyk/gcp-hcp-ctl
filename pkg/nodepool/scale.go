package nodepool

import (
	"encoding/json"
	"fmt"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/output"
	"github.com/spf13/cobra"
)

func newScaleCmd() *cobra.Command {
	var (
		replicaCount int32
		outputFmt    string
	)

	cmd := &cobra.Command{
		Use:   "scale <nodepool-name>",
		Short: "Scale a nodepool's replica count",
		Long: `Scale the number of replicas for a nodepool.

  gcphcpctl nodepool scale my-nodepool --replicas 5
  gcphcpctl nodepool scale my-nodepool --replicas 0`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("nodepool name is required\n\nUsage: %s", cmd.UseLine())
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("replicas") {
				return fmt.Errorf("--replicas is required")
			}
			if replicaCount < 0 {
				return fmt.Errorf("--replicas must be non-negative")
			}

			client := clientFromCmd(cmd)
			ctx := cmd.Context()
			npName := args[0]

			patch := map[string]interface{}{
				"spec": map[string]interface{}{
					"nodeCount": replicaCount,
				},
			}
			patchData, err := json.Marshal(patch)
			if err != nil {
				return fmt.Errorf("building patch: %w", err)
			}

			updated, err := client.NodePools().Patch(ctx, client.Namespace(), npName, patchData)
			if err != nil {
				return fmt.Errorf("scaling nodepool %s: %w", npName, err)
			}

			if output.ParseFormat(outputFmt) != output.FormatText {
				return printNodePool(cmd.OutOrStdout(), updated, outputFmt)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Nodepool %s scaled to %d replicas.\n", npName, replicaCount)
			return nil
		},
	}

	cmd.Flags().Int32Var(&replicaCount, "replicas", 0, "Number of replicas (required)")
	cmd.Flags().StringVarP(&outputFmt, "output", "o", "text", "Output format: text, json, yaml")
	return cmd
}
