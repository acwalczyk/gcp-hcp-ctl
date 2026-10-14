package cli

import (
	"fmt"
	"os"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/cli/auth"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/cluster"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/config"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/infra/iam"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/infra/network"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/nodepool"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/ops"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/regions"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/role"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/rolebinding"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// newRootCmd builds a fresh root command so each invocation (and each test) is
// independent of global state.
func newRootCmd() *cobra.Command {
	// cfg is both the target the persistent flags bind to and, after
	// PersistentPreRunE resolves flags/env/file (plus optional discovery), the
	// effective configuration that every subcommand reads.
	cfg := &config.Config{}
	var configPath string

	root := &cobra.Command{
		Use:   "gcphcpctl",
		Short: "CLI for managing GCP Hosted Control Plane clusters",
		Long: `gcphcpctl is the unified CLI for managing GCP Hosted Control Plane (HCP) clusters.

It provides commands for cluster lifecycle, infrastructure management,
and operational debugging of hosted control plane clusters on GCP.

Configuration priority: CLI flags > environment variables > config file (~/.gcphcpctl/config.yaml).`,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return loadConfig(cmd, configPath, cfg)
		},
	}

	flags := root.PersistentFlags()
	flags.StringVar(&cfg.Project, "project", "", "GCP project ID (env: GCPHCPCTL_PROJECT)")
	flags.StringVar(&cfg.Region, "region", "", "GCP region (env: GCPHCPCTL_REGION)")
	flags.StringVar(&cfg.Environment, "environment", "", "Environment for endpoint discovery: integration, stage, production (or a discovery subdomain for dev/CI); alias: --env (env: GCPHCPCTL_ENVIRONMENT)")
	flags.StringVarP(&cfg.Output, "output", "o", "text", "Output format: text, json, yaml")
	flags.StringVar(&configPath, "config", "", "Config file path (default: ~/.gcphcpctl/config.yaml)")
	flags.StringVar(&cfg.APIEndpoint, "api-endpoint", "", "Explicit platform API endpoint; overrides discovery (env: GCPHCPCTL_API_ENDPOINT)")
	flags.StringVar(&cfg.OIDCEndpoint, "oidc-endpoint", "", "Explicit OIDC issuer base URL; overrides discovery (env: GCPHCPCTL_OIDC_ENDPOINT)")

	// Accept --env as an alias for --environment on the root and all subcommands.
	root.SetGlobalNormalizationFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		if name == "env" {
			name = "environment"
		}
		return pflag.NormalizedName(name)
	})

	root.AddCommand(
		auth.NewAuthCmd(),
		newConfigCmd(cfg, &configPath),
		ops.NewOpsCmd(),
		iam.NewIAMCmd(cfg),
		network.NewNetworkCmd(),
		role.NewRoleCmd(),
		rolebinding.NewRoleBindingCmd(),
		cluster.NewClusterCmd(cfg),
		nodepool.NewNodePoolCmd(cfg),
		regions.NewRegionsCmd(),
		newVersionCmd(),
		newCompletionCmd(root),
	)
	return root
}

// loadConfig resolves the effective configuration into cfg by merging CLI flags,
// environment variables, and the config file (in that priority). Endpoint
// discovery is not done here: commands that consume a service endpoint resolve it
// themselves (via discovery.Resolver.Region) so unrelated commands make no
// network call.
func loadConfig(cmd *cobra.Command, configPath string, cfg *config.Config) error {
	file, err := config.Load(configPath)
	if err != nil {
		return err
	}
	// Publish the merged values: cobra's inherited flags and every subcommand read
	// cfg after this returns.
	*cfg = config.Resolve(*cfg, config.FromEnvironment(os.Getenv), *file, cmd.Flags().Changed("output"))
	return nil
}

// Execute runs a fresh root command, printing any error to stderr. The error is
// returned so main can set a non-zero exit code.
func Execute() error {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	return nil
}
