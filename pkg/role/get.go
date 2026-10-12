package role

import (
	"github.com/openshift-online/gcp-hcp-ctl/pkg/internal/access"
	"github.com/spf13/cobra"
)

func newGetCmd() *cobra.Command {
	return &cobra.Command{Use: "get <name>", Short: "Show a Role and all permissions", Args: access.Named, RunE: func(cmd *cobra.Command, args []string) error {
		c := access.Client(cmd)
		role, err := c.Roles().Get(cmd.Context(), c.Namespace(), args[0])
		if err != nil {
			return err
		}
		return printRole(cmd, role)
	}}
}
