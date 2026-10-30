package discovery

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/httpdebug"
)

func TestHTTPFetcherTraceIsInvocationScoped(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/manifest" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected discovery request: %s %s, authenticated=%v", r.Method, r.URL.Path, r.Header.Get("Authorization") != "")
		}
		io.WriteString(w, `{"schema_version":"v1","environment":"private-environment","regions":{}}`)
	}))
	defer server.Close()
	// Exercise the constructor's real transport hook with local TLS only.
	previous := http.DefaultTransport
	http.DefaultTransport = server.Client().Transport
	t.Cleanup(func() { http.DefaultTransport = previous })
	fetcher := NewHTTPFetcher(0)
	var trace bytes.Buffer
	for _, enabled := range []bool{false, true, false} {
		before := trace.Len()
		ctx := context.Background()
		if enabled {
			ctx = httpdebug.WithTrace(ctx, &trace, 6)
		}
		manifest, err := fetcher.Fetch(ctx, server.URL+"/manifest")
		if err != nil || manifest.Environment != "private-environment" {
			t.Fatalf("manifest = %+v, error = %v", manifest, err)
		}
		if !enabled && trace.Len() != before {
			t.Fatal("untraced invocation wrote to previous trace")
		}
		if enabled && (!strings.Contains(trace.String(), `method="GET"`) || !strings.Contains(trace.String(), "status=200")) {
			t.Fatalf("missing request/response trace: %q", trace.String())
		}
	}
	if strings.Contains(trace.String(), "private-environment") {
		t.Fatalf("trace contains manifest body: %q", trace.String())
	}
}
