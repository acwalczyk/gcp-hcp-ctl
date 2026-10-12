// Package role implements public custom Role management.
package role

import (
	"fmt"
	"strings"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/internal/access"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/output"
	gcpv1 "github.com/openshift-online/gecko/platform-api/api/public/v1"
	"github.com/spf13/cobra"
)

func NewRoleCmd() *cobra.Command {
	cmd := access.Group("role", "Manage namespace-scoped custom permission bundles")
	cmd.AddCommand(newCreateCmd(), newUpdateCmd(), newListCmd(), newGetCmd(), newDeleteCmd())
	return cmd
}

func validatePermissions(permissions []string) error {
	if len(permissions) == 0 {
		return fmt.Errorf("at least one --permission is required")
	}
	seen := map[string]bool{}
	for _, p := range permissions {
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("permission must not be empty")
		}
		if seen[p] {
			return fmt.Errorf("duplicate permission %q", p)
		}
		seen[p] = true
	}
	return nil
}

func printRole(cmd *cobra.Command, role *gcpv1.Role) error {
	if access.Format(cmd) != output.FormatText {
		return access.Structured(cmd, role)
	}
	// Keep the default view operator-focused; structured output retains metadata.
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Name: %s\nNamespace: %s\nAge: %s\n", role.Name, role.Namespace, access.Age(role.CreationTimestamp))
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), "Permissions:")
	if err != nil {
		return err
	}
	for _, p := range role.Spec.Permissions {
		if _, err = fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", p); err != nil {
			return err
		}
	}
	return nil
}
