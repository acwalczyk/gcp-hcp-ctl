package workflows

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/httpdebug"
	"golang.org/x/oauth2"
)

type workflowTestTransport func(*http.Request) (*http.Response, error)

func (f workflowTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type workflowTestTokenSource struct{ err error }

func (s workflowTestTokenSource) Token() (*oauth2.Token, error) {
	return &oauth2.Token{AccessToken: "fake-workflow-token", TokenType: "Bearer"}, s.err
}

func installWorkflowTestClient(t *testing.T, server *httptest.Server, tokenErr, clientErr error) {
	t.Helper()
	localURL, _ := url.Parse(server.URL)
	previous := newWorkflowHTTPClient
	newWorkflowHTTPClient = func(_ context.Context, scopes ...string) (*http.Client, error) {
		if !reflect.DeepEqual(scopes, []string{"https://www.googleapis.com/auth/cloud-platform"}) {
			t.Errorf("unexpected scopes: %v", scopes)
		}
		if clientErr != nil {
			return nil, clientErr
		}
		return &http.Client{Transport: &oauth2.Transport{
			Source: workflowTestTokenSource{err: tokenErr},
			Base: workflowTestTransport(func(req *http.Request) (*http.Response, error) {
				if req.URL.Host != "workflowexecutions.googleapis.com" && req.URL.Host != "us-central1-cloudresourcemanager.googleapis.com" {
					return nil, fmt.Errorf("unexpected host %q", req.URL.Host)
				}
				req = req.Clone(req.Context())
				req.URL.Scheme, req.URL.Host = localURL.Scheme, localURL.Host
				return server.Client().Transport.RoundTrip(req)
			}),
		}}, nil
	}
	t.Cleanup(func() { newWorkflowHTTPClient = previous })
}

const workflowExecution = "projects/test-project/locations/us-central1/workflows/test-workflow/executions/test-execution"
const workflowCallback = workflowExecution + "/callbacks/private-callback-token"

func workflowTraceOperations() []struct {
	name     string
	attempts int
	call     func(context.Context) error
} {
	client := &Client{Project: "test-project", Region: "us-central1"}
	return []struct {
		name     string
		attempts int
		call     func(context.Context) error
	}{
		{"list callbacks", 1, func(ctx context.Context) error {
			got, err := client.ListCallbacks(ctx, workflowExecution)
			if err != nil {
				return err
			}
			want := []CallbackInfo{{Name: workflowCallback, Method: "POST", URL: callbacksAPIBase + "/" + workflowCallback}}
			if !reflect.DeepEqual(got, want) {
				return fmt.Errorf("callbacks = %+v, want %+v", got, want)
			}
			return nil
		}},
		{"trigger callback", 1, func(ctx context.Context) error {
			return client.TriggerCallback(ctx, callbacksAPIBase+"/"+workflowCallback, "", map[string]interface{}{"message": "private-callback-payload"})
		}},
		{"single tag check", 1, func(ctx context.Context) error {
			if !CheckPamGatedTag(ctx, "test-project", "us-central1", "test-workflow") {
				return errors.New("expected pam-gated tag")
			}
			return nil
		}},
		{"concurrent tag checks", 2, func(ctx context.Context) error {
			got := client.fetchPamGatedFlags(ctx, []string{
				"projects/test-project/locations/us-central1/workflows/first",
				"projects/test-project/locations/us-central1/workflows/second",
			})
			if !reflect.DeepEqual(got, []bool{true, true}) {
				return fmt.Errorf("pam-gated flags = %v", got)
			}
			return nil
		}},
	}
}

func TestWorkflowRESTTraceHooks(t *testing.T) {
	for _, op := range workflowTraceOperations() {
		t.Run(op.name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Header.Get("Authorization") != "Bearer fake-workflow-token" {
					t.Error("authentication header changed")
				}
				switch {
				case r.URL.Path == "/v1/"+workflowExecution+"/callbacks":
					if r.Method != http.MethodGet {
						t.Errorf("callback list method = %s", r.Method)
					}
					fmt.Fprintf(w, `{"callbacks":[{"name":%q,"method":"POST"}]}`, workflowCallback)
				case r.URL.Path == "/v1/"+workflowCallback:
					body, err := io.ReadAll(r.Body)
					if err != nil || string(body) != `{"message":"private-callback-payload"}` || r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
						t.Errorf("callback request changed: method=%s body=%q error=%v", r.Method, body, err)
					}
					w.WriteHeader(http.StatusNoContent)
				case r.URL.Path == "/v3/tagBindings":
					if r.Method != http.MethodGet || !strings.HasPrefix(r.URL.Query().Get("parent"), "//workflows.googleapis.com/projects/test-project/locations/us-central1/workflows/") {
						t.Errorf("tag request changed: %s %s", r.Method, r.URL)
					}
					io.WriteString(w, `{"tagBindings":[{"tagValueNamespacedName":"private-org/pam-gated/true"}]}`)
				default:
					t.Errorf("unexpected request: %s", r.URL)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			installWorkflowTestClient(t, server, nil, nil)
			var trace bytes.Buffer
			for _, enabled := range []bool{false, true, false} {
				before := trace.Len()
				ctx := context.Background()
				if enabled {
					ctx = httpdebug.WithTrace(ctx, &trace, 6)
				}
				if err := op.call(ctx); err != nil {
					t.Fatal(err)
				}
				if !enabled && trace.Len() != before {
					t.Fatal("untraced invocation wrote to previous trace")
				}
				if enabled && (strings.Count(trace.String(), " start ") != op.attempts || strings.Count(trace.String(), " complete ") != op.attempts) {
					t.Fatalf("missing REST attempt traces: %q", trace.String())
				}
			}
			if got := requests.Load(); got != int32(3*op.attempts) {
				t.Errorf("HTTP requests = %d, want %d", got, 3*op.attempts)
			}
			for _, secret := range []string{"fake-workflow-token", "private-callback-payload", "private-callback-token", "private-org", "Authorization"} {
				if strings.Contains(trace.String(), secret) {
					t.Errorf("trace contains %q: %q", secret, trace.String())
				}
			}
		})
	}
}

func TestWorkflowRESTPreSendFailuresDoNotTrace(t *testing.T) {
	for _, op := range workflowTraceOperations() {
		for _, failure := range []string{"client creation", "token acquisition"} {
			t.Run(op.name+"/"+failure, func(t *testing.T) {
				var requests atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
				}))
				defer server.Close()
				var tokenErr, clientErr error
				if failure == "client creation" {
					clientErr = errors.New("private-client-failure")
				} else {
					tokenErr = errors.New("private-token-failure")
				}
				installWorkflowTestClient(t, server, tokenErr, clientErr)
				var trace bytes.Buffer
				err := op.call(httpdebug.WithTrace(t.Context(), &trace, 6))
				if err == nil || trace.Len() != 0 || requests.Load() != 0 {
					t.Fatalf("pre-send failure: error=%v, trace=%q, sent=%d", err, trace.String(), requests.Load())
				}
			})
		}
	}
}
