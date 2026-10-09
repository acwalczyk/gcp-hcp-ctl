package iam

import (
	"strings"
	"testing"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/config"
)

func TestDestroyOptionsValidateInputs(t *testing.T) {
	tests := []struct {
		name          string
		opts          *DestroyOptions
		expectedError string
	}{
		{
			name: "When all required fields are provided it should pass validation",
			opts: &DestroyOptions{
				InfraID:   "test-infra-id",
				ProjectID: "test-project-id",
			},
		},
		{
			name: "When infra-id is missing it should return error",
			opts: &DestroyOptions{
				InfraID:   "",
				ProjectID: "test-project-id",
			},
			expectedError: "infra-id is required",
		},
		{
			name: "When project is missing it should return error",
			opts: &DestroyOptions{
				InfraID:   "test-infra-id",
				ProjectID: "",
			},
			expectedError: "project is required",
		},
		{
			name: "When both infra-id and project are missing it should return infra-id error first",
			opts: &DestroyOptions{
				InfraID:   "",
				ProjectID: "",
			},
			expectedError: "infra-id is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.opts.ValidateInputs()

			if tt.expectedError != "" {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.expectedError)
					return
				}
				if !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("expected error containing %q, got %q", tt.expectedError, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			}
		})
	}
}

func TestNewDestroyCommand(t *testing.T) {
	cmd := NewDestroyCommand()

	if cmd == nil {
		t.Fatal("expected command to be non-nil")
	}

	if cmd.Use != "destroy <infra-id>" {
		t.Errorf("expected Use to be %q, got %q", "destroy <infra-id>", cmd.Use)
	}

	yesFlag := cmd.Flag("yes")
	if yesFlag == nil {
		t.Error("expected yes flag to be defined")
	}
}

func TestNewCreateCommand(t *testing.T) {
	cmd := NewCreateCommand(&config.Config{})

	if cmd == nil {
		t.Fatal("expected command to be non-nil")
	}

	if cmd.Use != "create <infra-id>" {
		t.Errorf("expected Use to be %q, got %q", "create <infra-id>", cmd.Use)
	}

	jwksFlag := cmd.Flag("oidc-jwks-file")
	if jwksFlag == nil {
		t.Error("expected oidc-jwks-file flag to be defined")
	}

	issuerFlag := cmd.Flag("oidc-issuer-url")
	if issuerFlag == nil {
		t.Error("expected oidc-issuer-url flag to be defined")
	}

	outputFlag := cmd.Flag("output-file")
	if outputFlag == nil {
		t.Error("expected output-file flag to be defined")
	}
}

func TestNewIAMCmd(t *testing.T) {
	cmd := NewIAMCmd(&config.Config{})

	if cmd == nil {
		t.Fatal("expected command to be non-nil")
	}

	if cmd.Use != "iam" {
		t.Errorf("expected Use to be %q, got %q", "iam", cmd.Use)
	}

	if len(cmd.Commands()) != 2 {
		t.Errorf("expected 2 subcommands, got %d", len(cmd.Commands()))
	}
}
