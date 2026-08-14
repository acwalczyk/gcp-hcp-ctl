package nodepool

import (
	"bufio"
	"context"
	"fmt"
	"io"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/auth"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/output"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/platformapi"
	gcpv1 "github.com/openshift-online/gecko/platform-api/api/public/v1"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type contextKey string

const clientKey contextKey = "platform-api-client"

// NewNodePoolCmd returns the "nodepool" command group.
func NewNodePoolCmd() *cobra.Command {
	var npCmd *cobra.Command
	npCmd = &cobra.Command{
		Use:          "nodepool",
		Short:        "Manage nodepools",
		Long:         `Create, get, list, delete, and scale nodepools via the platform API server.`,
		SilenceUsage: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if parent := npCmd.Parent(); parent != nil && parent.PersistentPreRunE != nil {
				if err := parent.PersistentPreRunE(cmd, args); err != nil {
					return err
				}
			}
			if err := validateRequiredFlags(cmd); err != nil {
				return err
			}
			apiEndpoint, _ := cmd.Flags().GetString("api-endpoint")
			project, _ := cmd.Flags().GetString("project")
			client, err := newClient(apiEndpoint, project)
			if err != nil {
				return err
			}
			cmd.SetContext(context.WithValue(cmd.Context(), clientKey, client))
			return nil
		},
	}

	npCmd.AddCommand(newCreateCmd())
	npCmd.AddCommand(newGetCmd())
	npCmd.AddCommand(newListCmd())
	npCmd.AddCommand(newDeleteCmd())
	npCmd.AddCommand(newScaleCmd())

	return npCmd
}

func validateRequiredFlags(cmd *cobra.Command) error {
	apiEndpoint, _ := cmd.Flags().GetString("api-endpoint")
	if apiEndpoint == "" {
		return fmt.Errorf("--api-endpoint is required (or set GCPHCPCTL_API_ENDPOINT or api_endpoint in config)")
	}
	return nil
}

func newClient(apiEndpoint, project string) (*platformapi.Client, error) {
	return platformapi.NewClient(apiEndpoint, project, auth.NewTokenSource())
}

func clientFromCmd(cmd *cobra.Command) *platformapi.Client {
	client, ok := cmd.Context().Value(clientKey).(*platformapi.Client)
	if !ok {
		panic("bug: clientFromCmd called before PersistentPreRunE set the platform API client")
	}
	return client
}

// fetchNodePools retrieves nodepools, optionally filtered by cluster name.
// It fetches the full namespace list in a single call (no continue-token
// pagination), which is sufficient for the per-project scale of the platform API.
func fetchNodePools(ctx context.Context, client *platformapi.Client, clusterName string) ([]gcpv1.NodePool, error) {
	list, err := client.NodePools().List(ctx, client.Namespace())
	if err != nil {
		return nil, fmt.Errorf("listing nodepools: %w", err)
	}
	return filterNodePoolsByCluster(list.Items, clusterName), nil
}

