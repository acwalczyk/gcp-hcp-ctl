package platformapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	gcpv1 "github.com/openshift-online/gecko/platform-api/api/public/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func accessOperation(ctx context.Context, c *Client, resource, verb string) error {
	if resource == "roles" {
		r := &gcpv1.Role{TypeMeta: metav1.TypeMeta{APIVersion: gcpv1.GroupVersion.String(), Kind: "Role"}, ObjectMeta: metav1.ObjectMeta{Name: "target", Namespace: "customer"}, Spec: gcpv1.RoleSpec{Permissions: []string{"cluster.get"}}}
		switch verb {
		case "POST":
			_, err := c.Roles().Create(ctx, "customer", r)
			return err
		case "PUT":
			_, err := c.Roles().Update(ctx, "customer", "target", r)
			return err
		case "DELETE":
			return c.Roles().Delete(ctx, "customer", "target")
		default:
			_, err := c.Roles().Get(ctx, "customer", "target")
			return err
		}
	}
	r := &gcpv1.RoleBinding{TypeMeta: metav1.TypeMeta{APIVersion: gcpv1.GroupVersion.String(), Kind: "RoleBinding"}, ObjectMeta: metav1.ObjectMeta{Name: "target", Namespace: "customer"}}
	switch verb {
	case "POST":
		_, err := c.RoleBindings().Create(ctx, "customer", r)
		return err
	case "DELETE":
		return c.RoleBindings().Delete(ctx, "customer", "target")
	default:
		_, err := c.RoleBindings().Get(ctx, "customer", "target")
		return err
	}
}

func TestAccessErrors(t *testing.T) {
	for _, resource := range []string{"roles", "rolebindings"} {
		for _, verb := range []string{"GET", "POST", "PUT", "DELETE"} {
			if resource == "rolebindings" && verb == "PUT" {
				continue
			}
			for _, status := range []int{400, 401, 403, 404, 409, 422, 429, 500, 503} {
				t.Run(fmt.Sprintf("%s/%s/%d", resource, verb, status), func(t *testing.T) {
					calls := 0
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						calls++
						path := "/apis/gcp.managed.openshift.io/v1/namespaces/customer/" + resource
						if verb != "POST" {
							path += "/target"
						}
						if r.Method != verb || r.URL.Path != path {
							t.Errorf("%s %s", r.Method, r.URL.Path)
						}
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(status)
						fmt.Fprint(w, `{"kind":"Status","apiVersion":"v1","message":"SECRET server Cedar policy explanation"}`)
					}))
					defer server.Close()
					c, err := NewClientForTest(server.URL, "customer")
					if err != nil {
						t.Fatal(err)
					}
					err = accessOperation(t.Context(), c, resource, verb)
					if err == nil || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "Cedar") {
						t.Fatalf("unsafe error: %v", err)
					}
					var httpErr *HTTPError
					if !errors.As(err, &httpErr) || httpErr.StatusCode() != status {
						t.Fatalf("status missing: %v", err)
					}
					if verb == http.MethodPut {
						wantUncertain := status >= http.StatusInternalServerError
						if IsUncertainOutcome(err) != wantUncertain || strings.Contains(err.Error(), checkBeforeRetrying) != wantUncertain {
							t.Fatalf("PUT status %d: uncertainty=%v error=%v", status, IsUncertainOutcome(err), err)
						}
					}
					if calls != 1 {
						t.Fatalf("repeated %s: %d", verb, calls)
					}
				})
			}
		}
	}
}

func TestAccessInvalidSuccessAndCancellation(t *testing.T) {
	for _, resource := range []string{"roles", "rolebindings"} {
		for _, body := range []string{`not JSON`, `{"apiVersion":"gcp.managed.openshift.io/v1","kind":"Cluster"}`, `<html>SECRET</html>`} {
			t.Run(resource+body, func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, body)
				}))
				defer server.Close()
				c, err := NewClientForTest(server.URL, "customer")
				if err != nil {
					t.Fatal(err)
				}
				err = accessOperation(t.Context(), c, resource, "POST")
				if err == nil || strings.Contains(err.Error(), "SECRET") {
					t.Fatalf("%v", err)
				}
				if !strings.Contains(err.Error(), "before retrying") {
					t.Fatalf("missing uncertain outcome guidance: %v", err)
				}
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				err = accessOperation(ctx, c, resource, "GET")
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("%v", err)
				}
			})
		}
	}
}

