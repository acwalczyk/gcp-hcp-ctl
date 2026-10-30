package cloudrun

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/httpdebug"
)

type traceTokenSource struct{ err error }

func (s traceTokenSource) Token(context.Context) (string, string, error) {
	return "fake-identity-token", "", s.err
}

// Exercise getHTTPClient's production wiring without reading operator credentials
// or invoking ADC/gcloud. Token acquisition is replaced before any request.
func traceTestClient(t *testing.T, server *httptest.Server, tokenErr error) *Client {
	t.Helper()
	adcPath := filepath.Join(t.TempDir(), "fake-adc.json")
	if err := os.WriteFile(adcPath, []byte(`{"type":"service_account"}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", adcPath)
	previous := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	client := &Client{}
	httpClient, err := client.getHTTPClient(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	httpClient.Transport.(*cloudRunTransport).tokenSource = traceTokenSource{err: tokenErr}
	return client
}

func TestCloudRunHTTPTraceIsInvocationScoped(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != `{"query":"private-query"}` {
			t.Errorf("request body = %q, error = %v", body, err)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/diagnose" || r.Header.Get("Authorization") != "Bearer fake-identity-token" {
			t.Error("request method, path, or authentication changed")
		}
		io.WriteString(w, `{"status":"success","diagnosis":{"root_cause":"private-diagnosis"}}`)
	}))
	defer server.Close()
	client := traceTestClient(t, server, nil)
	var trace bytes.Buffer
	for _, enabled := range []bool{false, true, false} {
		before := trace.Len()
		ctx := context.Background()
		if enabled {
			ctx = httpdebug.WithTrace(ctx, &trace, 6)
		}
		got, err := client.Diagnose(ctx, server.URL, "private-query")
		if err != nil || got.Diagnosis.RootCause != "private-diagnosis" {
			t.Fatalf("diagnose response = %+v, error = %v", got, err)
		}
		if !enabled && trace.Len() != before {
			t.Fatal("untraced invocation wrote to previous trace")
		}
		if enabled && (!strings.Contains(trace.String(), `method="POST"`) || !strings.Contains(trace.String(), "status=200")) {
			t.Fatalf("missing request/response trace: %q", trace.String())
		}
	}
	for _, secret := range []string{"private-query", "private-diagnosis", "fake-identity-token", "Authorization"} {
		if strings.Contains(trace.String(), secret) {
			t.Errorf("trace contains %q: %q", secret, trace.String())
		}
	}
}

func TestCloudRunPreSendFailuresDoNotTrace(t *testing.T) {
	for _, reason := range []string{"token failure", "HTTP endpoint", "wrong audience"} {
		t.Run(reason, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
			}))
			defer server.Close()
			var tokenErr error
			if reason == "token failure" {
				tokenErr = errors.New("private-token-failure")
			}
			client := traceTestClient(t, server, tokenErr)
			httpClient, err := client.getHTTPClient(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			endpoint := server.URL
			switch reason {
			case "HTTP endpoint":
				endpoint = strings.Replace(endpoint, "https://", "http://", 1)
			case "wrong audience":
				endpoint = "https://127.0.0.1:1"
			}
			var trace bytes.Buffer
			req, err := http.NewRequestWithContext(httpdebug.WithTrace(t.Context(), &trace, 6), http.MethodPost, endpoint, nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := httpClient.Do(req)
			if resp != nil {
				resp.Body.Close()
			}
			if err == nil || trace.Len() != 0 || requests.Load() != 0 {
				t.Fatalf("pre-send failure: error=%v, trace=%q, sent=%d", err, trace.String(), requests.Load())
			}
		})
	}
}