// truncateString truncates a string to maxRunes Unicode code points.
// Uses rune-based slicing to avoid splitting multi-byte UTF-8 characters.
func truncateString(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

// filterNodePoolsByCluster returns pools whose Spec.ClusterID matches
// clusterName. An empty clusterName returns all pools unchanged.
func filterNodePoolsByCluster(pools []gcpv1.NodePool, clusterName string) []gcpv1.NodePool {
	if clusterName == "" {
		return pools
	}
	filtered := make([]gcpv1.NodePool, 0, len(pools))
	for _, np := range pools {
		if np.Spec.ClusterID == clusterName {
			filtered = append(filtered, np)
		}
	}
	return filtered
}

func printNodePool(w io.Writer, np *gcpv1.NodePool, format string) error {
	switch output.ParseFormat(format) {
	case output.FormatJSON:
		return output.PrintJSON(w, np)
	case output.FormatYAML:
		return output.PrintYAML(w, np)
	default:
	}

	bw := bufio.NewWriter(w)

	fmt.Fprintf(bw, "Name:         %s\n", np.Name)
	fmt.Fprintf(bw, "ID:           %s\n", np.UID)
	fmt.Fprintf(bw, "Cluster:      %s\n", np.Spec.ClusterID)
	fmt.Fprintf(bw, "Generation:   %d\n", np.Generation)
	if np.Spec.NodeCount != nil {
		fmt.Fprintf(bw, "Replicas:     %d\n", *np.Spec.NodeCount)
	}
	if np.Spec.Platform.GCP != nil {
		if np.Spec.Platform.GCP.MachineType != "" {
			fmt.Fprintf(bw, "Instance:     %s\n", np.Spec.Platform.GCP.MachineType)
		}
		if np.Spec.Platform.GCP.DiskSizeGB > 0 {
			fmt.Fprintf(bw, "Disk Size:    %d GB\n", np.Spec.Platform.GCP.DiskSizeGB)
		}
		if np.Spec.Platform.GCP.DiskType != "" {
			fmt.Fprintf(bw, "Disk Type:    %s\n", np.Spec.Platform.GCP.DiskType)
		}
		if np.Spec.Platform.GCP.Zone != "" {
			fmt.Fprintf(bw, "Zone:         %s\n", np.Spec.Platform.GCP.Zone)
		}
	}
	if np.Spec.Release.Version != "" {
		fmt.Fprintf(bw, "Version:      %s\n", np.Spec.Release.Version)
	}
	fmt.Fprintf(bw, "Status:       %s\n", nodePoolStatusDetail(np))
	if !np.CreationTimestamp.IsZero() {
		fmt.Fprintf(bw, "CreatedAt:    %s\n", np.CreationTimestamp.Format("2006-01-02T15:04:05Z"))
	}

	if np.DeletionTimestamp != nil {
		fmt.Fprintf(bw, "DeletedAt:    %s\n", np.DeletionTimestamp.Format("2006-01-02T15:04:05Z"))
	}

	if len(np.Status.Conditions) > 0 {
		fmt.Fprintln(bw, "\nConditions:")
		t := output.NewTable(bw, "TYPE", "STATUS", "REASON", "MESSAGE", "LAST TRANSITION")
		for _, cond := range np.Status.Conditions {
			msg := truncateString(cond.Message, 80)
			t.AddRow(
				cond.Type,
				string(cond.Status),
				cond.Reason,
				msg,
				cond.LastTransitionTime.Format("2006-01-02T15:04:05Z"),
			)
		}
		if err := t.Flush(); err != nil {
			return err
		}
	}

	return bw.Flush()
}

// nodePoolStatus returns a short human-friendly phase for table/list output.
func nodePoolStatus(np *gcpv1.NodePool) string {
	phase, _ := deriveNodePoolStatus(np)
	return phase
}

// nodePoolStatusDetail returns a phase with parenthetical explanation for get output.
func nodePoolStatusDetail(np *gcpv1.NodePool) string {
	phase, detail := deriveNodePoolStatus(np)
	if detail == "" {
		return phase
	}
	return fmt.Sprintf("%s (%s)", phase, detail)
}

func deriveNodePoolStatus(np *gcpv1.NodePool) (phase, detail string) {
	if np.DeletionTimestamp != nil {
		return "Deleting", ""
	}

	conditions := np.Status.Conditions
	if len(conditions) == 0 {
		return "Pending", ""
	}

	reconciled := meta.FindStatusCondition(conditions, "Reconciled")
	lastKnown := meta.FindStatusCondition(conditions, "LastKnownReconciled")

	if reconciled != nil && reconciled.Status == metav1.ConditionTrue {
		return "Ready", ""
	}

	if reconciled != nil && reconciled.Status == metav1.ConditionFalse {
		if lastKnown != nil && lastKnown.Status == metav1.ConditionTrue {
			return "Degraded", npConditionSummary(reconciled, np.Generation)
		}
		return "Progressing", ""
	}

	return "Progressing", ""
}

func npConditionSummary(cond *metav1.Condition, generation int64) string {
	if cond.ObservedGeneration < generation && cond.ObservedGeneration > 0 {
		return fmt.Sprintf("adapters finalizing generation %d", generation)
	}

	if cond.Message != "" {
		return truncateString(cond.Message, 60)
	}
	return cond.Reason
}

func releaseVersion(np *gcpv1.NodePool) string {
	if np.Spec.Release.Version != "" {
		return np.Spec.Release.Version
	}
	return "<none>"
}

func nodeCount(np *gcpv1.NodePool) string {
	if np.Spec.NodeCount != nil {
		return fmt.Sprintf("%d", *np.Spec.NodeCount)
	}
	return "-"
}

func machineType(np *gcpv1.NodePool) string {
	if np.Spec.Platform.GCP != nil && np.Spec.Platform.GCP.MachineType != "" {
		return np.Spec.Platform.GCP.MachineType
	}
	return "-"
}
