package role_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/internal/access"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/platformapi"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/role"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/rolebinding"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

const apiVersion = "gcp.managed.openshift.io/v1"
const base = "/apis/" + apiVersion + "/namespaces/customer/"
const note = "Note: authorization changes may take a moment to propagate globally."

func execute(t *testing.T, handler http.HandlerFunc, args ...string) (string, string, error) {
	t.Helper()
	server := httptest.NewServer(handler)
	defer server.Close()
	client, err := platformapi.NewClientForTest(server.URL, "customer")
	if err != nil {
		t.Fatal(err)
	}
	root := &cobra.Command{Use: "test", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("project", "customer", "")
	root.PersistentFlags().String("api-endpoint", server.URL, "")
	root.PersistentFlags().StringP("output", "o", "text", "")
	root.AddCommand(role.NewRoleCmd(), rolebinding.NewRoleBindingCmd())
	var out, stderr bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&stderr)
	root.SetArgs(args)
	err = root.ExecuteContext(access.WithClient(context.Background(), client))
	return out.String(), stderr.String(), err
}

func TestValidationMakesNoRequests(t *testing.T) {
	cases := [][]string{
		{"role", "create"}, {"role", "create", " "}, {"role", "get", "x", "y"}, {"role", "list", "x"},
		{"role", "create", "x"}, {"role", "create", "x", "--permission", " "},
		{"role", "create", "x", "--permission", "cluster.get", "--permission", "cluster.get"},
		{"role", "update", "x"}, {"role", "delete", "x"},
		{"rolebinding", "create", "x"}, {"rolebinding", "create", "x", "--subject", "bad", "--role", "r"},
		{"rolebinding", "create", "x", "--subject", "a@b"},
		{"rolebinding", "create", "x", "--subject", "a@b", "--role", "r", "--platform-role", "p"},
		{"rolebinding", "create", "x", "--subject", "a@b", "--role", ""},
		{"rolebinding", "create", "x", "--subject", "a@b", "--platform-role", " "},
		{"rolebinding", "delete", "x"}, {"rolebinding", "list", "--subject", ""},
		{"rolebinding", "list", "extra"}, {"rolebinding", "get", " "},
		{"--project", "", "role", "list"}, {"--api-endpoint", "", "rolebinding", "list"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			calls := 0
			out, stderr, err := execute(t, func(w http.ResponseWriter, r *http.Request) { calls++ }, args...)
			if err == nil || calls != 0 {
				t.Fatalf("error=%v requests=%d", err, calls)
			}
			if strings.Contains(out+stderr, note) {
				t.Fatal("note on failure")
			}
		})
	}
}

func TestCreatePayloadAndStreams(t *testing.T) {
	for _, kind := range []string{"Role", "RoleBinding"} {
		for _, format := range []string{"text", "json", "yaml"} {
			for _, ref := range []string{"Role", "PlatformRole"} {
				if kind == "Role" && ref == "PlatformRole" {
					continue
				}
				t.Run(kind+format+ref, func(t *testing.T) {
					calls := 0
					group := strings.ToLower(kind)
					args := []string{group, "create", "grant", "-o", format}
					if kind == "Role" {
						args = append(args, "--permission", "cluster.get", "--permission", "future.permission")
					} else {
						flag := "--role"
						if ref == "PlatformRole" {
							flag = "--platform-role"
						}
						args = append(args, "--subject", " Alice@EXAMPLE.COM ", flag, "arbitrary-role")
					}
					out, stderr, err := execute(t, func(w http.ResponseWriter, r *http.Request) {
						calls++
						if r.Method != "POST" || r.URL.Path != base+group+"s" {
							t.Errorf("%s %s", r.Method, r.URL.Path)
						}
						var obj map[string]any
						if e := json.NewDecoder(r.Body).Decode(&obj); e != nil {
							t.Error(e)
						}
						if obj["kind"] != kind || obj["apiVersion"] != apiVersion {
							t.Errorf("type meta: %v", obj)
						}
						m := obj["metadata"].(map[string]any)
						if m["name"] != "grant" || m["namespace"] != "customer" {
							t.Errorf("metadata %v", m)
						}
						spec := obj["spec"].(map[string]any)
						if kind == "RoleBinding" {
							rr := spec["roleRef"].(map[string]any)
							if spec["subject"] != "Alice@example.com" || rr["kind"] != ref || rr["name"] != "arbitrary-role" || rr["apiGroup"] != "gcp.managed.openshift.io" {
								t.Errorf("spec %v", spec)
							}
						}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(201)
						_ = json.NewEncoder(w).Encode(obj)
					}, args...)
					if err != nil || calls != 1 {
						t.Fatalf("error=%v calls=%d", err, calls)
					}
					if format == "text" {
						if strings.Count(out, note) != 1 || stderr != "" {
							t.Fatalf("streams %q %q", out, stderr)
						}
					} else {
						if strings.Contains(out, note) || strings.Count(stderr, note) != 1 {
							t.Fatalf("streams %q %q", out, stderr)
						}
						var obj map[string]any
						if e := yaml.Unmarshal([]byte(out), &obj); e != nil {
							t.Fatal(e)
						}
						if obj["apiVersion"] != apiVersion || obj["metadata"] == nil {
							t.Fatal(obj)
						}
						if kind == "RoleBinding" {
							rr := obj["spec"].(map[string]any)["roleRef"].(map[string]any)
							if rr["apiGroup"] == nil {
								t.Fatal(rr)
							}
						}
					}
				})
			}
		}
	}
}

