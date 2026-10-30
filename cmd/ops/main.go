// Standalone entry point for the ops plugin (gcphcpctl-ops).
// This can be built and distributed independently of the main gcphcpctl binary.
//
// Build with: make build-ops
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/config"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/httpdebug"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/ops"

	"github.com/spf13/cobra"
)

// newRootCmd keeps flag values and configuration local to each invocation.
func newRootCmd() *cobra.Command {
	var (
		project      string
		region       string
		outputFormat string
		configPath   string
		verbosity    int
	)
	root := ops.NewOpsCmd()
	root.Use = "gcphcpctl-ops"
	root.Short = "Operational commands for GCP HCP cluster debugging"
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load(configPath)
		if err != nil {
			return err
		}
		environment, err := config.FromEnvironment(func(name string) string {
			// Ignore even an invalid environment value when the flag overrides it.
			if name == "GCPHCPCTL_VERBOSITY" && cmd.Flags().Changed("v") {
				return ""
			}
			return os.Getenv(name)
		})
		if err != nil {
			return err
		}
		verbosity = config.Resolve(config.Config{Verbosity: verbosity}, environment, *cfg, false, cmd.Flags().Changed("v")).Verbosity
		if verbosity < 0 {
			return fmt.Errorf("--v must be a nonnegative integer")
		}
		var traceOutput io.Writer
		if verbosity >= 6 {
			traceOutput = cmd.ErrOrStderr()
		}
		// Always replace any inherited tracer before subcommand network calls.
		cmd.SetContext(httpdebug.WithTrace(cmd.Context(), traceOutput, verbosity))
		if project == "" && cfg.Project != "" {
			project = cfg.Project
		}
		if region == "" && cfg.Region != "" {
			region = cfg.Region
		}
		if !cmd.Flags().Changed("output") && cfg.Output != "" {
			outputFormat = cfg.Output
		}
		return ops.ValidateRequiredFlags(cmd)
	}

	root.PersistentFlags().StringVar(&project, "project", os.Getenv("GCPHCPCTL_PROJECT"), "GCP project ID (env: GCPHCPCTL_PROJECT)")
	root.PersistentFlags().StringVar(&region, "region", os.Getenv("GCPHCPCTL_REGION"), "GCP region (env: GCPHCPCTL_REGION)")
	root.PersistentFlags().StringVarP(&outputFormat, "output", "o", "text", "Output format: text, json, yaml")
	root.PersistentFlags().IntVarP(&verbosity, "v", "v", 0, "HTTP tracing to stderr (0-5: no HTTP tracing; 6: metadata; 7: request headers; 8: bodies and response headers; 9: curl/timing; 10: untruncated supported bodies; env: GCPHCPCTL_VERBOSITY)")
	root.PersistentFlags().StringVar(&configPath, "config", "", "Config file path (default: ~/.gcphcpctl/config.yaml)")

	root.SilenceUsage = true
	root.SilenceErrors = true
	return root
}

func main() {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
