package rolebinding

import (
	"fmt"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/internal/access"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/output"
	gcpv1 "github.com/openshift-online/gecko/platform-api/api/public/v1"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	var subject string
	cmd := &cobra.Command{Use: "list", Short: "Audit grants, optionally filtering by user email", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		var email string
		var err error
		filter := cmd.Flags().Changed("subject")
		if filter {
			email, err = NormalizeEmail(subject)
			if err != nil {
				return fmt.Errorf("invalid --subject: %w", err)
			}
		}
		c := access.Client(cmd)
		list, err := c.RoleBindings().List(cmd.Context(), c.Namespace())
		if err != nil {
			return err
		}
		items := make([]gcpv1.RoleBinding, 0, len(list.Items))
		for _, b := range list.Items {
			if !filter || b.Spec.Subject == email {
				items = append(items, b)
			}
		}
		list.Items = items
		if access.Format(cmd) != output.FormatText {
			return access.Structured(cmd, list)
		}
		t := output.NewTable(cmd.OutOrStdout(), "NAME", "SUBJECT", "ROLE KIND", "ROLE NAME", "AGE")
		for _, b := range list.Items {
			t.AddRow(b.Name, b.Spec.Subject, b.Spec.RoleRef.Kind, b.Spec.RoleRef.Name, access.Age(b.CreationTimestamp))
		}
		return t.Flush()
	}}
	cmd.Flags().StringVar(&subject, "subject", "", "Filter by normalized user email (local-part case preserved)")
	return cmd
}
