package role

import (
	"github.com/openshift-online/gcp-hcp-ctl/pkg/internal/access"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/output"
	gcpv1 "github.com/openshift-online/gecko/platform-api/api/public/v1"
	"github.com/spf13/cobra"
	"strconv"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{Use: "list", Short: "List custom Roles in the selected project", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		c := access.Client(cmd)
		list, err := c.Roles().List(cmd.Context(), c.Namespace())
		if err != nil {
			return err
		}
		if list.Items == nil {
			list.Items = []gcpv1.Role{}
		}
		if access.Format(cmd) != output.FormatText {
			return access.Structured(cmd, list)
		}
		t := output.NewTable(cmd.OutOrStdout(), "NAME", "PERMISSIONS", "AGE")
		for _, r := range list.Items {
			t.AddRow(r.Name, strconv.Itoa(len(r.Spec.Permissions)), access.Age(r.CreationTimestamp))
		}
		return t.Flush()
	}}
}
