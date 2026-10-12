package platformapi

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/auth"
)

func TestGcloudAuthenticationFailureHasActionablePlatformAPIMessage(t *testing.T) {
	var requests atomic.Int32
	cfg := clientRESTConfig("https://example.invalid")
	cfg.Transport = countingTransport{requests: &requests}
	cfg.WrapTransport = func(rt http.RoundTripper) http.RoundTripper {
		return &tokenTransport{
			base:        rt,
			tokenSource: failingTokenSource{err: auth.ErrGcloudAuthentication},
		}
	}
	client := testRESTClientForConfig(t, cfg)

	_, err := client.Clusters().List(context.Background(), "test-project")
	if err == nil || err.Error() != "not authenticated; run gcloud auth login" {
		t.Fatalf("error = %v, want actionable gcloud authentication message", err)
	}
	var transportErr *TransportError
	if !errors.As(err, &transportErr) {
		t.Fatalf("error = %T, want TransportError", err)
	}
	if !errors.Is(err, auth.ErrGcloudAuthentication) {
		t.Fatalf("error = %v does not retain ErrGcloudAuthentication", err)
	}
	if requests.Load() != 0 {
		t.Errorf("base transport requests = %d, want 0", requests.Load())
	}
}
