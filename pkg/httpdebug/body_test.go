package httpdebug

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type snapshotTestBody struct {
	read  func([]byte) (int, error)
	close func() error
}

func (b snapshotTestBody) Read(p []byte) (int, error) { return b.read(p) }
func (b snapshotTestBody) Close() error               { return b.close() }

func TestRequestSnapshotCancellationBoundaries(t *testing.T) {
	for _, stage := range []string{"GetBody", "Read requires Close", "Close"} {
		t.Run(stage, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var trace bytes.Buffer
				ctx, cancel := context.WithCancel(WithTrace(t.Context(), &trace, 9))
				defer cancel()
				const payload = `{"name":"unchanged"}`
				original := &trackedBody{Reader: strings.NewReader(payload)}
				req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.test", original)
				req.Header.Set("Content-Type", "application/json")
				started, release, closed := make(chan struct{}), make(chan struct{}), make(chan struct{})
				var reads, closes, calls atomic.Int32
				var closeOnce sync.Once
				snapshot := snapshotTestBody{
					read: func(p []byte) (int, error) {
						reads.Add(1)
						if stage == "Read requires Close" {
							close(started)
							<-closed
							return 0, io.ErrClosedPipe
						}
						return 0, io.EOF
					},
					close: func() error {
						closes.Add(1)
						if stage == "Close" {
							close(started)
							<-release
						}
						closeOnce.Do(func() { close(closed) })
						return errors.New("private-snapshot-close-error")
					},
				}
				req.GetBody = func() (io.ReadCloser, error) {
					if stage == "GetBody" {
						close(started)
						<-release
					}
					return snapshot, nil
				}
				done := make(chan error, 1)
				go func() {
					_, err := Wrap(roundTripFunc(func(*http.Request) (*http.Response, error) {
						calls.Add(1)
						return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
					})).RoundTrip(req)
					done <- err
				}()
				<-started
				cancel()
				synctest.Wait()
				select {
				case err := <-done:
					if !errors.Is(err, context.Canceled) {
						t.Errorf("cancellation changed: %v", err)
					}
				default:
					t.Errorf("request blocked in diagnostic %s after cancellation", stage)
				}
				if stage == "Read requires Close" {
					select {
					case <-closed:
					default:
						t.Error("cancellation did not close the independent snapshot")
						// Release the pre-fix reader so the RED run also terminates.
						closeOnce.Do(func() { close(closed) })
					}
				}
				close(release)
				synctest.Wait()
				if closes.Load() != 1 || calls.Load() != 0 {
					t.Errorf("snapshot closes=%d, API calls after cancellation=%d", closes.Load(), calls.Load())
				}
				if stage == "GetBody" && reads.Load() != 0 {
					t.Error("read a late snapshot after cancellation")
				}
				// RoundTripper still owns normal request-body cleanup on an abort;
				// it must not consume the original to obtain the diagnostic snapshot.
				got, err := io.ReadAll(original)
				if err != nil || string(got) != payload || req.Body != original || original.closes != 1 {
					t.Errorf("original body lifecycle changed: payload=%q closes=%d error=%v", got, original.closes, err)
				}
				if strings.Contains(trace.String(), "private-") || !strings.Contains(trace.String(), "error=canceled") {
					t.Errorf("unsafe or missing cancellation diagnostic: %s", trace.String())
				}
			})
		})
	}
}

func TestRequestSnapshotAlreadyCanceled(t *testing.T) {
	for _, mode := range []string{"cancel", "deadline", "cause"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var trace bytes.Buffer
				ctx := WithTrace(t.Context(), &trace, 8)
				want := error(context.Canceled)
				switch mode {
				case "deadline":
					var cancel context.CancelFunc
					ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
					defer cancel()
					want = context.DeadlineExceeded
				default:
					var cancel context.CancelCauseFunc
					ctx, cancel = context.WithCancelCause(ctx)
					if mode == "cause" {
						want = errors.New("private-cancellation-cause")
					}
					cancel(want)
				}
				original := &trackedBody{Reader: strings.NewReader(`{"ok":true}`)}
				req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.test", original)
				req.Header.Set("Content-Type", "application/json")
				req.GetBody = func() (io.ReadCloser, error) {
					t.Error("started GetBody with a canceled context")
					return http.NoBody, nil
				}
				_, err := Wrap(roundTripFunc(func(*http.Request) (*http.Response, error) {
					t.Error("started API request with a canceled context")
					return nil, nil
				})).RoundTrip(req)
				synctest.Wait()
				if err != want || original.closes != 1 || strings.Contains(trace.String(), "private-") {
					t.Errorf("cancellation changed: error=%v want=%v closes=%d trace=%s", err, want, original.closes, trace.String())
				}
			})
		})
	}
}

