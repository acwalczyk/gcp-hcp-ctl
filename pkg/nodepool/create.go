package nodepool

import (
	"fmt"

	gcpv1 "github.com/openshift-online/gecko/platform-api/api/public/v1"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type createOptions struct {
	clusterRef   string
	replicas     int32
	instanceType string
	diskSize     int64
	diskType     string
	zone         string
	version      string
	channelGroup string
	outputFmt    string
}

func newCreateCmd() *cobra.Command {
	opts := &createOptions{}

	cmd := &cobra.Command{
		Use:   "create <nodepool-name>",
		Short: "Create a nodepool",
		Long: `Create a nodepool in a cluster via the platform API server.

  gcphcpctl nodepool create my-nodepool --cluster my-cluster --replicas 2
  gcphcpctl nodepool create workers --cluster my-cluster \
    --replicas 3 --instance-type n2-standard-8 --disk-size 200
  gcphcpctl nodepool create workers --cluster my-cluster \
    --replicas 2 --version 4.22.0-rc.5 --channel-group candidate`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("nodepool name is required\n\nUsage: %s", cmd.UseLine())
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return opts.run(cmd, args[0])
		},
	}

	cmd.Flags().StringVar(&opts.clusterRef, "cluster", "", "Cluster name (required)")
	cmd.Flags().Int32Var(&opts.replicas, "replicas", 2, "Number of replicas")
	cmd.Flags().StringVar(&opts.instanceType, "instance-type", "n2-standard-4", "GCE machine type")
	cmd.Flags().Int64Var(&opts.diskSize, "disk-size", 100, "Boot disk size in GB")
	cmd.Flags().StringVar(&opts.diskType, "disk-type", "pd-balanced", "Boot disk type: pd-standard, pd-ssd, pd-balanced")
	cmd.Flags().StringVar(&opts.zone, "zone", "", "GCP zone (defaults to cluster region + \"-a\")")
	cmd.Flags().StringVar(&opts.version, "version", "", "OCP version (e.g. 4.22.0-rc.5); defaults to cluster version")
	cmd.Flags().StringVar(&opts.channelGroup, "channel-group", "", "Channel group: stable, fast, candidate, eus; defaults to cluster channel")
	cmd.Flags().StringVarP(&opts.outputFmt, "output", "o", "text", "Output format: text, json, yaml")

	_ = cmd.MarkFlagRequired("cluster")

	return cmd
}

func (o *createOptions) run(cmd *cobra.Command, npName string) error {
	switch o.diskType {
	case "pd-standard", "pd-ssd", "pd-balanced":
	default:
		return fmt.Errorf("--disk-type must be one of: pd-standard, pd-ssd, pd-balanced")
	}
	if o.replicas < 0 {
		return fmt.Errorf("--replicas must be non-negative")
	}

	client := clientFromCmd(cmd)
	ctx := cmd.Context()
	ns := client.Namespace()

	nodePool := &gcpv1.NodePool{
		TypeMeta: metav1.TypeMeta{
			APIVersion: gcpv1.GroupVersion.String(),
			Kind:       "NodePool",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: npName,
		},
		Spec: gcpv1.NodePoolSpec{
			ClusterID: o.clusterRef,
			NodeCount: &o.replicas,
			Platform: gcpv1.NodePoolPlatformSpec{
				Type: "GCP",
				GCP: &gcpv1.GCPNodePoolPlatform{
					MachineType: o.instanceType,
					DiskSizeGB:  o.diskSize,
					DiskType:    o.diskType,
				},
			},
			Release: gcpv1.ReleaseSpec{
				Version:      o.version,
				ChannelGroup: o.channelGroup,
			},
		},
	}

	if o.zone != "" {
		nodePool.Spec.Platform.GCP.Zone = o.zone
	}

	created, err := client.NodePools().Create(ctx, ns, nodePool)
	if err != nil {
		return fmt.Errorf("creating nodepool: %w", err)
	}

	return printNodePool(cmd.OutOrStdout(), created, o.outputFmt)
}
