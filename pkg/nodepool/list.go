package nodepool

import (
	"fmt"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/output"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	var (
		outputFmt  string
		clusterRef string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List nodepools",
		Long: `List nodepools. Optionally filter by cluster with --cluster.

  gcphcpctl nodepool list
  gcphcpctl nodepool list --cluster my-cluster`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := clientFromCmd(cmd)
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			allNodePools, err := fetchNodePools(ctx, client, clusterRef)
			if err != nil {
				return err
			}

			switch output.ParseFormat(outputFmt) {
			case output.FormatJSON:
				return output.PrintJSON(out, allNodePools)
			case output.FormatYAML:
				return output.PrintYAML(out, allNodePools)
			default:
			}

			if len(allNodePools) == 0 {
				fmt.Fprintln(out, "No nodepools found.")
				return nil
			}

			t := output.NewTable(out, "NAME", "CLUSTER", "REPLICAS", "INSTANCE", "VERSION", "STATUS", "AGE")
			for _, np := range allNodePools {
				t.AddRow(
					np.Name,
					np.Spec.ClusterID,
					nodeCount(&np),
					machineType(&np),
					releaseVersion(&np),
					nodePoolStatus(&np),
					output.Age(np.CreationTimestamp.Format("2006-01-02T15:04:05Z")),
				)
			}
			return t.Flush()
		},
	}

	cmd.Flags().StringVarP(&outputFmt, "output", "o", "text", "Output format: text, json, yaml")
	cmd.Flags().StringVar(&clusterRef, "cluster", "", "Filter by cluster name")
	return cmd
}