func TestUpdatePreservesMetadataAndReplaces(t *testing.T) {
	calls := []string{}
	out, stderr, err := execute(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method)
		if r.URL.Path != base+"roles/target" {
			t.Error(r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		obj := map[string]any{"apiVersion": apiVersion, "kind": "Role", "metadata": map[string]any{"name": "target", "namespace": "customer", "resourceVersion": "42", "uid": "original", "labels": map[string]any{"keep": "me"}}, "spec": map[string]any{"permissions": []string{"old"}}}
		if r.Method == "PUT" {
			var got map[string]any
			_ = json.NewDecoder(r.Body).Decode(&got)
			if !reflect.DeepEqual(got["metadata"], obj["metadata"]) {
				t.Errorf("metadata changed: %v", got)
			}
			if !reflect.DeepEqual(got["spec"].(map[string]any)["permissions"], []any{"new"}) {
				t.Errorf("not replaced: %v", got)
			}
			obj = got
		}
		_ = json.NewEncoder(w).Encode(obj)
	}, "role", "update", "target", "--permission", "new", "-o", "json")
	if err != nil || !reflect.DeepEqual(calls, []string{"GET", "PUT"}) {
		t.Fatalf("%v %v", err, calls)
	}
	if strings.Contains(out, "old") || strings.Count(stderr, note) != 1 {
		t.Fatalf("%q %q", out, stderr)
	}
}

func TestUpdateFailures(t *testing.T) {
	for _, status := range []int{404, 403, 409} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			out, stderr, err := execute(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				if status == 409 && r.Method == "GET" {
					fmt.Fprintf(w, `{"apiVersion":%q,"kind":"Role","metadata":{"name":"target","namespace":"customer","resourceVersion":"1"},"spec":{"permissions":["old"]}}`, apiVersion)
					return
				}
				w.WriteHeader(status)
				fmt.Fprint(w, `{"message":"SECRET SERVER PROSE"}`)
			}, "role", "update", "target", "--permission", "new")
			want := 1
			if status == 409 {
				want = 2
			}
			if err == nil || calls != want || strings.Contains(err.Error(), "SECRET") || strings.Contains(out+stderr, note) {
				t.Fatalf("%v %d %q %q", err, calls, out, stderr)
			}
		})
	}
}

func TestBindingListFilteringAllFormats(t *testing.T) {
	for _, format := range []string{"text", "json", "yaml"} {
		for _, subject := range []string{" Alice@EXAMPLE.COM ", "alice@example.com", "absent@example.com"} {
			t.Run(format+subject, func(t *testing.T) {
				out, stderr, err := execute(t, func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path != base+"rolebindings" || r.Method != "GET" {
						t.Fatal(r.URL)
					}
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprintf(w, `{"apiVersion":%q,"kind":"RoleBindingList","metadata":{"resourceVersion":"5"},"items":[{"metadata":{"name":"matching"},"spec":{"subject":"Alice@example.com","roleRef":{"kind":"Role","name":"limited","apiGroup":"gcp.managed.openshift.io"}}},{"metadata":{"name":"other"},"spec":{"subject":"bob@example.com","roleRef":{"kind":"PlatformRole","name":"cluster-viewer","apiGroup":"gcp.managed.openshift.io"}}}]}`, apiVersion)
				}, "rolebinding", "list", "--subject", subject, "-o", format)
				if err != nil || stderr != "" || strings.Contains(out, "other") {
					t.Fatalf("%v %q %q", err, out, stderr)
				}
				matches := strings.Contains(out, "matching")
				if matches != (strings.TrimSpace(subject) == "Alice@EXAMPLE.COM") {
					t.Fatal(out)
				}
				if format != "text" {
					var obj map[string]any
					if e := yaml.Unmarshal([]byte(out), &obj); e != nil {
						t.Fatal(e)
					}
					if obj["items"] == nil || obj["metadata"].(map[string]any)["resourceVersion"] != "5" {
						t.Fatal(obj)
					}
				} else if !strings.Contains(out, "ROLE KIND") || matches && !strings.Contains(out, "<unknown>") {
					t.Fatal(out)
				}
			})
		}
	}
}

