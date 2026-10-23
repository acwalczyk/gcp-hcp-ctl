package network

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
)

type DestroyOptions struct {
	ProjectID   string
	Region      string
	InfraID     string
	SkipConfirm bool
}

func NewDestroyCommand() *cobra.Command {
	opts := &DestroyOptions{}

	cmd := &cobra.Command{
		Use:   "destroy <infra-id>",
		Short: "Destroy GCP network infrastructure for a HyperShift cluster",
		Long: `Destroy all network resources in reverse dependency order:
  1. Cloud NAT (removed from router)
  2. Cloud Router
  3. Subnet
  4. Worker firewall rules: <infra-id>-internal-cluster and the legacy
     <infra-id>-allow-kubelet
  5. VPC network

Both firewall rules are removed before the VPC network, which GCP refuses to
delete while a rule still references it. Delete the HostedCluster first so the
control-plane-operator has released the managed rule before the network is torn
down.

All delete operations tolerate not-found errors (idempotent).`,
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

			if !opts.SkipConfirm {
				confirmed, err := confirmDestroy(opts.InfraID, opts.ProjectID, opts.Region)
				if err != nil {
					return fmt.Errorf("failed to read destroy confirmation: %w", err)
				}
				if !confirmed {
					logger.Info("Destroy cancelled by user")
					return nil
				}
			}

			if err := opts.Run(cmd.Context(), logger); err != nil {
				return err
			}
			logger.Info("Successfully destroyed GCP network infrastructure")
			return nil
		},
	}

	cmd.Flags().BoolVar(&opts.SkipConfirm, "yes", false, "Skip confirmation prompt")

	return cmd
}

func (o *DestroyOptions) ValidateInputs() error {
	if o.InfraID == "" {
		return fmt.Errorf("infra-id is required")
	}
	if o.ProjectID == "" {
		return fmt.Errorf("project is required")
	}
	if o.Region == "" {
		return fmt.Errorf("region is required")
	}
	return nil
}

func (o *DestroyOptions) Run(ctx context.Context, logger logr.Logger) error {
	return o.DestroyNetwork(ctx, logger)
}

func (o *DestroyOptions) DestroyNetwork(ctx context.Context, logger logr.Logger) error {
	logger.Info("Destroying GCP network infrastructure",
		"projectID", o.ProjectID, "region", o.Region, "infraID", o.InfraID)

	mgr, err := NewManager(ctx, o.ProjectID, o.InfraID, o.Region, logger)
	if err != nil {
		return fmt.Errorf("failed to initialize network manager: %w", err)
	}

	if err := mgr.DeleteNAT(ctx); err != nil {
		return fmt.Errorf("failed to delete Cloud NAT: %w", err)
	}

	if err := mgr.DeleteRouter(ctx); err != nil {
		return fmt.Errorf("failed to delete Cloud Router: %w", err)
	}

	if err := mgr.DeleteSubnet(ctx); err != nil {
		return fmt.Errorf("failed to delete subnet: %w", err)
	}

	if err := mgr.DeleteFirewallRule(ctx); err != nil {
		return fmt.Errorf("failed to delete firewall rule: %w", err)
	}

	if err := mgr.DeleteNetwork(ctx); err != nil {
		return fmt.Errorf("failed to delete VPC network: %w", err)
	}

	return nil
}

func confirmDestroy(infraID, projectID, region string) (bool, error) {
	fmt.Fprintf(os.Stderr, "This will destroy all network resources for infra-id %q in project %q region %q.\n", infraID, projectID, region)
	fmt.Fprintf(os.Stderr, "  - Cloud NAT\n")
	fmt.Fprintf(os.Stderr, "  - Cloud Router\n")
	fmt.Fprintf(os.Stderr, "  - Subnet\n")
	fmt.Fprintf(os.Stderr, "  - Firewall rules (%s-internal-cluster, legacy %s-allow-kubelet)\n", infraID, infraID)
	fmt.Fprintf(os.Stderr, "  - VPC network\n")
	fmt.Fprintf(os.Stderr, "\nAre you sure? [y/N]: ")

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("reading confirmation input: %w", err)
	}

	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes", nil
}
