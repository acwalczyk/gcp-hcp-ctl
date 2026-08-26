package cli

import (
	"fmt"
	"os"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/cluster"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/config"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/infra/iam"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/infra/network"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/nodepool"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/ops"

	"github.com/spf13/cobra"
)

var (
	project      string
	region       string
	outputFormat string
	configPath   string
	apiEndpoint  string
	oidcEndpoint string
)

var rootCmd = &cobra.Command{
	Use:   "gcphcpctl",
	Short: "CLI for managing GCP Hosted Control Plane clusters",
	Long: `gcphcpctl is the unified CLI for managing GCP Hosted Control Plane (HCP) clusters.

It provides commands for cluster lifecycle, infrastructure management,
and operational debugging of hosted control plane clusters on GCP.

Configuration priority: CLI flags > environment variables > config file (~/.gcphcpctl/config.yaml).`,
	SilenceUsage:  true,
	SilenceErrors: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return loadConfig(cmd)
	},
}

func loadConfig(cmd *cobra.Command) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	if project == "" && cfg.Project != "" {
		project = cfg.Project
	}
	if region == "" && cfg.Region != "" {
		region = cfg.Region
	}
	if !cmd.Flags().Changed("output") && cfg.Output != "" {
		outputFormat = cfg.Output
	}
	if !cmd.Flags().Changed("api-endpoint") && apiEndpoint == "" && cfg.APIEndpoint != "" {
		apiEndpoint = cfg.APIEndpoint
	}
	if !cmd.Flags().Changed("oidc-endpoint") && oidcEndpoint == "" && cfg.OIDCEndpoint != "" {
		oidcEndpoint = cfg.OIDCEndpoint
	}

	return nil
}

func init() {
	rootCmd.PersistentFlags().StringVar(&project, "project", os.Getenv("GCPHCPCTL_PROJECT"), "GCP project ID (env: GCPHCPCTL_PROJECT)")
	rootCmd.PersistentFlags().StringVar(&region, "region", os.Getenv("GCPHCPCTL_REGION"), "GCP region (env: GCPHCPCTL_REGION)")
	rootCmd.PersistentFlags().StringVarP(&outputFormat, "output", "o", "text", "Output format: text, json, yaml")
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "Config file path (default: ~/.gcphcpctl/config.yaml)")
	rootCmd.PersistentFlags().StringVar(&apiEndpoint, "api-endpoint", os.Getenv("GCPHCPCTL_API_ENDPOINT"), "Platform API endpoint URL (env: GCPHCPCTL_API_ENDPOINT)")
	rootCmd.PersistentFlags().StringVar(&oidcEndpoint, "oidc-endpoint", os.Getenv("GCPHCPCTL_OIDC_ENDPOINT"), "OIDC issuer base URL (env: GCPHCPCTL_OIDC_ENDPOINT)")

	rootCmd.AddCommand(newConfigCmd())
	rootCmd.AddCommand(ops.NewOpsCmd())
	rootCmd.AddCommand(iam.NewIAMCmd())
	rootCmd.AddCommand(network.NewNetworkCmd())
	rootCmd.AddCommand(cluster.NewClusterCmd())
	rootCmd.AddCommand(nodepool.NewNodePoolCmd())
}

// Execute runs the root command.
func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	return nil
}
