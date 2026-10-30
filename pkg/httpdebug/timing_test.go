package httpdebug

import (
	"bytes"
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func timingDuration(t *testing.T, trace, field string) time.Duration {
	t.Helper()
	for _, value := range strings.Fields(trace) {
		if strings.HasPrefix(value, field+"=") {
			d, err := time.ParseDuration(strings.TrimPrefix(value, field+"="))
			if err != nil {
				t.Fatal(err)
			}
			return d
		}
	}
	t.Fatalf("missing %s in %s", field, trace)
	return 0
}

func TestDetailedTimingOverlappingConnectDurations(t *testing.T) {
	for _, second := range []struct{ network, address string }{
		{"tcp", "127.0.0.1:443"},
		{"tcp6", "[::1]:443"},
		{"tcp", "[::1]:443"},
	} {
		t.Run(second.network+"/"+second.address, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				d := &detailedTiming{}
				hooks := d.clientTrace()
				hooks.ConnectStart("tcp", "[::1]:443")
				time.Sleep(120 * time.Millisecond)
				hooks.ConnectStart(second.network, second.address)
				time.Sleep(40 * time.Millisecond)
				hooks.ConnectDone("tcp", "[::1]:443", io.EOF)
				time.Sleep(40 * time.Millisecond)
				hooks.ConnectDone(second.network, second.address, nil)
				// A completion without a matching start must not count again.
				hooks.ConnectDone(second.network, second.address, nil)
				var trace bytes.Buffer
				d.log(&tracer{writer: &trace}, 1)
				want := (120+40)*time.Millisecond + (40+40)*time.Millisecond
				if got := timingDuration(t, trace.String(), "connect"); got != want {
					t.Fatalf("connect=%s, want summed completed attempt durations %s", got, want)
				}
			})
		})
	}
}

func TestDetailedTimingFirstByteCallbackOrder(t *testing.T) {
	for _, early := range []bool{false, true} {
		t.Run(map[bool]string{false: "after write", true: "before write"}[early], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var trace bytes.Buffer
				writes, firstBytes := 0, 0
				ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{
					WroteRequest:         func(httptrace.WroteRequestInfo) { writes++ },
					GotFirstResponseByte: func() { firstBytes++ },
				})
				req, _ := http.NewRequestWithContext(WithTrace(ctx, &trace, 9), http.MethodPost, "https://example.test", strings.NewReader(`{"ok":true}`))
				req.Header.Set("Content-Type", "application/json")
				getBody := req.GetBody
				req.GetBody = func() (io.ReadCloser, error) {
					// Snapshot diagnostics are excluded from transport timing.
					time.Sleep(20 * time.Millisecond)
					return getBody()
				}
				_, err := Wrap(roundTripFunc(func(sent *http.Request) (*http.Response, error) {
					hooks := httptrace.ContextClientTrace(sent.Context())
					time.Sleep(30 * time.Millisecond)
					if early {
						hooks.GotFirstResponseByte()
					} else {
						hooks.WroteRequest(httptrace.WroteRequestInfo{})
					}
					time.Sleep(70 * time.Millisecond)
					if early {
						hooks.WroteRequest(httptrace.WroteRequestInfo{})
					} else {
						hooks.GotFirstResponseByte()
					}
					return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
				})).RoundTrip(req)
				want := 30 * time.Millisecond
				if !early {
					want += 70 * time.Millisecond
				}
				if got := timingDuration(t, trace.String(), "first_byte"); got != want {
					t.Errorf("first_byte=%s, want elapsed from base transport start %s", got, want)
				}
				if err != nil || writes != 1 || firstBytes != 1 {
					t.Errorf("existing hooks changed: error=%v writes=%d firstBytes=%d", err, writes, firstBytes)
				}
			})
		})
	}
}

func TestDetailedTimingHTTP100Continue(t *testing.T) {
	const responseDelay = 30 * time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(responseDelay)
		// Reading the body sends 100 Continue before the client can finish writing.
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			t.Error(err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.ExpectContinueTimeout = 5 * time.Second
	defer base.CloseIdleConnections()
	var trace bytes.Buffer
	var firstAt, wroteAt, got100 atomic.Int64
	started := time.Now()
	ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{
		GotFirstResponseByte: func() { firstAt.Store(time.Since(started).Nanoseconds()) },
		WroteRequest:         func(httptrace.WroteRequestInfo) { wroteAt.Store(time.Since(started).Nanoseconds()) },
		Got100Continue:       func() { got100.Add(1) },
	})
	req, _ := http.NewRequestWithContext(WithTrace(ctx, &trace, 9), http.MethodPost, server.URL, strings.NewReader(`{"ok":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Expect", "100-continue")
	resp, err := (&http.Client{Transport: Wrap(base), Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	got := timingDuration(t, trace.String(), "first_byte")
	// Bound against the observed callback, not a scheduler-sensitive upper limit.
	if got < responseDelay || got > time.Duration(firstAt.Load()) || firstAt.Load() >= wroteAt.Load() || got100.Load() != 1 {
		t.Fatalf("first_byte=%s external=%s write=%s 100-continue=%d", got, time.Duration(firstAt.Load()), time.Duration(wroteAt.Load()), got100.Load())
	}
}

func TestDetailedTimingPreservesExistingTrace(t *testing.T) {
	var trace bytes.Buffer
	calls, writes := 0, 0
	ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{
		DNSStart:     func(httptrace.DNSStartInfo) { calls++ },
		WroteRequest: func(httptrace.WroteRequestInfo) { writes++ },
	})
	req, _ := http.NewRequestWithContext(WithTrace(ctx, &trace, 9), http.MethodGet, "https://example.com", nil)
	originalContext := req.Context()
	_, err := Wrap(roundTripFunc(func(sent *http.Request) (*http.Response, error) {
		hooks := httptrace.ContextClientTrace(sent.Context())
		if hooks == nil || hooks.ConnectStart == nil || hooks.TLSHandshakeStart == nil {
			t.Fatal("missing detailed timing instrumentation")
		}
		hooks.DNSStart(httptrace.DNSStartInfo{Host: "private-dns-detail"})
		hooks.DNSDone(httptrace.DNSDoneInfo{})
		hooks.ConnectStart("tcp", "127.0.0.1:443")
		hooks.ConnectDone("tcp", "127.0.0.1:443", nil)
		hooks.TLSHandshakeStart()
		hooks.TLSHandshakeDone(tls.ConnectionState{}, nil)
		hooks.GotConn(httptrace.GotConnInfo{Reused: true})
		hooks.WroteRequest(httptrace.WroteRequestInfo{})
		hooks.GotFirstResponseByte()
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	})).RoundTrip(req)
	if err != nil || calls != 1 || writes != 1 || req.Context() != originalContext {
		t.Fatalf("existing instrumentation changed: error=%v calls=%d writes=%d", err, calls, writes)
	}
	for _, field := range []string{" timing ", "dns=", "connect=", "tls=", "first_byte=", "connection_reused=true"} {
		if !strings.Contains(trace.String(), field) {
			t.Errorf("missing timing %s: %s", field, trace.String())
		}
	}
	if strings.Contains(trace.String(), "private-") {
		t.Fatal("timing exposed DNS details")
	}
}
