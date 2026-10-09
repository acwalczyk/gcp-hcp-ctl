package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func childCommand(t *testing.T, parent *cobra.Command, name string) *cobra.Command {
	t.Helper()
	for _, child := range parent.Commands() {
		if child.Name() == name {
			return child
		}
	}
	t.Fatalf("%s has no %s subcommand", parent.Name(), name)
	return nil
}

func TestConfigCommandUsesFreshState(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("project: file-project\nregion: us-central1\noutput: json\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GCPHCPCTL_PROJECT", "env-project")
	var output bytes.Buffer
	root := newRootCmd()
	root.SetOut(&output)
	root.SetArgs([]string{"config", "--config", configPath, "--project", "flag-project"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "flag-project") || !strings.Contains(output.String(), "json") {
		t.Errorf("flag and file values missing from effective config:\n%s", output.String())
	}
	project, err := childCommand(t, root, "config").Flags().GetString("project")
	if err != nil || project != "flag-project" {
		t.Errorf("inherited project flag = %q, %v; want flag-project", project, err)
	}
	region, err := childCommand(t, root, "config").Flags().GetString("region")
	if err != nil || region != "us-central1" {
		t.Errorf("inherited region flag = %q, %v; want us-central1", region, err)
	}

	output.Reset()
	root = newRootCmd()
	root.SetOut(&output)
	root.SetArgs([]string{"config", "--config", configPath})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "env-project") || strings.Contains(output.String(), "flag-project") {
		t.Errorf("fresh command leaked a previous flag value:\n%s", output.String())
	}
}

func TestEndpointFlagsOverrideEnvironmentAndFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("api_endpoint: https://file-api.example.com\noidc_endpoint: https://file-oidc.example.com\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GCPHCPCTL_API_ENDPOINT", "https://env-api.example.com")
	t.Setenv("GCPHCPCTL_OIDC_ENDPOINT", "https://env-oidc.example.com")

	var output bytes.Buffer
	root := newRootCmd()
	root.SetOut(&output)
	root.SetArgs([]string{"config", "--config", configPath,
		"--api-endpoint", "https://flag-api.example.com",
		"--oidc-endpoint", "https://flag-oidc.example.com"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	for _, endpoint := range []string{"https://flag-api.example.com", "https://flag-oidc.example.com"} {
		if !strings.Contains(output.String(), endpoint) {
			t.Errorf("effective config missing %q:\n%s", endpoint, output.String())
		}
	}
	for flag, want := range map[string]string{
		"api-endpoint":  "https://flag-api.example.com",
		"oidc-endpoint": "https://flag-oidc.example.com",
	} {
		got, err := childCommand(t, root, "config").Flags().GetString(flag)
		if err != nil || got != want {
			t.Errorf("inherited %s = %q, %v; want %q", flag, got, err, want)
		}
	}

	output.Reset()
	root = newRootCmd()
	root.SetOut(&output)
	root.SetArgs([]string{"config", "--config", configPath})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "https://env-api.example.com") || !strings.Contains(output.String(), "https://env-oidc.example.com") {
		t.Errorf("environment endpoints did not override file:\n%s", output.String())
	}
}
