package iam

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/go-logr/logr"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/config"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/discovery"
	"github.com/spf13/cobra"
)

type CreateOptions struct {
	ProjectID           string
	InfraID             string
	ClusterOIDCJWKSFile string
	OutputFile          string
	OIDCIssuerURL       string
}

type CreateOutput struct {
	ProjectID            string                 `json:"projectId"`
	ProjectNumber        string                 `json:"projectNumber"`
	InfraID              string                 `json:"infraId"`
	WorkloadIdentityPool WorkloadIdentityConfig `json:"workloadIdentityPool"`
	ServiceAccounts      map[string]string      `json:"serviceAccounts"`
}

type WorkloadIdentityConfig struct {
	PoolID     string `json:"poolId"`
	ProviderID string `json:"providerId"`
	Audience   string `json:"audience"`
}

// NewCreateCommand returns the "iam create" command. Unless the caller passes a
// complete --oidc-issuer-url or an --oidc-jwks-file, the OIDC issuer is derived
// from effective: discovered from --env/--region, or an explicit endpoint.
func NewCreateCommand(effective *config.Config) *cobra.Command {
	opts := &CreateOptions{}

	cmd := &cobra.Command{
		Use:   "create <infra-id>",
		Short: "Create GCP IAM infrastructure for a HyperShift cluster",
		Long: `Create Workload Identity Federation (WIF) infrastructure including:
  - Workload Identity Pool
  - OIDC Provider
  - 6 Google Service Accounts with IAM roles and WIF bindings

All operations are idempotent and safe to run multiple times.`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			opts.InfraID = args[0]
			opts.ProjectID, _ = cmd.Flags().GetString("project")

			// Derive the issuer URL from the OIDC endpoint only when the caller
			// hasn't supplied a complete issuer URL or a JWKS file. The endpoint is
			// discovered from --env/--region, or taken from an explicit flag,
			// environment variable, or config value.
			if opts.OIDCIssuerURL == "" && opts.ClusterOIDCJWKSFile == "" {
				info, err := discovery.NewResolver().Region(cmd.Context(), effective)
				if err != nil {
					return err
				}
				if info.OIDCIssuer == "" {
					return discovery.EndpointRequiredError(discovery.KindOIDC, effective.Environment)
				}
				opts.OIDCIssuerURL = fmt.Sprintf("%s/%s", strings.TrimRight(info.OIDCIssuer, "/"), opts.InfraID)
			}

			return opts.ValidateInputs()
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := logr.FromSlogHandler(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
			return opts.Run(cmd.Context(), logger)
		},
	}

	cmd.Flags().StringVar(&opts.ClusterOIDCJWKSFile, "oidc-jwks-file", "", "Path to a local JSON file containing OIDC provider's public key in JWKS format")
	cmd.Flags().StringVar(&opts.OutputFile, "output-file", "", "Path to output JSON file with GSA details (default: stdout)")
	cmd.Flags().StringVar(&opts.OIDCIssuerURL, "oidc-issuer-url", "", "OIDC issuer URL for WIF provider (defaults to the discovered OIDC endpoint + /{infra-id})")

	return cmd
}

func (o *CreateOptions) ValidateInputs() error {
	if o.InfraID == "" {
		return fmt.Errorf("infra-id is required")
	}
	if o.ProjectID == "" {
		return fmt.Errorf("project is required")
	}
	if o.ClusterOIDCJWKSFile == "" && o.OIDCIssuerURL == "" {
		return fmt.Errorf("at least one of --oidc-jwks-file or --oidc-issuer-url is required")
	}

	if o.ClusterOIDCJWKSFile != "" {
		if err := o.ValidateJWKSFile(); err != nil {
			return fmt.Errorf("invalid JWKS file: %w", err)
		}
	}

	return nil
}

func (o *CreateOptions) Run(ctx context.Context, logger logr.Logger) error {
	results, err := o.CreateIAM(ctx, logger)
	if err != nil {
		return err
	}
	return o.writeOutput(results, logger)
}

func (o *CreateOptions) CreateIAM(ctx context.Context, logger logr.Logger) (*CreateOutput, error) {
	iamManager, err := NewManager(ctx, o.ProjectID, o.InfraID, o.ClusterOIDCJWKSFile, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize GCP clients: %w", err)
	}

	if o.OIDCIssuerURL != "" {
		iamManager.SetOIDCIssuerURL(o.OIDCIssuerURL)
	}

	projectNumber, err := iamManager.GetProjectNumber(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to validate project: %w", err)
	}

	results := &CreateOutput{
		ProjectID:     o.ProjectID,
		ProjectNumber: projectNumber,
		InfraID:       o.InfraID,
	}

	poolID, err := iamManager.CreateWorkloadIdentityPool(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create Workload Identity Pool: %w", err)
	}

	providerID, providerAudience, err := iamManager.CreateOIDCProvider(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create OIDC Provider: %w", err)
	}

	serviceAccountEmails, err := iamManager.CreateServiceAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create service accounts: %w", err)
	}

	results.WorkloadIdentityPool = WorkloadIdentityConfig{
		PoolID:     poolID,
		ProviderID: providerID,
		Audience:   providerAudience,
	}
	results.ServiceAccounts = serviceAccountEmails

	logger.Info("Created GCP IAM infrastructure", "infraID", o.InfraID, "projectID", o.ProjectID, "serviceAccountsCreated", len(serviceAccountEmails))

	return results, nil
}

func (o *CreateOptions) writeOutput(results *CreateOutput, logger logr.Logger) error {
	out := os.Stdout
	if len(o.OutputFile) > 0 {
		var err error
		out, err = os.Create(o.OutputFile)
		if err != nil {
			return fmt.Errorf("cannot create output file: %w", err)
		}
		defer func(out *os.File) {
			if err := out.Close(); err != nil {
				logger.Error(err, "Failed to close output file", "file", o.OutputFile)
			}
		}(out)
	}
	outputBytes, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize result: %w", err)
	}
	_, err = out.Write(outputBytes)
	if err != nil {
		return fmt.Errorf("failed to write result: %w", err)
	}
	return nil
}

func (o *CreateOptions) ValidateJWKSFile() error {
	jwksData, err := os.ReadFile(o.ClusterOIDCJWKSFile)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("cluster OIDC JWKS file does not exist: %s", o.ClusterOIDCJWKSFile)
		}
		return fmt.Errorf("failed to read JWKS file: %w", err)
	}

	var jwks map[string]interface{}
	if err := json.Unmarshal(jwksData, &jwks); err != nil {
		return fmt.Errorf("JWKS file contains invalid JSON: %w", err)
	}

	if _, exists := jwks["keys"]; !exists {
		return fmt.Errorf("JWKS file must contain a 'keys' field")
	}

	return nil
}
