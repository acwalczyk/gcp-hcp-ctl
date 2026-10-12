package rolebinding

import (
	"fmt"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/internal/access"
	"github.com/spf13/cobra"
)

func newDeleteCmd() *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{Use: "delete <name>", Short: "Delete only the named RoleBinding", Long: "Delete only the named RoleBinding. Independent bindings remain unchanged; this revokes only the named grant. Requires --confirm.", Args: access.Named, RunE: func(cmd *cobra.Command, args []string) error {
		if !confirm {
			return fmt.Errorf("--confirm is required to delete a RoleBinding")
		}
		c := access.Client(cmd)
		if err := c.RoleBindings().Delete(cmd.Context(), c.Namespace(), args[0]); err != nil {
			return err
		}
		return access.Deleted(cmd, "RoleBinding", args[0], c.Namespace())
	}}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "Confirm deletion of only this RoleBinding")
	return cmd
}
