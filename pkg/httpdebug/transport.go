// Package httpdebug provides opt-in, invocation-scoped HTTP tracing.
// Higher verbosity levels add redacted headers and supported JSON payloads.
package httpdebug

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

type contextKey struct{}

type tracer struct {
	mu     sync.Mutex
	writer io.Writer
	nextID uint64
	level  int
}

// WithTrace starts a fresh trace for an invocation. A nil writer explicitly
// disables tracing, including any tracer inherited from the parent context.
// Levels below 6 are silent; 7 adds request headers, 8 JSON bodies and response
// headers, 9 curl and timing details, and 10 removes JSON output truncation.
func WithTrace(ctx context.Context, writer io.Writer, verbosity int) context.Context {
	return context.WithValue(ctx, contextKey{}, &tracer{writer: writer, level: verbosity})
}

// Wrap instruments a transport without changing the request or response.
// A nil base uses http.DefaultTransport. Requests without a trace are silent.
// OAuth2 transports are cloned and instrumented below authentication so failed
// token acquisition is never reported as a request sent to the API.
func Wrap(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	if auth, ok := base.(*oauth2.Transport); ok {
		return &oauth2.Transport{Source: auth.Source, Base: Wrap(auth.Base)}
	}
	return &transport{base: base}
}

type transport struct {
	base http.RoundTripper
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	trace, _ := req.Context().Value(contextKey{}).(*tracer)
	if trace == nil || trace.writer == nil || trace.level < 6 {
		return t.base.RoundTrip(req)
	}
	trace.mu.Lock()
	trace.nextID++
	id := trace.nextID
	fmt.Fprintf(trace.writer, "%s http request=%d start method=%q url=%q\n", time.Now().UTC().Format(time.RFC3339Nano), id, req.Method, safeURL(req.URL))
	trace.mu.Unlock()

	if trace.level >= 7 {
		trace.log(id, "request_headers", "headers=%s", headerText(req.Header))
	}
	if trace.level >= 8 {
		trace.requestBody(id, req)
	}
	var timing *detailedTiming
	if trace.level >= 9 {
		trace.log(id, "curl", "command=%q", curlCommand(req))
	}
	start := time.Now()
	if trace.level >= 9 {
		timing = &detailedTiming{start: start}
		// WithClientTrace composes existing hooks. Clone just the request context
		// so instrumentation never mutates the caller's request.
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), timing.clientTrace()))
	}

	var resp *http.Response
	err := context.Cause(req.Context())
	if err == nil {
		resp, err = t.base.RoundTrip(req)
	} else if req.Body != nil {
		// We did not transfer the request to base. Honor RoundTripper's body
		// ownership even on an abort, without making cancellation wait for a
		// custom Close. Snapshot cleanup owns only the independent copy.
		go req.Body.Close()
	}
	elapsed := time.Since(start)
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	if err != nil {
		trace.log(id, "complete", "status=%d elapsed=%s error=%s", status, elapsed, errorLabel(err))
	} else {
		trace.log(id, "complete", "status=%d elapsed=%s", status, elapsed)
	}
	if timing != nil {
		timing.log(trace, id)
	}
	if resp != nil && err == nil && trace.level >= 8 {
		trace.log(id, "response_headers", "headers=%s", headerText(resp.Header))
		trace.responseBody(id, req, resp)
	}
	return resp, err
}

func (t *tracer) log(id uint64, event, format string, args ...any) {
	line := fmt.Sprintf("%s http request=%d %s %s\n", time.Now().UTC().Format(time.RFC3339Nano), id, event, fmt.Sprintf(format, args...))
	t.mu.Lock()
	defer t.mu.Unlock()
	// A logging failure must not turn a successful API write into a failure.
	io.WriteString(t.writer, line)
}