func TestAccessListPagination(t *testing.T) {
	for _, resource := range []string{"roles", "rolebindings"} {
		t.Run(resource, func(t *testing.T) {
			calls := 0
			kind := "RoleList"
			if resource == "rolebindings" {
				kind = "RoleBindingList"
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				token := "next"
				name := "first"
				if calls == 2 {
					if r.URL.Query().Get("continue") != "next" {
						t.Error(r.URL)
					}
					token = ""
					name = "second"
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"apiVersion":"gcp.managed.openshift.io/v1","kind":%q,"metadata":{"continue":%q,"resourceVersion":"5"},"items":[{"metadata":{"name":%q}}]}`, kind, token, name)
			}))
			defer server.Close()
			c, err := NewClientForTest(server.URL, "customer")
			if err != nil {
				t.Fatal(err)
			}
			if resource == "roles" {
				list, err := c.Roles().List(t.Context(), "customer")
				if err != nil || len(list.Items) != 2 || list.Continue != "" || list.ResourceVersion != "5" {
					t.Fatalf("%v %v", list, err)
				}
			} else {
				list, err := c.RoleBindings().List(t.Context(), "customer")
				if err != nil || len(list.Items) != 2 || list.Continue != "" || list.ResourceVersion != "5" {
					t.Fatalf("%v %v", list, err)
				}
			}
			if calls != 2 {
				t.Fatal(calls)
			}
		})
	}
}

func TestAccessListRejectsRepeatedPaginationToken(t *testing.T) {
	for _, resource := range []string{"roles", "rolebindings"} {
		t.Run(resource, func(t *testing.T) {
			calls := 0
			kind := "RoleList"
			if resource == "rolebindings" {
				kind = "RoleBindingList"
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"apiVersion":"gcp.managed.openshift.io/v1","kind":%q,"metadata":{"continue":"same"},"items":[]}`, kind)
			}))
			defer server.Close()
			c, err := NewClientForTest(server.URL, "customer")
			if err != nil {
				t.Fatal(err)
			}
			if resource == "roles" {
				_, err = c.Roles().List(t.Context(), "customer")
			} else {
				_, err = c.RoleBindings().List(t.Context(), "customer")
			}
			if err == nil || calls != 2 {
				t.Fatalf("%v calls=%d", err, calls)
			}
		})
	}
}

func TestRoleUpdateAmbiguousResponses(t *testing.T) {
	for _, body := range []string{`not JSON`, `{"apiVersion":"gcp.managed.openshift.io/v1","kind":"Cluster"}`, `<html>SECRET</html>`} {
		t.Run(body, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			c, err := NewClientForTest(server.URL, "customer")
			if err != nil {
				t.Fatal(err)
			}
			err = accessOperation(t.Context(), c, "roles", http.MethodPut)
			if !IsUncertainOutcome(err) || !strings.Contains(err.Error(), checkBeforeRetrying) || strings.Contains(err.Error(), "SECRET") || calls != 1 {
				t.Fatalf("error=%v uncertainty=%v calls=%d", err, IsUncertainOutcome(err), calls)
			}
		})
	}
}

func TestRoleUpdateDroppedConnection(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	}))
	defer server.Close()
	c, err := NewClientForTest(server.URL, "customer")
	if err != nil {
		t.Fatal(err)
	}
	err = accessOperation(t.Context(), c, "roles", http.MethodPut)
	if !IsUncertainOutcome(err) || !strings.Contains(err.Error(), checkBeforeRetrying) || calls.Load() != 1 {
		t.Fatalf("error=%v uncertainty=%v calls=%d", err, IsUncertainOutcome(err), calls.Load())
	}
}

func TestAccessCreateRejectsNilResource(t *testing.T) {
	for _, resource := range []string{"roles", "rolebindings"} {
		t.Run(resource, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				calls++
			}))
			defer server.Close()

			c, err := NewClientForTest(server.URL, "customer")
			if err != nil {
				t.Fatal(err)
			}
			if resource == "roles" {
				_, err = c.Roles().Create(t.Context(), "customer", nil)
			} else {
				_, err = c.RoleBindings().Create(t.Context(), "customer", nil)
			}
			if err == nil || calls != 0 {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
		})
	}
}
