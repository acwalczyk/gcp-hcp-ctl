package version

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/platformapi"
	"github.com/spf13/cobra"
)

func versionTestClient(t *testing.T, handler http.HandlerFunc) *platformapi.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := platformapi.NewClientForTest(server.URL, "my-project")
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func executeVersionTestCommand(t *testing.T, client *platformapi.Client, args ...string) (error, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := &cobra.Command{Use: "gcphcpctl", SilenceUsage: true}
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	group := NewVersionCmd()
	group.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		cmd.SetContext(context.WithValue(cmd.Context(), clientKey, client))
		return nil
	}
	root.AddCommand(group)
	root.SetArgs(append([]string{"versions"}, args...))
	err := root.Execute()
	return err, stdout.String(), stderr.String()
}

func TestVersionListCommand(t *testing.T) {
	client := versionTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/apis/gcp.managed.openshift.io/v1/versions":
			_, _ = w.Write([]byte(`{
  "apiVersion":"gcp.managed.openshift.io/v1",
  "kind":"VersionList",
  "items":[
    {"metadata":{"name":"4.22.10"},"spec":{"channelGroups":["fast","stable"]}},
    {"metadata":{"name":"4.22.2"},"spec":{"channelGroups":["candidate","stable"]}}
  ]
}`))
		case "/apis/gcp.managed.openshift.io/v1/channels":
			_, _ = w.Write([]byte(`{
  "apiVersion":"gcp.managed.openshift.io/v1",
  "kind":"ChannelList",
  "items":[
    {"metadata":{"name":"stable"},"spec":{"installDefaultVersion":"4.22.10","fleetMinorVersion":"4.22"}}
  ]
}`))
		default:
			t.Errorf("path = %q", r.URL.Path)
		}
	})

	err, stdout, stderr := executeVersionTestCommand(t, client, "list")
	if err != nil {
		t.Fatalf("executing command: %v", err)
	}
	if stderr != "" {
		t.Errorf("unexpected stderr: %q", stderr)
	}
	for _, want := range []string{"VERSION", "CHANNEL GROUPS", "4.22.2", "candidate, stable", "4.22.10", "fast, stable (default)"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output missing %q:\n%s", want, stdout)
		}
	}
	if strings.Index(stdout, "4.22.2") > strings.Index(stdout, "4.22.10") {
		t.Errorf("versions are not semantically ordered:\n%s", stdout)
	}
}

func TestVersionListCommandContinuesWhenChannelsFail(t *testing.T) {
	client := versionTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/channels") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"InternalError"}`))
			return
		}
		_, _ = w.Write([]byte(`{
  "apiVersion":"gcp.managed.openshift.io/v1",
  "kind":"VersionList",
  "items":[{"metadata":{"name":"4.22.14"},"spec":{"channelGroups":["stable"]}}]
}`))
	})

	err, stdout, stderr := executeVersionTestCommand(t, client, "list")
	if err != nil {
		t.Fatalf("executing command: %v", err)
	}
	if !strings.Contains(stdout, "4.22.14") || !strings.Contains(stdout, "stable") {
		t.Errorf("version output missing after channel failure:\n%s", stdout)
	}
	if !strings.Contains(stderr, "Warning: unable to load channel defaults") {
		t.Errorf("warning missing from stderr: %q", stderr)
	}
}

func TestVersionGetCommandYAML(t *testing.T) {
	client := versionTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apis/gcp.managed.openshift.io/v1/versions/4.22.14" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
  "apiVersion":"gcp.managed.openshift.io/v1",
  "kind":"Version",
  "metadata":{"name":"4.22.14","managedFields":[{"manager":"gecko-controllers","fieldsV1":{"f:spec":{"f:releaseImage":{}}}}]},
  "spec":{"channelGroups":["stable"]}
}`))
	})

	err, stdout, stderr := executeVersionTestCommand(t, client, "get", "4.22.14", "-o", "yaml")
	if err != nil {
		t.Fatalf("executing command: %v", err)
	}
	if stderr != "" {
		t.Errorf("unexpected stderr: %q", stderr)
	}
	for _, want := range []string{
		"apiVersion: gcp.managed.openshift.io/v1",
		"kind: Version",
		"name: 4.22.14",
		"channelGroups:",
		"- stable",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("output missing %q:\n%s", want, stdout)
		}
	}
	for _, unwanted := range []string{"managedFields", "releaseImage"} {
		if strings.Contains(stdout, unwanted) {
			t.Errorf("output contains %q:\n%s", unwanted, stdout)
		}
	}
}

func TestVersionGetCommandNotFound(t *testing.T) {
	client := versionTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound"}`))
	})

	err, stdout, _ := executeVersionTestCommand(t, client, "get", "4.19.99")
	if err == nil || err.Error() != `getting version "4.19.99": not found` {
		t.Fatalf("error = %v", err)
	}
	if stdout != "" {
		t.Errorf("unexpected stdout: %q", stdout)
	}
}
