package cluster

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/kubeconfig"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/platformapi"
	"github.com/spf13/cobra"
)

type validateAccessFunc func(ctx context.Context, kubeconfigPath, contextName string) (string, error)

type loginOptions struct {
	namespace        string
	kubeconfigPath   string
	insecureSkipTLS  bool
	validateAccessFn validateAccessFunc
}

func newLoginCmd() *cobra.Command {
	opts := &loginOptions{}

	cmd := &cobra.Command{
		Use:   "login <cluster-name>",
		Short: "Log in to a hosted cluster",
		Long: `Configures kubectl to access a hosted cluster by setting up a kubeconfig
context with gcloud exec-based authentication.

The command resolves the cluster's API endpoint from the platform API server
(via status.hostedClusterResult.apiEndpoint).`,
		Example: `  # Login using the cluster name
  gcphcpctl cluster login my-cluster

  # Write to a specific kubeconfig file
  gcphcpctl cluster login my-cluster --kubeconfig ~/.kube/hyperfleet`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLogin(cmd.Context(), cmd, args[0], opts)
		},
	}

	cmd.Flags().StringVar(&opts.namespace, "namespace", "default", "Default namespace for the context")
	cmd.Flags().StringVar(&opts.kubeconfigPath, "kubeconfig", "", "Path to kubeconfig file (defaults to $KUBECONFIG or ~/.kube/config)")
	cmd.Flags().BoolVar(&opts.insecureSkipTLS, "insecure-skip-tls-verify", false, "Skip TLS certificate verification")

	return cmd
}

func runLogin(ctx context.Context, cmd *cobra.Command, clusterRef string, opts *loginOptions) error {
	client := clientFromCmd(cmd)

	resolveCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	server, clusterName, err := resolveClusterEndpoint(resolveCtx, client, clusterRef)
	if err != nil {
		return fmt.Errorf("resolving cluster endpoint: %w", err)
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Cluster:    %s\n", clusterName)
	fmt.Fprintf(out, "API Server: %s\n", server)

	contextName, previousContext, kubeconfigPath, err := kubeconfig.Update(kubeconfig.UpdateOptions{
		ClusterName:     clusterName,
		Server:          server,
		Namespace:       opts.namespace,
		InsecureSkipTLS: opts.insecureSkipTLS,
		KubeconfigPath:  opts.kubeconfigPath,
	})
	if err != nil {
		return fmt.Errorf("updating kubeconfig: %w", err)
	}
	fmt.Fprintf(out, "Context:    %s\n", contextName)

	validateCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	validate := opts.validateAccessFn
	if validate == nil {
		validate = validateAccess
	}
	whoami, err := validate(validateCtx, kubeconfigPath, contextName)
	if err != nil {
		return handleValidationFailure(out, err, kubeconfigPath, contextName, previousContext)
	}
	fmt.Fprintf(out, "\n%s\n", whoami)
	fmt.Fprintf(out, "\nLogged in to %q successfully.\n", clusterName)
	return nil
}

func handleValidationFailure(out io.Writer, valErr error, kubeconfigPath, contextName, previousContext string) error {
	fmt.Fprintf(out, "\nWarning: access validation failed: %v\n", valErr)
	fmt.Fprintf(out, "  The kubeconfig entry was written but cluster access could not be verified.\n")
	fmt.Fprintf(out, "  You may need to check your RBAC permissions or gcloud authentication.\n")
	if previousContext != "" {
		if restoreErr := kubeconfig.RestoreContext(kubeconfigPath, previousContext); restoreErr != nil {
			fmt.Fprintf(out, "  Could not restore previous context %q: %v\n", previousContext, restoreErr)
		} else {
			fmt.Fprintf(out, "  Current context restored to %q.\n", previousContext)
		}
	}
	fmt.Fprintf(out, "  To switch manually: kubectl config use-context %s\n", contextName)
	return fmt.Errorf("access validation failed: %w", valErr)
}

// resolveClusterEndpoint gets the cluster from the platform-api-server and
// extracts the API endpoint from status.hostedClusterResult.apiEndpoint.
func resolveClusterEndpoint(ctx context.Context, client *platformapi.Client, clusterRef string) (string, string, error) {
	cluster, err := client.ResolveCluster(ctx, clusterRef)
	if err != nil {
		return "", "", err
	}

	clusterName := cluster.Name

	hcr := cluster.Status.HostedClusterResult
	if hcr == nil || hcr.APIEndpoint == "" {
		return "", clusterName, fmt.Errorf("no API endpoint found in cluster status (cluster may still be provisioning)")
	}

	parsed, err := url.Parse(hcr.APIEndpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return "", clusterName, fmt.Errorf("cluster returned invalid endpoint %q: must be a valid HTTPS URL", hcr.APIEndpoint)
	}

	return hcr.APIEndpoint, clusterName, nil
}

func validateAccess(ctx context.Context, kubeconfigPath, contextName string) (string, error) {
	args := []string{"auth", "whoami"}
	if kubeconfigPath != "" {
		args = append(args, "--kubeconfig", kubeconfigPath)
	}
	if contextName != "" {
		args = append(args, "--context", contextName)
	}

	cmd := exec.CommandContext(ctx, "kubectl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		output := strings.TrimSpace(string(out))
		if output != "" {
			return "", fmt.Errorf("kubectl auth whoami failed: %s", output)
		}
		return "", fmt.Errorf("kubectl auth whoami failed: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
