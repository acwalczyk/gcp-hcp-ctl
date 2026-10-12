package auth

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

func TestGcloudFetcher_WhenIdentityTokenCommandExits_ItMarksAuthenticationFailure(t *testing.T) {
	fetcher := gcloudFetcher{
		commandOutput: func(_ context.Context, args ...string) ([]byte, error) {
			if len(args) != 2 || args[0] != "auth" || args[1] != "print-identity-token" {
				t.Fatalf("gcloud arguments = %q, want auth print-identity-token", args)
			}
			return nil, &exec.ExitError{Stderr: []byte("no active account")}
		},
	}

	_, _, err := fetcher.FetchIdentityToken(context.Background())
	if !errors.Is(err, ErrGcloudAuthentication) {
		t.Fatalf("error = %v, want ErrGcloudAuthentication", err)
	}
	if strings.Contains(err.Error(), "no active account") {
		t.Fatalf("error = %q unexpectedly includes gcloud output", err)
	}
	if !strings.Contains(err.Error(), "gcloud auth login") {
		t.Fatalf("error = %q, want gcloud login guidance", err)
	}
}

func TestGcloudFetcher_WhenAccountCommandExits_ItReturnsSafeAuthenticationError(t *testing.T) {
	fetcher := gcloudFetcher{
		commandOutput: func(_ context.Context, args ...string) ([]byte, error) {
			if len(args) != 3 || args[0] != "config" || args[1] != "get-value" || args[2] != "account" {
				t.Fatalf("gcloud arguments = %q, want config get-value account", args)
			}
			return nil, &exec.ExitError{Stderr: []byte("sensitive gcloud output")}
		},
	}

	_, err := fetcher.FetchAccountEmail(context.Background())
	if !errors.Is(err, ErrGcloudAuthentication) {
		t.Fatalf("error = %v, want ErrGcloudAuthentication", err)
	}
	if strings.Contains(err.Error(), "sensitive gcloud output") {
		t.Fatalf("error = %q unexpectedly includes gcloud output", err)
	}
	if !strings.Contains(err.Error(), "gcloud auth login") {
		t.Fatalf("error = %q, want gcloud login guidance", err)
	}
}

func TestGcloudFetcher_CanceledContextTakesPriorityOverCommandFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fetcher := gcloudFetcher{
		commandOutput: func(context.Context, ...string) ([]byte, error) {
			return nil, &exec.ExitError{Stderr: []byte("gcloud command failed")}
		},
	}
	tests := []struct {
		name  string
		fetch func(context.Context) error
	}{
		{
			name: "identity token",
			fetch: func(ctx context.Context) error {
				_, _, err := fetcher.FetchIdentityToken(ctx)
				return err
			},
		},
		{
			name: "account email",
			fetch: func(ctx context.Context) error {
				_, err := fetcher.FetchAccountEmail(ctx)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fetch(ctx)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want context.Canceled", err)
			}
			if errors.Is(err, ErrGcloudAuthentication) {
				t.Fatalf("error = %v unexpectedly matches ErrGcloudAuthentication", err)
			}
		})
	}
}

func TestGcloudFetcher_WhenIdentityTokenCommandCannotStart_ItDoesNotMarkAuthenticationFailure(t *testing.T) {
	fetcher := gcloudFetcher{
		commandOutput: func(context.Context, ...string) ([]byte, error) {
			return nil, errors.New("gcloud executable not found")
		},
	}

	_, _, err := fetcher.FetchIdentityToken(context.Background())
	if errors.Is(err, ErrGcloudAuthentication) {
		t.Fatalf("error = %v unexpectedly matches ErrGcloudAuthentication", err)
	}
}

func TestGcloudFetcher_WhenAccountIsUnset_ItMarksAuthenticationFailure(t *testing.T) {
	fetcher := gcloudFetcher{
		commandOutput: func(_ context.Context, args ...string) ([]byte, error) {
			if len(args) != 3 || args[0] != "config" || args[1] != "get-value" || args[2] != "account" {
				t.Fatalf("gcloud arguments = %q, want config get-value account", args)
			}
			return []byte("(unset)\n"), nil
		},
	}

	_, err := fetcher.FetchAccountEmail(context.Background())
	if !errors.Is(err, ErrGcloudAuthentication) {
		t.Fatalf("error = %v, want ErrGcloudAuthentication", err)
	}
}
