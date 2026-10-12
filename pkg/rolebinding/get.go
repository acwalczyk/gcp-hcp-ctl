package rolebinding

import (
	"github.com/openshift-online/gcp-hcp-ctl/pkg/internal/access"
	"github.com/spf13/cobra"
)

func newGetCmd() *cobra.Command {
	return &cobra.Command{Use: "get <name>", Short: "Show a binding and its complete role reference", Args: access.Named, RunE: func(cmd *cobra.Command, args []string) error {
		c := access.Client(cmd)
		role, err := c.RoleBindings().Get(cmd.Context(), c.Namespace(), args[0])
		if err != nil {
			return err
		}
		return printBinding(cmd, role)
	}}
}
