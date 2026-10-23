package network

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

const DefaultSubnetCIDR = "10.0.0.0/24"

type CreateOptions struct {
	ProjectID  string
	Region     string
	InfraID    string
	VPCCidr    string
	OutputFile string

	// Version is the requested cluster version. Below 5.1.0 the CLI creates the
	// worker firewall rule; from 5.1.0 the control-plane-operator owns it and no
	// rule is created. Empty means unknown, which creates the rule.
	Version string
	// NetworkType selects the firewall ports; OVNKubernetes adds the Geneve
	// overlay port.
	NetworkType string
	// NodePortRange is the cluster's service node port range.
	NodePortRange string
	// NoFirewallRule suppresses firewall rule creation regardless of version.
	NoFirewallRule bool
}

type CreateOutput struct {
	Region           string `json:"region"`
	ProjectID        string `json:"projectId"`
	InfraID          string `json:"infraId"`
	NetworkName      string `json:"networkName"`
	NetworkSelfLink  string `json:"networkSelfLink"`
	SubnetName       string `json:"subnetName"`
	SubnetSelfLink   string `json:"subnetSelfLink"`
	SubnetCIDR       string `json:"subnetCidr"`
	RouterName       string `json:"routerName"`
	NATName          string `json:"natName"`
	FirewallRuleName string `json:"firewallRuleName"`
}

func NewCreateCommand() *cobra.Command {
	opts := &CreateOptions{
		VPCCidr:       DefaultSubnetCIDR,
		NetworkType:   DefaultNetworkType,
		NodePortRange: DefaultNodePortRange,
	}

	cmd := &cobra.Command{
		Use:   "create <infra-id>",
		Short: "Create GCP network infrastructure for a HyperShift cluster",
		Long: `Create network infrastructure including:
  - VPC network (custom subnet mode)
  - Worker firewall rule for worker-to-worker and kubelet traffic
  - Subnet with Private Google Access
  - Cloud Router
  - Cloud NAT

The worker firewall rule (<infra-id>-internal-cluster) matches the
control-plane-operator's desired state exactly, including its ownership marker.

The rule is created unless the cluster runs OpenShift 5.1.0 or later, where the
control-plane-operator owns it. Pass --version to apply that comparison; when
it is omitted the rule is created, which is safe because the operator adopts a
marked rule it finds. Use --no-firewall-rule to suppress creation entirely.

All operations are idempotent and safe to run multiple times.`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			opts.InfraID = args[0]
			var err error
			opts.ProjectID, err = cmd.Flags().GetString("project")
			if err != nil {
				return fmt.Errorf("failed to read --project flag: %w", err)
			}
			opts.Region, err = cmd.Flags().GetString("region")
			if err != nil {
				return fmt.Errorf("failed to read --region flag: %w", err)
			}
			return opts.ValidateInputs()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := logr.FromSlogHandler(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
			return opts.Run(cmd.Context(), logger)
		},
	}

	cmd.Flags().StringVar(&opts.VPCCidr, "vpc-cidr", opts.VPCCidr, "CIDR block for the subnet")
	cmd.Flags().StringVar(&opts.OutputFile, "output-file", "", "Path to output JSON file with resource details (default: stdout)")
	cmd.Flags().StringVar(&opts.Version, "version", "", "Target OCP version; below 5.1.0 the worker firewall rule is created, from 5.1.0 the control-plane-operator creates it (default: create the rule)")
	cmd.Flags().StringVar(&opts.NetworkType, "network-type", opts.NetworkType, "Cluster network type: OVNKubernetes or OpenShiftSDN")
	cmd.Flags().StringVar(&opts.NodePortRange, "node-port-range", opts.NodePortRange, "Service node port range for the worker firewall rule")
	cmd.Flags().BoolVar(&opts.NoFirewallRule, "no-firewall-rule", false, "Do not create the worker firewall rule; the control-plane-operator owns it")

	return cmd
}

