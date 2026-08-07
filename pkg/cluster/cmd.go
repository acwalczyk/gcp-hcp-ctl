package cluster

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type contextKey string

const clientKey contextKey = "platform-api-client"

// NewClusterCmd returns the "cluster" command group.
func NewClusterCmd() *cobra.Command {
	var clusterCmd *cobra.Command
	clusterCmd = &cobra.Command{
		Use:   "cluster",
		Short: "Manage GCP HCP clusters",
		Long:  `Create, get, list, delete, and log in to GCP HCP clusters via the platform API server.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if parent := clusterCmd.Parent(); parent != nil && parent.PersistentPreRunE != nil {
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

	clusterCmd.AddCommand(newCreateCmd())
	clusterCmd.AddCommand(newGetCmd())
	clusterCmd.AddCommand(newListCmd())
	clusterCmd.AddCommand(newDeleteCmd())
	clusterCmd.AddCommand(newLoginCmd())

	return clusterCmd
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

func printCluster(w io.Writer, c *gcpv1.Cluster, format string) error {
	switch output.ParseFormat(format) {
	case output.FormatJSON:
		return output.PrintJSON(w, c)
	case output.FormatYAML:
		return output.PrintYAML(w, c)
	default:
	}

	bw := bufio.NewWriter(w)

	fmt.Fprintf(bw, "Name:            %s\n", c.Name)
	fmt.Fprintf(bw, "ID:              %s\n", c.UID)
	if c.Spec.InfraID != "" {
		fmt.Fprintf(bw, "Infra ID:        %s\n", c.Spec.InfraID)
	}
	fmt.Fprintf(bw, "Status:          %s\n", clusterStatusDetail(c))
	if !c.CreationTimestamp.IsZero() {
		fmt.Fprintf(bw, "Created:         %s (%s)\n",
			c.CreationTimestamp.Format("2006-01-02T15:04:05Z"),
			output.Age(c.CreationTimestamp.Format("2006-01-02T15:04:05Z")))
	}

	if c.Spec.Release.Version != "" {
		ver := c.Spec.Release.Version
		if c.Spec.Release.ChannelGroup != "" {
			ver = fmt.Sprintf("%s (%s)", ver, c.Spec.Release.ChannelGroup)
		}
		fmt.Fprintf(bw, "Version:         %s\n", ver)
	}

	if hcr := c.Status.HostedClusterResult; hcr != nil {
		if hcr.APIEndpoint != "" {
			fmt.Fprintf(bw, "API Endpoint:    %s\n", hcr.APIEndpoint)
		}
		if hcr.Version != "" {
			fmt.Fprintf(bw, "HC Version:      %s\n", hcr.Version)
		}
	}

	if gcp := c.Spec.Platform.GCP; gcp != nil {
		fmt.Fprintln(bw, "\nPlatform:")
		fmt.Fprintf(bw, "  Provider:      GCP\n")
		fmt.Fprintf(bw, "  Project:       %s\n", gcp.ProjectID)
		fmt.Fprintf(bw, "  Region:        %s\n", gcp.Region)
		if gcp.EndpointAccess != "" {
			fmt.Fprintf(bw, "  Access:        %s\n", gcp.EndpointAccess)
		}
		if gcp.Network != "" {
			fmt.Fprintf(bw, "  Network:       %s\n", gcp.Network)
		}
		if gcp.Subnet != "" {
			fmt.Fprintf(bw, "  Subnet:        %s\n", gcp.Subnet)
		}
	}

	net := c.Spec.Networking
	if net.NetworkType != "" || len(net.ServiceNetwork) > 0 ||
		len(net.MachineNetwork) > 0 || len(net.ClusterNetwork) > 0 {
		fmt.Fprintln(bw, "\nNetworking:")
		if net.NetworkType != "" {
			fmt.Fprintf(bw, "  Type:          %s\n", net.NetworkType)
		}
		for _, mn := range net.MachineNetwork {
			fmt.Fprintf(bw, "  Machine CIDR:  %s\n", mn.CIDR)
		}
		for _, sn := range net.ServiceNetwork {
			fmt.Fprintf(bw, "  Service CIDR:  %s\n", sn)
		}
		for _, cn := range net.ClusterNetwork {
			fmt.Fprintf(bw, "  Cluster CIDR:  %s (/%d)\n", cn.CIDR, cn.HostPrefix)
		}
	}

	if len(c.Status.Conditions) > 0 {
		fmt.Fprintln(bw, "\nConditions:")
		t := output.NewTable(bw, "TYPE", "STATUS", "REASON", "MESSAGE", "LAST TRANSITION")
		for _, cond := range c.Status.Conditions {
			msg := cond.Message
			if len(msg) > 80 {
				msg = msg[:80] + "..."
			}
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

func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}

func clusterStatus(c *gcpv1.Cluster) string {
	phase, _ := deriveClusterStatus(c)
	return phase
}

func clusterStatusDetail(c *gcpv1.Cluster) string {
	phase, detail := deriveClusterStatus(c)
	if detail == "" {
		return phase
	}
	return fmt.Sprintf("%s (%s)", phase, detail)
}

func deriveClusterStatus(c *gcpv1.Cluster) (phase, detail string) {
	if c.DeletionTimestamp != nil {
		return "Deleting", ""
	}

	conditions := c.Status.Conditions
	if len(conditions) == 0 {
		return "Pending", ""
	}

	ready := findCondition(conditions, "Ready")

	if ready != nil && ready.Status == metav1.ConditionTrue {
		return "Ready", ""
	}

	if ready != nil && ready.Status == metav1.ConditionFalse {
		msg := ready.Message
		if len(msg) > 60 {
			msg = msg[:60] + "..."
		}
		if msg != "" {
			return "Progressing", msg
		}
		return "Progressing", ready.Reason
	}

	return "Progressing", ""
}

func releaseVersion(c *gcpv1.Cluster) string {
	if c.Spec.Release.Version != "" {
		return c.Spec.Release.Version
	}
	return "<none>"
}
