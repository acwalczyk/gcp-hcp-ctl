package httpdebug

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
)

func TestTraceInvocationIsolation(t *testing.T) {
	var first, second bytes.Buffer
	firstCtx := WithTrace(context.Background(), &first, 6)
	secondCtx := WithTrace(firstCtx, &second, 6)
	transport := Wrap(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	}))
	for _, tc := range []struct {
		ctx  context.Context
		path string
	}{
		{firstCtx, "/first"},
		{context.Background(), "/disabled"},
		{WithTrace(firstCtx, nil, 0), "/explicitly-disabled"},
		{secondCtx, "/second"},
	} {
		req, _ := http.NewRequestWithContext(tc.ctx, http.MethodGet, "https://example.com"+tc.path, nil)
		if _, err := transport.RoundTrip(req); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		trace string
		path  string
	}{
		{first.String(), "/first"},
		{second.String(), "/second"},
	} {
		if strings.Count(tc.trace, "\n") != 2 || !strings.Contains(tc.trace, tc.path) || strings.Count(tc.trace, "request=1 ") != 2 {
			t.Errorf("invocation state leaked: %s", tc.trace)
		}
	}
}

func TestTraceConcurrentRequestsHaveMatchedLines(t *testing.T) {
	var trace bytes.Buffer
	ctx := WithTrace(context.Background(), &trace, 6)
	transport := Wrap(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	}))
	const requests = 40
	var wg sync.WaitGroup
	for i := range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://example.com/%d", i), nil)
			if _, err := transport.RoundTrip(req); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSpace(trace.String()), "\n")
	if len(lines) != requests*2 {
		t.Fatalf("got %d lines; want %d", len(lines), requests*2)
	}
	pattern := regexp.MustCompile(`^\S+ http request=([0-9]+) (start|complete) `)
	seen := make(map[string]map[string]int)
	for _, line := range lines {
		match := pattern.FindStringSubmatch(line)
		if match == nil {
			t.Fatalf("interleaved or malformed trace: %q", line)
		}
		if seen[match[1]] == nil {
			seen[match[1]] = make(map[string]int)
		}
		seen[match[1]][match[2]]++
	}
	if len(seen) != requests {
		t.Fatalf("got %d unique request IDs; want %d", len(seen), requests)
	}
	for id, events := range seen {
		if events["start"] != 1 || events["complete"] != 1 {
			t.Errorf("request %s has unmatched events: %v", id, events)
		}
	}
}

type untouchedBody struct {
	t *testing.T
}

func (b untouchedBody) Read([]byte) (int, error) {
	b.t.Error("tracer must not read a streaming body")
	return 0, io.EOF
}

func (b untouchedBody) Close() error {
	b.t.Error("tracer must not close a body")
	return nil
}

func TestTraceLeavesStreamingBodiesUntouched(t *testing.T) {
	var trace bytes.Buffer
	body := untouchedBody{t}
	req, _ := http.NewRequestWithContext(WithTrace(context.Background(), &trace, 6), http.MethodPost, "https://example.com/stream", body)
	response := &http.Response{StatusCode: 200, Body: body}
	got, err := Wrap(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Body != body {
			t.Error("request body was replaced")
		}
		return response, nil
	})).RoundTrip(req)
	if err != nil || got != response || got.Body != body {
		t.Fatalf("tracing changed streaming response: %v, %v", got, err)
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) { return 0, errors.New("trace output unavailable") }

func TestTraceWriterFailureDoesNotChangeAPICall(t *testing.T) {
	req, _ := http.NewRequestWithContext(WithTrace(context.Background(), failedWriter{}, 6), http.MethodGet, "https://example.com", nil)
	response := &http.Response{StatusCode: 200, Body: http.NoBody}
	got, err := Wrap(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return response, nil
	})).RoundTrip(req)
	if err != nil || got != response {
		t.Fatal("logging failure changed API result")
	}
}

func TestTraceRedirectAttemptsAndDefaultTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/first" {
			http.Redirect(w, r, "/second?token=private-token", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	var trace bytes.Buffer
	req, _ := http.NewRequestWithContext(WithTrace(context.Background(), &trace, 6), http.MethodGet, server.URL+"/first", nil)
	client := &http.Client{Transport: Wrap(nil)}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 204 || strings.Count(trace.String(), "start") != 2 || strings.Count(trace.String(), "complete") != 2 || strings.Contains(trace.String(), "private-token") {
		t.Fatalf("missing or unsafe redirect trace: %s", trace.String())
	}
}
