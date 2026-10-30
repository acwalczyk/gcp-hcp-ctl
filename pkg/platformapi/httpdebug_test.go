package platformapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/auth"
	"github.com/openshift-online/gcp-hcp-ctl/pkg/httpdebug"
	"k8s.io/client-go/rest"
)

func TestHTTPTraceIsInvocationScoped(t *testing.T) {
	const patch = `{"metadata":{"labels":{"trace-test":"private-payload"}}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != patch {
			t.Errorf("request body = %q, error = %v", body, err)
		}
		if r.Method != http.MethodPatch || r.URL.Path != "/apis/gcp.managed.openshift.io/v1/namespaces/test-project/nodepools/test-pool" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, nodePoolJSON)
	}))
	defer server.Close()
	client, err := NewClientForTest(server.URL, "test-project")
	if err != nil {
		t.Fatal(err)
	}
	var trace bytes.Buffer
	for _, enabled := range []bool{false, true, false} {
		before := trace.Len()
		ctx := context.Background()
		if enabled {
			ctx = httpdebug.WithTrace(ctx, &trace, 6)
		}
		got, err := client.NodePools().Patch(ctx, client.Namespace(), "test-pool", []byte(patch))
		if err != nil || got.Name != "test-pool" {
			t.Fatalf("patch response = %+v, error = %v", got, err)
		}
		if !enabled && trace.Len() != before {
			t.Fatal("untraced invocation wrote to previous trace")
		}
		if enabled && (!strings.Contains(trace.String(), `method="PATCH"`) || !strings.Contains(trace.String(), "status=200")) {
			t.Fatalf("missing request/response trace: %q", trace.String())
		}
	}
	if strings.Contains(trace.String(), "private-payload") || strings.Contains(trace.String(), nodePoolJSON) {
		t.Fatalf("trace contains payload: %q", trace.String())
	}
}

type platformTraceTokenSource struct{ err error }

func (s platformTraceTokenSource) Token(context.Context) (string, string, error) {
	return "fake-platform-token", "", s.err
}

func TestPlatformAPITraceRunsAfterAuthentication(t *testing.T) {
	for _, tokenFails := range []bool{false, true} {
		name := "authenticated"
		if tokenFails {
			name = "token failure"
		}
		t.Run(name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Header.Get("Authorization") != "Bearer fake-platform-token" {
					t.Error("authentication header changed")
				}
				w.Header().Set("Content-Type", "application/json")
				io.WriteString(w, clusterJSON)
			}))
			defer server.Close()
			previous := http.DefaultTransport
			http.DefaultTransport = server.Client().Transport
			t.Cleanup(func() { http.DefaultTransport = previous })
			// No ADC or gcloud lookup: replace the unused source before any request.
			client, err := NewClient(server.URL, "test-project", &auth.TokenSource{})
			if err != nil {
				t.Fatal(err)
			}
			transport, ok := client.restClient.(*rest.RESTClient).Client.Transport.(*tokenTransport)
			if !ok {
				t.Fatal("authentication must remain outside the trace transport")
			}
			var tokenErr error
			if tokenFails {
				tokenErr = errors.New("private-token-failure")
			}
			transport.tokenSource = platformTraceTokenSource{err: tokenErr}
			var trace bytes.Buffer
			got, err := client.Clusters().Get(httpdebug.WithTrace(t.Context(), &trace, 6), client.Namespace(), "test-cluster")
			if tokenFails {
				if err == nil || err.Error() != "request failed" || requests.Load() != 0 || trace.Len() != 0 {
					t.Fatalf("pre-auth failure: error=%v, sent=%d, trace=%q", err, requests.Load(), trace.String())
				}
				return
			}
			if err != nil || got.Name != "test-cluster" || requests.Load() != 1 || strings.Count(trace.String(), " start ") != 1 || !strings.Contains(trace.String(), "status=200") {
				t.Fatalf("authenticated response=%+v, error=%v, sent=%d, trace=%q", got, err, requests.Load(), trace.String())
			}
			if strings.Contains(trace.String(), "fake-platform-token") || strings.Contains(trace.String(), "Authorization") || strings.Contains(trace.String(), clusterJSON) {
				t.Fatalf("trace contains headers or body: %q", trace.String())
			}
		})
	}
}
