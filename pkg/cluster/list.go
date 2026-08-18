package cluster

import (
	"fmt"
	"time"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/output"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	var outputFmt string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all clusters",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client := clientFromCmd(cmd)
			out := cmd.OutOrStdout()

			clusterList, err := client.Clusters().List(cmd.Context(), client.Namespace())
			if err != nil {
				return fmt.Errorf("listing clusters: %w", err)
			}

			switch output.ParseFormat(outputFmt) {
			case output.FormatJSON:
				return output.PrintJSON(out, clusterList)
			case output.FormatYAML:
				return output.PrintYAML(out, clusterList)
			default:
			}

			if len(clusterList.Items) == 0 {
				_, err := fmt.Fprintln(out, "No clusters found.")
				return err
			}

			t := output.NewTable(out, "NAME", "PROJECT", "INFRA ID", "REGION", "VERSION", "ACCESS", "STATUS", "AGE")
			for _, c := range clusterList.Items {
				region := ""
				access := ""
				if c.Spec.Platform.GCP != nil {
					region = c.Spec.Platform.GCP.Region
					access = c.Spec.Platform.GCP.EndpointAccess
				}
				t.AddRow(
					c.Name,
					c.Namespace,
					c.Spec.InfraID,
					region,
					releaseVersion(&c),
					access,
					clusterStatus(&c),
					output.Age(c.CreationTimestamp.UTC().Format(time.RFC3339)),
				)
			}
			return t.Flush()
		},
	}

	cmd.Flags().StringVarP(&outputFmt, "output", "o", "text", "Output format: text, json, yaml")
	return cmd
}
