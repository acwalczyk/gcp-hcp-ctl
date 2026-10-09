package iam

import (
	"fmt"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/config"
	"github.com/spf13/cobra"
)

// NewIAMCmd returns the parent "iam" command with create/destroy subcommands.
// effective is the resolved (merged) configuration; `iam create` uses it to
// discover the OIDC issuer from --env/--region, or to read an explicitly
// configured one.
func NewIAMCmd(effective *config.Config) *cobra.Command {
	var iamCmd *cobra.Command
	iamCmd = &cobra.Command{
		Use:   "iam",
		Short: "Manage GCP IAM infrastructure for HyperShift clusters",
		Long: `Create and destroy Workload Identity Federation (WIF) infrastructure
including WIF pools, OIDC providers, and Google Service Accounts
with IAM role bindings.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if parent := iamCmd.Parent(); parent != nil && parent.PersistentPreRunE != nil {
				if err := parent.PersistentPreRunE(cmd, args); err != nil {
					return err
				}
			}
			return validateRequiredFlags(cmd)
		},
	}

	iamCmd.AddCommand(NewCreateCommand(effective))
	iamCmd.AddCommand(NewDestroyCommand())

	return iamCmd
}

func validateRequiredFlags(cmd *cobra.Command) error {
	project, _ := cmd.Flags().GetString("project")
	if project == "" {
		return fmt.Errorf("--project is required (or set GCPHCPCTL_PROJECT)")
	}
	return nil
}
