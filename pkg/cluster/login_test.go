package cluster

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/kubeconfig"
)

func writeTestKubeconfig(t *testing.T, path, currentContext string) {
	t.Helper()
	content := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    server: https://prev.example.com
  name: %[1]s
contexts:
- context:
    cluster: %[1]s
    user: %[1]s
  name: %[1]s
users:
- name: %[1]s
  user: {}
current-context: %[1]s
`, currentContext)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("creating dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("writing kubeconfig: %v", err)
	}
}

func setupLoginWithValidation(t *testing.T, previousContext string, validateFn validateAccessFunc) (output string, err error) {
	t.Helper()
	dir := t.TempDir()
	kcPath := filepath.Join(dir, "config")

	if previousContext != "" {
		writeTestKubeconfig(t, kcPath, previousContext)
	}

	contextName, prevCtx, kubeconfigPath, updateErr := kubeconfig.Update(kubeconfig.UpdateOptions{
		ClusterName:    "test-cluster",
		Server:         "https://api.test.example.com",
		KubeconfigPath: kcPath,
	})
	if updateErr != nil {
		t.Fatalf("kubeconfig.Update error: %v", updateErr)
	}

	var buf bytes.Buffer
	_, valErr := validateFn(context.Background(), kubeconfigPath, contextName)
	if valErr != nil {
		err = handleValidationFailure(&buf, valErr, kubeconfigPath, contextName, prevCtx)
		return buf.String(), err
	}
	return buf.String(), nil
}

func TestLoginRollback(t *testing.T) {
	t.Run("When validation fails with previousContext it should restore the previous context", func(t *testing.T) {
		output, err := setupLoginWithValidation(t, "prev-ctx", func(_ context.Context, _, _ string) (string, error) {
			return "", fmt.Errorf("connection refused")
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(output, `Current context restored to "prev-ctx"`) {
			t.Errorf("expected restore message in output, got:\n%s", output)
		}
	})

	t.Run("When validation fails without previousContext it should not attempt restore", func(t *testing.T) {
		output, err := setupLoginWithValidation(t, "", func(_ context.Context, _, _ string) (string, error) {
			return "", fmt.Errorf("connection refused")
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if strings.Contains(output, "restored") {
			t.Errorf("should not attempt restore when no previous context, got:\n%s", output)
		}
		if !strings.Contains(output, "To switch manually") {
			t.Errorf("expected manual switch hint in output, got:\n%s", output)
		}
	})

	t.Run("When RestoreContext itself fails it should report the restore error", func(t *testing.T) {
		dir := t.TempDir()
		kcPath := filepath.Join(dir, "config")

		writeTestKubeconfig(t, kcPath, "original-ctx")

		contextName, prevCtx, kubeconfigPath, err := kubeconfig.Update(kubeconfig.UpdateOptions{
			ClusterName:    "test-cluster",
			Server:         "https://api.test.example.com",
			KubeconfigPath: kcPath,
		})
		if err != nil {
			t.Fatalf("kubeconfig.Update error: %v", err)
		}
		if prevCtx == "" {
			t.Fatal("expected non-empty previous context")
		}

		if removeErr := os.Remove(kubeconfigPath); removeErr != nil {
			t.Fatalf("removing kubeconfig: %v", removeErr)
		}

		var buf bytes.Buffer
		valErr := fmt.Errorf("connection refused")
		_ = handleValidationFailure(&buf, valErr, kubeconfigPath, contextName, prevCtx)

		output := buf.String()
		if !strings.Contains(output, "Could not restore previous context") {
			t.Errorf("expected restore failure message, got:\n%s", output)
		}
		if !strings.Contains(output, prevCtx) {
			t.Errorf("expected previous context name in error, got:\n%s", output)
		}
	})
}
