package role

import (
	"github.com/openshift-online/gcp-hcp-ctl/pkg/internal/access"
	"github.com/spf13/cobra"
)

func newUpdateCmd() *cobra.Command {
	var permissions []string
	cmd := &cobra.Command{Use: "update <name>", Short: "Replace the entire permission set of an existing Role",
		Long: "Replace the entire permission set using PUT. Omitted permissions are removed. Existing bindings remain unchanged. Conflicts are not retried.", Args: access.Named,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validatePermissions(permissions); err != nil {
				return err
			}
			c := access.Client(cmd)
			role, err := c.Roles().Get(cmd.Context(), c.Namespace(), args[0])
			if err != nil {
				return err
			}
			role.Spec.Permissions = permissions
			// Scope the mutation to the selected project, never to returned metadata.
			role.Name = args[0]
			role.Namespace = c.Namespace()
			role, err = c.Roles().Update(cmd.Context(), c.Namespace(), args[0], role)
			if err != nil {
				return err
			}
			if err = printRole(cmd, role); err != nil {
				return err
			}
			return access.Note(cmd)
		}}
	cmd.Flags().StringArrayVar(&permissions, "permission", nil, "Complete desired permission set (repeat; omitted permissions are removed)")
	return cmd
}