func TestNamedDeletion(t *testing.T) {
	for _, group := range []string{"role", "rolebinding"} {
		for _, format := range []string{"text", "json", "yaml"} {
			t.Run(group+format, func(t *testing.T) {
				calls := 0
				out, stderr, err := execute(t, func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.Method != "DELETE" || r.URL.Path != base+group+"s/only-this" {
						t.Errorf("%s %s", r.Method, r.URL.Path)
					}
					w.WriteHeader(204)
				}, group, "delete", "only-this", "--confirm", "-o", format)
				if err != nil || calls != 1 {
					t.Fatalf("%v %d", err, calls)
				}
				if strings.Count(out+stderr, note) != 1 {
					t.Fatal(out, stderr)
				}
				if format != "text" {
					var obj map[string]any
					if e := yaml.Unmarshal([]byte(out), &obj); e != nil {
						t.Fatal(e)
					}
					if obj["accepted"] != true || obj["name"] != "only-this" || obj["namespace"] != "customer" {
						t.Fatal(obj)
					}
				}
			})
		}
	}
}

func TestGetAndRoleList(t *testing.T) {
	for _, group := range []string{"role", "rolebinding"} {
		for _, format := range []string{"text", "json", "yaml"} {
			for _, verb := range []string{"get", "list"} {
				if group == "rolebinding" && verb == "list" {
					continue
				}
				t.Run(group+format+verb, func(t *testing.T) {
					kind := "Role"
					spec := `"permissions":["cluster.get","nodepool.get"]`
					if group == "rolebinding" {
						kind = "RoleBinding"
						spec = `"subject":"Alice@example.com","roleRef":{"kind":"Role","name":"limited","apiGroup":"gcp.managed.openshift.io"}`
					}
					args := []string{group, verb}
					if verb == "get" {
						args = append(args, "item")
					}
					args = append(args, "-o", format)
					out, stderr, err := execute(t, func(w http.ResponseWriter, r *http.Request) {
						path := base + group + "s"
						if verb == "get" {
							path += "/item"
						}
						if r.Method != "GET" || r.URL.Path != path {
							t.Error(r.Method, r.URL.Path)
						}
						w.Header().Set("Content-Type", "application/json")
						obj := fmt.Sprintf(`{"apiVersion":%q,"kind":%q,"metadata":{"name":"item","namespace":"customer","labels":{"preserved":"yes"}},"spec":{%s}}`, apiVersion, kind, spec)
						if verb == "list" {
							obj = fmt.Sprintf(`{"apiVersion":%q,"kind":"RoleList","items":[%s]}`, apiVersion, obj)
						}
						fmt.Fprint(w, obj)
					}, args...)
					if err != nil || stderr != "" || strings.Contains(out, note) {
						t.Fatalf("%v %q %q", err, out, stderr)
					}
					if format == "text" {
						if !strings.Contains(out, "<unknown>") {
							t.Fatal(out)
						}
						if verb == "get" && strings.Contains(out, "preserved") {
							t.Fatal(out)
						}
						if group == "role" && verb == "get" && !strings.Contains(out, "nodepool.get") {
							t.Fatal(out)
						}
					} else {
						var obj map[string]any
						if e := yaml.Unmarshal([]byte(out), &obj); e != nil {
							t.Fatal(e)
						}
					}
				})
			}
		}
	}
}

func TestFailedMutationsHaveNoSuccessNote(t *testing.T) {
	for _, args := range [][]string{
		{"role", "create", "target", "--permission", "cluster.get"},
		{"role", "delete", "target", "--confirm"},
		{"rolebinding", "create", "target", "--subject", "a@b", "--platform-role", "cluster-admin"},
		{"rolebinding", "delete", "target", "--confirm"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			calls := 0
			out, stderr, err := execute(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(403)
				fmt.Fprint(w, "<html>SECRET policy prose</html>")
			}, args...)
			if err == nil || calls != 1 || out != "" || stderr != "" || strings.Contains(err.Error(), "SECRET") {
				t.Fatalf("%v calls=%d out=%q stderr=%q", err, calls, out, stderr)
			}
		})
	}
}

