package version

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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

func writeVersionFixture(t *testing.T, w http.ResponseWriter, body []byte) {
	t.Helper()
	if _, err := w.Write(body); err != nil {
		t.Errorf("writing response fixture: %v", err)
	}
}

func executeVersionTestCommand(t *testing.T, client *platformapi.Client, args ...string) (error, string, string) {
	return executeVersionTestCommandWithFormat(t, client, "", args...)
}

func executeVersionTestCommandWithFormat(t *testing.T, client *platformapi.Client, configuredFormat string, args ...string) (error, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := &cobra.Command{Use: "gcphcpctl", SilenceUsage: true}
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	var outputFormat string
	root.PersistentFlags().StringVarP(&outputFormat, "output", "o", "text", "Output format")
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		if configuredFormat != "" && !cmd.Flags().Changed("output") {
			return root.PersistentFlags().Lookup("output").Value.Set(configuredFormat)
		}
		return nil
	}
	group := NewVersionCmd()
	group.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		if err := root.PersistentPreRunE(cmd, nil); err != nil {
			return err
		}
		cmd.SetContext(context.WithValue(cmd.Context(), clientKey, client))
		return nil
	}
	root.AddCommand(group)
	root.SetArgs(append([]string{"versions"}, args...))
	err := root.Execute()
	return err, stdout.String(), stderr.String()
}

type failingVersionWriter struct{ err error }

func (w failingVersionWriter) Write([]byte) (int, error) { return 0, w.err }

func TestVersionListReturnsWarningWriteError(t *testing.T) {
	want := errors.New("stderr unavailable")
	client := versionTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/channels") {
			w.WriteHeader(http.StatusInternalServerError)
			writeVersionFixture(t, w, []byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"InternalError"}`))
			return
		}
		writeVersionFixture(t, w, []byte(`{"apiVersion":"gcp.managed.openshift.io/v1","kind":"VersionList","items":[{"metadata":{"name":"4.22.14"}}]}`))
	})
	cmd := newListCmd()
	cmd.SetContext(context.WithValue(context.Background(), clientKey, client))
	cmd.SetOut(io.Discard)
	cmd.SetErr(failingVersionWriter{err: want})
	cmd.Flags().String("output", "text", "Output format")
	if err := cmd.Execute(); !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func TestVersionCommandsUseConfiguredOutput(t *testing.T) {
	client := versionTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/apis/gcp.managed.openshift.io/v1/versions":
			writeVersionFixture(t, w, []byte(`{"apiVersion":"gcp.managed.openshift.io/v1","kind":"VersionList","items":[{"metadata":{"name":"4.22.14"},"spec":{"channelGroups":["stable"]}}]}`))
		case "/apis/gcp.managed.openshift.io/v1/versions/4.22.14":
			writeVersionFixture(t, w, []byte(`{"apiVersion":"gcp.managed.openshift.io/v1","kind":"Version","metadata":{"name":"4.22.14"},"spec":{"channelGroups":["stable"]}}`))
		default:
			t.Errorf("unexpected request path: %s", r.URL.Path)
		}
	})
	for _, tc := range []struct {
		name, format, want string
		args               []string
	}{
		{name: "configured list JSON", format: "json", args: []string{"list"}, want: `"kind": "VersionList"`},
		{name: "configured get YAML", format: "yaml", args: []string{"get", "4.22.14"}, want: "kind: Version"},
		{name: "explicit output overrides config", format: "yaml", args: []string{"get", "4.22.14", "-o", "json"}, want: `"kind": "Version"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err, stdout, stderr := executeVersionTestCommandWithFormat(t, client, tc.format, tc.args...)
			if err != nil {
				t.Fatal(err)
			}
			if stderr != "" {
				t.Errorf("stderr = %q", stderr)
			}
			if !strings.Contains(stdout, tc.want) {
				t.Errorf("output missing %q:\n%s", tc.want, stdout)
			}
		})
	}
}

func TestVersionListCommand(t *testing.T) {
	client := versionTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/apis/gcp.managed.openshift.io/v1/versions":
			writeVersionFixture(t, w, []byte(`{
  "apiVersion":"gcp.managed.openshift.io/v1",
  "kind":"VersionList",
  "items":[
    {"metadata":{"name":"4.22.10"},"spec":{"channelGroups":["fast","stable"]}},
    {"metadata":{"name":"4.22.2"},"spec":{"channelGroups":["candidate","stable"]}}
  ]
}`))
		case "/apis/gcp.managed.openshift.io/v1/channels":
			writeVersionFixture(t, w, []byte(`{
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

func TestVersionListMixedNamesHaveConsistentOrder(t *testing.T) {
	for _, names := range [][]string{
		{"4.22.2", "4.22.10", "4.22.11x"},
		{"4.22.2", "4.22.11x", "4.22.10"},
		{"4.22.10", "4.22.2", "4.22.11x"},
		{"4.22.10", "4.22.11x", "4.22.2"},
		{"4.22.11x", "4.22.2", "4.22.10"},
		{"4.22.11x", "4.22.10", "4.22.2"},
	} {
		t.Run(strings.Join(names, "_"), func(t *testing.T) {
			client := versionTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if strings.HasSuffix(r.URL.Path, "/channels") {
					writeVersionFixture(t, w, []byte(`{"apiVersion":"gcp.managed.openshift.io/v1","kind":"ChannelList","items":[]}`))
					return
				}
				items := make([]string, len(names))
				for i, name := range names {
					items[i] = fmt.Sprintf(`{"metadata":{"name":%q}}`, name)
				}
				writeVersionFixture(t, w, []byte(`{"apiVersion":"gcp.managed.openshift.io/v1","kind":"VersionList","items":[`+strings.Join(items, ",")+`]}`))
			})
			err, stdout, _ := executeVersionTestCommand(t, client, "list")
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, line := range strings.Split(strings.TrimSpace(stdout), "\n")[1:] {
				got = append(got, strings.Fields(line)[0])
			}
			if want := "4.22.2,4.22.10,4.22.11x"; strings.Join(got, ",") != want {
				t.Errorf("ordering = %v, want %s", got, want)
			}
		})
	}
}

func TestVersionListCommandContinuesWhenChannelsFail(t *testing.T) {
	client := versionTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/channels") {
			w.WriteHeader(http.StatusInternalServerError)
			writeVersionFixture(t, w, []byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"InternalError"}`))
			return
		}
		writeVersionFixture(t, w, []byte(`{
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
		writeVersionFixture(t, w, []byte(`{
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
		writeVersionFixture(t, w, []byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound"}`))
	})

	err, stdout, _ := executeVersionTestCommand(t, client, "get", "4.19.99")
	if err == nil || err.Error() != `getting version "4.19.99": not found` {
		t.Fatalf("error = %v", err)
	}
	if stdout != "" {
		t.Errorf("unexpected stdout: %q", stdout)
	}
}
