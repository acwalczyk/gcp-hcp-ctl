package cli

import (
	"bytes"
	"context"
	"fmt"
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
			cmd, remaining, err := newRootCmd().Find([]string{group, verb})
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
				configPath := filepath.Join(t.TempDir(), "config.yaml")
				if err := os.WriteFile(configPath, []byte("project: customer\napi_endpoint: "+server.URL+"\noutput: "+format+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
				c, _ := platformapi.NewClientForTest(server.URL, "customer")
				root := newRootCmd()
				root.SetContext(access.WithClient(context.Background(), c))
				var out, stderr bytes.Buffer
				root.SetOut(&out)
				root.SetErr(&stderr)
				args := []string{"role", "list", "--config", configPath}
				if override {
					args = append(args, "--output", "text")
				}
				root.SetArgs(args)
				if err := root.Execute(); err != nil {
					t.Fatal(err)
				}
				project, _ := root.PersistentFlags().GetString("project")
				apiEndpoint, _ := root.PersistentFlags().GetString("api-endpoint")
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

func TestAccessEndpointDiscoveryRequiresRegion(t *testing.T) {
	t.Setenv("GCPHCPCTL_API_ENDPOINT", "")
	t.Setenv("GCPHCPCTL_REGION", "")
	root := newRootCmd()
	root.SetArgs([]string{"role", "list", "--project", "customer", "--env", "int", "--config", filepath.Join(t.TempDir(), "missing.yaml")})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--region is required for endpoint discovery") {
		t.Fatalf("expected missing region error, got %v", err)
	}
}