func TestEmptyLists(t *testing.T) {
	for _, group := range []string{"role", "rolebinding"} {
		for _, format := range []string{"text", "json", "yaml"} {
			t.Run(group+format, func(t *testing.T) {
				kind := "RoleList"
				if group == "rolebinding" {
					kind = "RoleBindingList"
				}
				out, stderr, err := execute(t, func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprintf(w, `{"apiVersion":%q,"kind":%q,"items":null}`, apiVersion, kind)
				}, group, "list", "-o", format)
				if err != nil || stderr != "" {
					t.Fatal(err, stderr)
				}
				if format == "text" {
					if !strings.Contains(out, "NAME") {
						t.Fatal(out)
					}
				} else {
					var obj map[string]any
					if e := yaml.Unmarshal([]byte(out), &obj); e != nil {
						t.Fatal(e)
					}
					items, ok := obj["items"].([]any)
					if !ok || len(items) != 0 {
						t.Fatal(obj)
					}
				}
			})
		}
	}
}

func TestDetailOutputKeepsInternalMetadataStructuredOnly(t *testing.T) {
	for _, group := range []string{"role", "rolebinding"} {
		for _, verb := range []string{"get", "create", "update"} {
			if group == "rolebinding" && verb == "update" {
				continue
			}
			for _, format := range []string{"text", "json", "yaml"} {
				t.Run(group+verb+format, func(t *testing.T) {
					kind := "Role"
					spec := `"permissions":["cluster.get","nodepool.list"]`
					if group == "rolebinding" {
						kind = "RoleBinding"
						spec = `"subject":"Alice@example.com","roleRef":{"kind":"Role","name":"limited","apiGroup":"gcp.managed.openshift.io"}`
					}
					args := []string{group, verb, "item", "-o", format}
					if verb != "get" {
						if group == "role" {
							args = append(args, "--permission", "cluster.get")
						} else {
							args = append(args, "--subject", "Alice@example.com", "--role", "limited")
						}
					}
					out, _, err := execute(t, func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						fmt.Fprintf(w, `{"apiVersion":%q,"kind":%q,"metadata":{"name":"item","namespace":"customer","uid":"internal-uid","resourceVersion":"42","generation":3,"labels":{"internal-label":"yes"},"annotations":{"internal-annotation":"yes"},"creationTimestamp":"2026-10-09T10:00:00Z"},"spec":{%s}}`, apiVersion, kind, spec)
					}, args...)
					if err != nil {
						t.Fatal(err)
					}
					if format == "text" {
						for _, hidden := range []string{"ID:", "Resource Version:", "API Group:", "internal-uid", "internal-label", "internal-annotation", "generation", "creationTimestamp", "apiGroup", "gcp.managed.openshift.io", "{"} {
							if strings.Contains(out, hidden) {
								t.Fatalf("unexpected %q in %s", hidden, out)
							}
						}
						for _, visible := range []string{"Name: item", "Namespace: customer", "Age:"} {
							if !strings.Contains(out, visible) {
								t.Fatalf("missing %q: %s", visible, out)
							}
						}
						if group == "role" {
							if !strings.Contains(out, "Permissions:") || !strings.Contains(out, "cluster.get") || !strings.Contains(out, "nodepool.list") {
								t.Fatal(out)
							}
						} else {
							for _, visible := range []string{"Subject: Alice@example.com", "Role Kind: Role", "Role Name: limited"} {
								if !strings.Contains(out, visible) {
									t.Fatal(out)
								}
							}
						}
					} else {
						var obj map[string]any
						if e := yaml.Unmarshal([]byte(out), &obj); e != nil {
							t.Fatal(e)
						}
						metadata := obj["metadata"].(map[string]any)
						if metadata["uid"] != "internal-uid" || metadata["resourceVersion"] != "42" || metadata["generation"] != float64(3) || metadata["creationTimestamp"] != "2026-10-09T10:00:00Z" || metadata["labels"] == nil || metadata["annotations"] == nil {
							t.Fatal(metadata)
						}
						if group == "rolebinding" && obj["spec"].(map[string]any)["roleRef"].(map[string]any)["apiGroup"] != "gcp.managed.openshift.io" {
							t.Fatal(obj)
						}
					}
				})
			}
		}
	}
}