func TestUnsupportedBodiesStayUntouched(t *testing.T) {
	for _, tc := range []struct {
		name, contentType, encoding, query string
		status                             int
	}{
		{"text", "text/plain", "", "", 200},
		{"NDJSON", "application/x-ndjson", "", "", 200},
		{"SSE", "text/event-stream", "", "", 200},
		{"watch", "application/json", "", "?watch=true", 200},
		{"encoded", "application/json", "gzip", "", 200},
		{"error response", "application/json", "", "", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var trace bytes.Buffer
			body := untouchedBody{t}
			req, _ := http.NewRequestWithContext(WithTrace(t.Context(), &trace, 10), http.MethodPost, "https://example.com"+tc.query, body)
			req.Header.Set("Content-Type", tc.contentType)
			req.Header.Set("Content-Encoding", tc.encoding)
			resp, err := Wrap(roundTripFunc(func(sent *http.Request) (*http.Response, error) {
				if sent.Body != body {
					t.Error("streaming request body replaced")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": {tc.contentType}, "Content-Encoding": {tc.encoding}}, Body: body}, nil
			})).RoundTrip(req)
			if err != nil || resp.Body != body {
				t.Fatalf("unsupported response changed: %v", err)
			}
			if strings.Count(trace.String(), "[omitted:") != 2 {
				t.Errorf("missing explicit omissions: %s", trace.String())
			}
		})
	}
}

