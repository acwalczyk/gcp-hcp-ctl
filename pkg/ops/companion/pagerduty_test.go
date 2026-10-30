package companion

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/httpdebug"
)

type pagerDutyTestTransport func(*http.Request) (*http.Response, error)

func (f pagerDutyTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestPagerDutyTraceIsInvocationScoped(t *testing.T) {
	t.Setenv("PAGERDUTY_TOKEN", "fake-pagerduty-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Token token=fake-pagerduty-token" {
			t.Errorf("unexpected method or authentication")
		}
		switch r.URL.Path {
		case "/incidents/P123":
			io.WriteString(w, `{"incident":{"id":"P123","title":"private-incident-title"}}`)
		case "/incidents/P123/alerts":
			io.WriteString(w, `{"alerts":[{"summary":"private-alert-summary"}]}`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	localURL, _ := url.Parse(server.URL)
	previous := http.DefaultTransport
	// Route only the expected API host to a real local server. Never dial PagerDuty.
	http.DefaultTransport = pagerDutyTestTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "api.pagerduty.com" {
			return nil, fmt.Errorf("unexpected host %q", req.URL.Host)
		}
		req = req.Clone(req.Context())
		req.URL.Scheme, req.URL.Host = localURL.Scheme, localURL.Host
		return server.Client().Transport.RoundTrip(req)
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	client, err := NewPDClient()
	if err != nil {
		t.Fatal(err)
	}
	var trace bytes.Buffer
	var wantSummary string
	for _, enabled := range []bool{false, true, false} {
		before := trace.Len()
		ctx := context.Background()
		if enabled {
			ctx = httpdebug.WithTrace(ctx, &trace, 6)
		}
		got, err := client.FetchIncidentContext(ctx, "P123")
		if err != nil {
			t.Fatal(err)
		}
		if wantSummary == "" {
			wantSummary = got.Summary
		}
		if got.IncidentID != "P123" || got.Summary != wantSummary || !strings.Contains(got.Summary, "private-alert-summary") {
			t.Fatalf("incident response changed: %+v", got)
		}
		if !enabled && trace.Len() != before {
			t.Fatal("untraced invocation wrote to previous trace")
		}
		if enabled && (strings.Count(trace.String(), " start ") != 2 || strings.Count(trace.String(), "status=200") != 2) {
			t.Fatalf("missing incident/alerts request traces: %q", trace.String())
		}
	}
	for _, secret := range []string{"fake-pagerduty-token", "private-incident-title", "private-alert-summary", "Authorization"} {
		if strings.Contains(trace.String(), secret) {
			t.Errorf("trace contains %q: %q", secret, trace.String())
		}
	}
}