func (o *CreateOptions) ValidateInputs() error {
	if o.InfraID == "" {
		return fmt.Errorf("infra-id is required")
	}
	if o.ProjectID == "" {
		return fmt.Errorf("project is required")
	}
	if o.Region == "" {
		return fmt.Errorf("region is required")
	}
	if o.VPCCidr != "" {
		if _, _, err := net.ParseCIDR(o.VPCCidr); err != nil {
			return fmt.Errorf("vpc-cidr must be a valid CIDR: %w", err)
		}
	}
	if !isValidVersion(o.Version) {
		return fmt.Errorf("version must be a semantic version such as 5.1.0 or 5.0.0-ec.6, got %q", o.Version)
	}
	if o.NetworkType != "" {
		if err := validateNetworkType(o.NetworkType); err != nil {
			return err
		}
	}
	if o.NodePortRange != "" {
		if err := validateNodePortRange(o.NodePortRange); err != nil {
			return err
		}
	}
	return nil
}

// applyDefaults fills in values omitted by programmatic callers (for example
// cluster create --setup-infra).
func (o *CreateOptions) applyDefaults() {
	if o.VPCCidr == "" {
		o.VPCCidr = DefaultSubnetCIDR
	}
	if o.NetworkType == "" {
		o.NetworkType = DefaultNetworkType
	}
	if o.NodePortRange == "" {
		o.NodePortRange = DefaultNodePortRange
	}
}

func (o *CreateOptions) Run(ctx context.Context, logger logr.Logger) error {
	result, err := o.CreateNetwork(ctx, logger)
	if err != nil {
		return err
	}

	if err := o.writeOutput(result, logger); err != nil {
		return err
	}
	logger.Info("Successfully created network infrastructure",
		"infraID", o.InfraID, "projectID", o.ProjectID, "region", o.Region)
	return nil
}

func (o *CreateOptions) CreateNetwork(ctx context.Context, logger logr.Logger) (*CreateOutput, error) {
	logger.Info("Creating GCP network infrastructure",
		"projectID", o.ProjectID, "region", o.Region, "infraID", o.InfraID)

	o.applyDefaults()

	mgr, err := NewManager(ctx, o.ProjectID, o.InfraID, o.Region, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize network manager: %w", err)
	}

	result := &CreateOutput{
		Region:    o.Region,
		ProjectID: o.ProjectID,
		InfraID:   o.InfraID,
	}

	network, err := mgr.CreateNetwork(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create VPC network: %w", err)
	}
	result.NetworkName = network.Name
	result.NetworkSelfLink = network.SelfLink

	if shouldCreateFirewallRule(o.Version, o.NoFirewallRule) {
		firewall, err := mgr.CreateFirewallRule(ctx, network.SelfLink, o.NetworkType, o.NodePortRange)
		if err != nil {
			return nil, fmt.Errorf("failed to create firewall rule: %w", err)
		}
		result.FirewallRuleName = firewall.Name
	} else {
		logger.Info("Skipping worker firewall rule; it is owned by the control-plane-operator",
			"name", mgr.formatFirewallName(), "version", o.Version)
	}

	subnet, err := mgr.CreateSubnet(ctx, network.SelfLink, o.VPCCidr)
	if err != nil {
		return nil, fmt.Errorf("failed to create subnet: %w", err)
	}
	result.SubnetName = subnet.Name
	result.SubnetSelfLink = subnet.SelfLink
	result.SubnetCIDR = subnet.IpCidrRange

	router, err := mgr.CreateRouter(ctx, network.SelfLink)
	if err != nil {
		return nil, fmt.Errorf("failed to create Cloud Router: %w", err)
	}
	result.RouterName = router.Name

	natName, err := mgr.CreateNAT(ctx, router.Name, subnet.SelfLink)
	if err != nil {
		return nil, fmt.Errorf("failed to create Cloud NAT: %w", err)
	}
	result.NATName = natName

	return result, nil
}

func (o *CreateOptions) writeOutput(result *CreateOutput, logger logr.Logger) error {
	out := os.Stdout
	if len(o.OutputFile) > 0 {
		var err error
		out, err = os.Create(o.OutputFile)
		if err != nil {
			return fmt.Errorf("cannot create output file: %w", err)
		}
		defer func(out *os.File) {
			if err := out.Close(); err != nil {
				logger.Error(err, "Failed to close output file", "file", o.OutputFile)
			}
		}(out)
	}
	outputBytes, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize result: %w", err)
	}
	_, err = out.Write(outputBytes)
	if err != nil {
		return fmt.Errorf("failed to write result: %w", err)
	}
	return nil
}
