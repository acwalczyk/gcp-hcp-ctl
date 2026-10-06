package version

import (
	"context"
	"fmt"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/auth"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/platformapi"
	"github.com/spf13/cobra"
)

type contextKey string

const clientKey contextKey = "platform-api-client"

// NewVersionCmd returns the "versions" command group.
func NewVersionCmd() *cobra.Command {
	var versionsCmd *cobra.Command
	versionsCmd = &cobra.Command{
		Use:   "versions",
		Short: "View supported OpenShift versions",
		Long:  "Get and list OpenShift versions supported by GCP HCP.",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if parent := versionsCmd.Parent(); parent != nil && parent.PersistentPreRunE != nil {
				if err := parent.PersistentPreRunE(cmd, args); err != nil {
					return err
				}
			}

			apiEndpoint, err := cmd.Flags().GetString("api-endpoint")
			if err != nil {
				return fmt.Errorf("reading --api-endpoint: %w", err)
			}
			if apiEndpoint == "" {
				return fmt.Errorf("--api-endpoint is required (or set GCPHCPCTL_API_ENDPOINT or api_endpoint in config)")
			}
			project, err := cmd.Flags().GetString("project")
			if err != nil {
				return fmt.Errorf("reading --project: %w", err)
			}
			client, err := newClient(apiEndpoint, project)
			if err != nil {
				return err
			}
			cmd.SetContext(context.WithValue(cmd.Context(), clientKey, client))
			return nil
		},
	}

	versionsCmd.AddCommand(newGetCmd())
	versionsCmd.AddCommand(newListCmd())
	return versionsCmd
}

func newClient(apiEndpoint, project string) (*platformapi.Client, error) {
	return platformapi.NewClient(apiEndpoint, project, auth.NewTokenSource(auth.PlatformAPIAudience))
}

func clientFromCmd(cmd *cobra.Command) *platformapi.Client {
	client, ok := cmd.Context().Value(clientKey).(*platformapi.Client)
	if !ok {
		panic("bug: clientFromCmd called before PersistentPreRunE set the platform API client")
	}
	return client
}
