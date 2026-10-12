package cli

import (
	"bytes"
	"context"
	"fmt"
	"github.com/spf13/cobra"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/internal/access"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/platformapi"
)

func TestAccessCommandDiscovery(t *testing.T) {
	for group, verbs := range map[string][]string{"role": {"create", "update", "list", "get", "delete"}, "rolebinding": {"create", "list", "get", "delete"}} {
		for _, verb := range verbs {
			cmd, remaining, err := rootCmd.Find([]string{group, verb})
			if err != nil || len(remaining) != 0 || cmd.Name() != verb || cmd.Parent().Name() != group {
				t.Fatalf("%s %s: %v", group, verb, err)
			}
			if cmd.Flags().Lookup("output") != nil {
				t.Fatal("resource shadows inherited output")
			}
		}
	}
}

func TestAccessResolvedConfiguration(t *testing.T) {
	// The root uses global flag variables. Save and restore them; do not run in parallel.
	oldProject, oldEndpoint, oldFormat, oldConfig := project, apiEndpoint, outputFormat, configPath
	oldOut, oldErr, oldCtx := rootCmd.OutOrStdout(), rootCmd.ErrOrStderr(), rootCmd.Context()
	defer func() {
		project, apiEndpoint, outputFormat, configPath = oldProject, oldEndpoint, oldFormat, oldConfig
		rootCmd.SetOut(oldOut)
		rootCmd.SetErr(oldErr)
		rootCmd.SetContext(oldCtx)
		rootCmd.SetArgs(nil)
		rootCmd.PersistentFlags().Lookup("output").Changed = false
	}()
	for _, format := range []string{"json", "yaml"} {
		for _, override := range []bool{false, true} {
			t.Run(fmt.Sprint(format, override), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != "/apis/gcp.managed.openshift.io/v1/namespaces/customer/roles" {
						t.Error(r.URL)
					}
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `{"apiVersion":"gcp.managed.openshift.io/v1","kind":"RoleList","items":[]}`)
				}))
				defer server.Close()
				configPath = filepath.Join(t.TempDir(), "config.yaml")
				if err := os.WriteFile(configPath, []byte("project: customer\napi_endpoint: "+server.URL+"\noutput: "+format+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				project, apiEndpoint, outputFormat = "", "", "text"
				rootCmd.PersistentFlags().Lookup("output").Changed = false
				c, _ := platformapi.NewClientForTest(server.URL, "customer")
				var resetContext func(*cobra.Command)
				resetContext = func(cmd *cobra.Command) {
					cmd.SetContext(nil)
					for _, child := range cmd.Commands() {
						resetContext(child)
					}
				}
				resetContext(rootCmd)
				rootCmd.SetContext(access.WithClient(context.Background(), c))
				var out, stderr bytes.Buffer
				rootCmd.SetOut(&out)
				rootCmd.SetErr(&stderr)
				args := []string{"role", "list"}
				if override {
					args = append(args, "--output", "text")
				}
				rootCmd.SetArgs(args)
				if err := rootCmd.Execute(); err != nil {
					t.Fatal(err)
				}
				if project != "customer" || apiEndpoint != server.URL {
					t.Fatal(project, apiEndpoint)
				}
				if override {
					if !strings.Contains(out.String(), "PERMISSIONS") {
						t.Fatal(out.String())
					}
				} else if !strings.Contains(out.String(), "apiVersion") {
					t.Fatal(out.String())
				}
				if stderr.Len() != 0 {
					t.Fatal(stderr.String())
				}
			})
		}
	}
}
