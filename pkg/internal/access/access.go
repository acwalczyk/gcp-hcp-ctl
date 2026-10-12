// Package access contains shared wiring and output for public access-management commands.
package access

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/auth"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/output"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/platformapi"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

type clientKey struct{}

// WithClient injects a credential-free client for command tests.
func WithClient(ctx context.Context, client *platformapi.Client) context.Context {
	return context.WithValue(ctx, clientKey{}, client)
}

// Group sets up inherited configuration and authentication without depending on pkg/cli.
func Group(name, short string) *cobra.Command {
	group := &cobra.Command{Use: name, Short: short}
	group.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if parent := group.Parent(); parent != nil && parent.PersistentPreRunE != nil {
			if err := parent.PersistentPreRunE(cmd, args); err != nil {
				return err
			}
		}
		project, _ := cmd.Flags().GetString("project")
		endpoint, _ := cmd.Flags().GetString("api-endpoint")
		if strings.TrimSpace(project) == "" {
			return fmt.Errorf("--project is required (or set GCPHCPCTL_PROJECT or project in config)")
		}
		if strings.TrimSpace(endpoint) == "" {
			return fmt.Errorf("--api-endpoint is required (or set GCPHCPCTL_API_ENDPOINT or api_endpoint in config)")
		}
		if Client(cmd) != nil {
			return nil
		}
		client, err := platformapi.NewClient(endpoint, project, auth.NewTokenSource(auth.PlatformAPIAudience))
		if err != nil {
			return err
		}
		cmd.SetContext(WithClient(cmd.Context(), client))
		return nil
	}
	return group
}

func Client(cmd *cobra.Command) *platformapi.Client {
	client, _ := cmd.Context().Value(clientKey{}).(*platformapi.Client)
	return client
}

func Named(cmd *cobra.Command, args []string) error {
	if err := cobra.ExactArgs(1)(cmd, args); err != nil {
		return err
	}
	if strings.TrimSpace(args[0]) == "" {
		return fmt.Errorf("name must not be empty")
	}
	return nil
}

func Format(cmd *cobra.Command) output.Format {
	format, _ := cmd.Flags().GetString("output")
	return output.ParseFormat(format)
}

// Structured preserves the Kubernetes JSON schema in both JSON and YAML.
func Structured(cmd *cobra.Command, obj any) error {
	if Format(cmd) == output.FormatJSON {
		return output.PrintJSON(cmd.OutOrStdout(), obj)
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	data, err = yaml.JSONToYAML(data)
	if err != nil {
		return err
	}
	_, err = cmd.OutOrStdout().Write(data)
	return err
}

func Note(cmd *cobra.Command) error {
	w := cmd.OutOrStdout()
	if Format(cmd) != output.FormatText {
		w = cmd.ErrOrStderr()
	}
	_, err := fmt.Fprintln(w, "Note: authorization changes may take a moment to propagate globally.")
	return err
}

func Deleted(cmd *cobra.Command, kind, name, namespace string) error {
	var err error
	if Format(cmd) == output.FormatText {
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "Deletion accepted for %s %q in namespace %q.\n", kind, name, namespace)
	} else {
		err = Structured(cmd, struct {
			Kind      string `json:"kind"`
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
			Accepted  bool   `json:"accepted"`
		}{kind, name, namespace, true})
	}
	if err != nil {
		return err
	}
	return Note(cmd)
}

func Age(t metav1.Time) string {
	if t.IsZero() {
		return "<unknown>"
	}
	return output.Age(t.UTC().Format(time.RFC3339))
}
