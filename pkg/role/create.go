package role

import (
	"github.com/openshift-online/gcp-hcp-ctl/pkg/internal/access"
	gcpv1 "github.com/openshift-online/gecko/platform-api/api/public/v1"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newCreateCmd() *cobra.Command {
	var permissions []string
	cmd := &cobra.Command{Use: "create <name>", Short: "Create a custom Role without overwriting an existing name", Args: access.Named,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validatePermissions(permissions); err != nil {
				return err
			}
			c := access.Client(cmd)
			role, err := c.Roles().Create(cmd.Context(), c.Namespace(), &gcpv1.Role{
				TypeMeta:   metav1.TypeMeta{APIVersion: gcpv1.GroupVersion.String(), Kind: "Role"},
				ObjectMeta: metav1.ObjectMeta{Name: args[0], Namespace: c.Namespace()}, Spec: gcpv1.RoleSpec{Permissions: permissions}})
			if err != nil {
				return err
			}
			if err = printRole(cmd, role); err != nil {
				return err
			}
			return access.Note(cmd)
		}}
	cmd.Flags().StringArrayVar(&permissions, "permission", nil, "Permission to grant (repeat for each permission; at least one required)")
	return cmd
}
