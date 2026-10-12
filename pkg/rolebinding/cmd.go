// Package rolebinding implements namespace-scoped user-email grants.
package rolebinding

import (
	"fmt"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/internal/access"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/output"
	gcpv1 "github.com/openshift-online/gecko/platform-api/api/public/v1"
	"github.com/spf13/cobra"
	"golang.org/x/text/unicode/norm"
	"strings"
)

func NewRoleBindingCmd() *cobra.Command {
	cmd := access.Group("rolebinding", "Manage named user-email grants in the selected project")
	cmd.AddCommand(newCreateCmd(), newListCmd(), newGetCmd(), newDeleteCmd())
	return cmd
}

// NormalizeEmail mirrors Gecko api/private/v1.NormalizeEmail (Gecko PR #336).
// Local-part case is preserved; only the domain is lowercased. This is not RFC-5322 parsing.
func NormalizeEmail(email string) (string, error) {
	email = norm.NFC.String(strings.TrimSpace(email))
	if email == "" {
		return "", fmt.Errorf("email must not be empty")
	}
	if strings.Count(email, "@") != 1 {
		return "", fmt.Errorf("email must contain exactly one @")
	}
	parts := strings.SplitN(email, "@", 2)
	if parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("email must contain a local part and domain")
	}
	return parts[0] + "@" + strings.ToLower(parts[1]), nil
}

func printBinding(cmd *cobra.Command, b *gcpv1.RoleBinding) error {
	if access.Format(cmd) != output.FormatText {
		return access.Structured(cmd, b)
	}
	_, err := fmt.Fprintf(cmd.OutOrStdout(), "Name: %s\nNamespace: %s\nAge: %s\nSubject: %s\nRole Kind: %s\nRole Name: %s\n", b.Name, b.Namespace, access.Age(b.CreationTimestamp), b.Spec.Subject, b.Spec.RoleRef.Kind, b.Spec.RoleRef.Name)
	return err
}