func TestRequestSnapshotFailureDoesNotChangeAPICall(t *testing.T) {
	for _, failure := range []string{"open", "read"} {
		t.Run(failure, func(t *testing.T) {
			var trace bytes.Buffer
			const payload = `{"name":"unchanged"}`
			req, _ := http.NewRequestWithContext(WithTrace(t.Context(), &trace, 8), http.MethodPost, "https://example.com", strings.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			req.GetBody = func() (io.ReadCloser, error) {
				if failure == "open" {
					return nil, errors.New("private-snapshot-error")
				}
				return io.NopCloser(failingBodyReader{}), nil
			}
			_, err := Wrap(roundTripFunc(func(sent *http.Request) (*http.Response, error) {
				got, readErr := io.ReadAll(sent.Body)
				if readErr != nil || string(got) != payload {
					t.Fatal("request body changed after snapshot error")
				}
				return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
			})).RoundTrip(req)
			if err != nil || !strings.Contains(trace.String(), "[omitted:") || strings.Contains(trace.String(), "private-") {
				t.Fatalf("snapshot error leaked or changed API: %v %s", err, trace.String())
			}
		})
	}
}

func TestRequestSnapshotCleanupPreservesAPIError(t *testing.T) {
	for _, failure := range []string{"none", "open", "read", "close"} {
		t.Run(failure, func(t *testing.T) {
			var trace bytes.Buffer
			const payload = `{"name":"unchanged"}`
			original := &trackedBody{Reader: strings.NewReader(payload)}
			req, _ := http.NewRequestWithContext(WithTrace(t.Context(), &trace, 8), http.MethodPost, "https://example.test", original)
			req.Header.Set("Content-Type", "application/json")
			closes := 0
			snapshotReader := strings.NewReader(payload)
			req.GetBody = func() (io.ReadCloser, error) {
				if failure == "open" {
					return nil, errors.New("private-snapshot-open-error")
				}
				return snapshotTestBody{
					read: func(p []byte) (int, error) {
						if failure == "read" {
							return 0, errors.New("private-snapshot-read-error")
						}
						return snapshotReader.Read(p)
					},
					close: func() error {
						closes++
						if failure == "close" {
							return errors.New("private-snapshot-close-error")
						}
						return nil
					},
				}, nil
			}
			apiErr := errors.New("private-original-api-error")
			_, err := Wrap(roundTripFunc(func(sent *http.Request) (*http.Response, error) {
				if sent.Body != original || original.closes != 0 {
					t.Error("snapshot closed or replaced original request body")
				}
				got, err := io.ReadAll(sent.Body)
				if err != nil || string(got) != payload {
					t.Errorf("snapshot changed API payload: %q %v", got, err)
				}
				sent.Body.Close()
				return nil, apiErr
			})).RoundTrip(req)
			wantCloses := 1
			if failure == "open" {
				wantCloses = 0
			}
			if err != apiErr || closes != wantCloses || original.closes != 1 || strings.Contains(trace.String(), "private-") {
				t.Errorf("changed errors or cleanup: error=%v snapshots=%d original=%d trace=%s", err, closes, original.closes, trace.String())
			}
		})
	}
}

type failingBodyReader struct{}

func (failingBodyReader) Read([]byte) (int, error) { return 0, errors.New("private-read-error") }

type trackedBody struct {
	io.Reader
	closes int
}

func (b *trackedBody) Close() error { b.closes++; return nil }

func TestResponseCaptureLifecycle(t *testing.T) {
	for _, mode := range []string{"EOF", "exact length then close", "partial close", "read error", "capture overflow"} {
		t.Run(mode, func(t *testing.T) {
			var trace bytes.Buffer
			const payload = `{"name":"visible","token":"private-token"}`
			underlying := &trackedBody{Reader: strings.NewReader(payload)}
			if mode == "read error" {
				underlying.Reader = failingBodyReader{}
			}
			observer := &observedBody{source: underlying, trace: &tracer{writer: &trace, level: 8}, id: 1, contentType: "application/json", expected: int64(len(payload)), limit: maxBodyCapture}
			if mode == "capture overflow" {
				observer.limit = 8
			}
			switch mode {
			case "exact length then close":
				data := make([]byte, len(payload))
				if _, err := io.ReadFull(observer, data); err != nil || string(data) != payload {
					t.Fatalf("changed read: %q %v", data, err)
				}
				if trace.Len() != 0 {
					t.Fatal("logged before EOF or Close")
				}
			case "partial close":
				if _, err := observer.Read(make([]byte, 5)); err != nil {
					t.Fatal(err)
				}
			case "read error":
				if _, err := observer.Read(make([]byte, 5)); err == nil || err.Error() != "private-read-error" {
					t.Fatal("lost original read error")
				}
			default:
				data, err := io.ReadAll(observer)
				if err != nil || string(data) != payload {
					t.Fatalf("changed read: %q %v", data, err)
				}
			}
			if err := observer.Close(); err != nil || underlying.closes != 1 {
				t.Fatal("underlying body not closed")
			}
			if strings.Count(trace.String(), " response_body ") != 1 || strings.Contains(trace.String(), "private-") {
				t.Errorf("duplicate or unsafe body log: %s", trace.String())
			}
			wantBody := mode == "EOF" || mode == "exact length then close"
			if strings.Contains(trace.String(), "visible") != wantBody {
				t.Errorf("unexpected body capture: %s", trace.String())
			}
			if observer.data.Len() != 0 {
				t.Fatal("retained body buffer after logging")
			}
		})
	}
}

type blockingBody struct {
	started, closed chan struct{}
	once            sync.Once
}

func (b *blockingBody) Read([]byte) (int, error) {
	close(b.started)
	<-b.closed
	return 0, io.ErrClosedPipe
}
func (b *blockingBody) Close() error { b.once.Do(func() { close(b.closed) }); return nil }

func TestResponseCloseUnblocksConcurrentRead(t *testing.T) {
	var trace bytes.Buffer
	underlying := &blockingBody{started: make(chan struct{}), closed: make(chan struct{})}
	observer := &observedBody{source: underlying, trace: &tracer{writer: &trace, level: 8}, id: 1, contentType: "application/json", expected: -1, limit: maxBodyCapture}
	readDone := make(chan error, 1)
	go func() { _, err := observer.Read(make([]byte, 10)); readDone <- err }()
	<-underlying.started
	closeDone := make(chan error, 1)
	go func() { closeDone <- observer.Close() }()
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close blocked behind Read")
	}
	select {
	case err := <-readDone:
		if err != io.ErrClosedPipe {
			t.Fatal("read error changed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Read did not unblock")
	}
	if strings.Count(trace.String(), " response_body ") != 1 {
		t.Errorf("duplicate logs: %s", trace.String())
	}
}

func TestTracingPreservesHTTPCancellation(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "waiting for headers", true: "reading JSON body"}[streaming], func(t *testing.T) {
			started := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if streaming {
					w.Header().Set("Content-Type", "application/json")
					io.WriteString(w, `{"message":"private-incomplete-body`)
					w.(http.Flusher).Flush()
				}
				close(started)
				<-r.Context().Done()
			}))
			defer server.Close()
			var trace bytes.Buffer
			ctx, cancel := context.WithTimeout(WithTrace(t.Context(), &trace, 9), 3*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
			client := &http.Client{Transport: Wrap(server.Client().Transport)}
			done := make(chan error, 1)
			bodyStarted := make(chan struct{})
			go func() {
				resp, err := client.Do(req)
				if err == nil {
					if streaming {
						// Ensure cancellation happens after headers and an actual
						// partial body read, not merely after the server flushed.
						_, err = io.ReadFull(resp.Body, make([]byte, 5))
						close(bodyStarted)
					}
					if err == nil {
						_, err = io.ReadAll(resp.Body)
					}
					resp.Body.Close()
				}
				done <- err
			}()
			<-started
			if streaming {
				select {
				case <-bodyStarted:
				case <-time.After(2 * time.Second):
					t.Fatal("body read never started")
				}
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("cancellation changed: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("request ignored cancellation")
			}
			if strings.Contains(trace.String(), "private-") {
				t.Fatalf("partial body leaked: %s", trace.String())
			}
		})
	}
}
