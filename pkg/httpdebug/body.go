package httpdebug

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

// Redaction needs a complete JSON value. Bound the capture at truncated levels;
// level 10 explicitly opts into unbounded complete JSON capture and output.
const maxBodyCapture = 16 << 20

func captureLimit(level int) int {
	if level >= 10 {
		return 0
	}
	return maxBodyCapture
}

func bodyOmission(contentType, encoding string, req *http.Request) string {
	if !isJSONBody(contentType) {
		return "[omitted: unsupported or streaming content type]"
	}
	if encoding != "" && !strings.EqualFold(encoding, "identity") {
		return "[omitted: encoded body]"
	}
	watch := strings.ToLower(req.URL.Query().Get("watch"))
	if watch == "true" || watch == "1" {
		return "[omitted: streaming watch body]"
	}
	return ""
}

func (t *tracer) requestBody(id uint64, req *http.Request) {
	if req.Body == nil || req.Body == http.NoBody {
		return
	}
	omit := bodyOmission(req.Header.Get("Content-Type"), req.Header.Get("Content-Encoding"), req)
	if omit == "" && req.GetBody == nil {
		omit = "[omitted: non-replayable request body]"
	}
	limit := captureLimit(t.level)
	if omit == "" && limit > 0 && req.ContentLength > int64(limit) {
		omit = "[omitted: JSON body exceeds 16 MiB capture limit]"
	}
	if omit != "" {
		t.log(id, "request_body", "body=%q", omit)
		return
	}
	ctx := req.Context()
	omit = "[omitted: request body snapshot canceled]"
	if ctx.Err() == nil {
		// GetBody and Close have no context parameter. Keep every potentially
		// blocking snapshot operation off the request goroutine. A buffered
		// result lets the worker exit even if cancellation wins the select.
		done := make(chan requestBodySnapshot, 1)
		getBody := req.GetBody
		go func() { done <- captureRequestBody(ctx, getBody, limit) }()
		select {
		case result := <-done:
			if ctx.Err() != nil {
				break
			}
			omit = result.omission
			if omit == "" {
				omit = formatBody(result.data, req.Header.Get("Content-Type"), t.level)
			}
		case <-ctx.Done():
		}
	}
	t.log(id, "request_body", "body=%q", omit)
}

type requestBodySnapshot struct {
	data     []byte
	omission string
}

func captureRequestBody(ctx context.Context, getBody func() (io.ReadCloser, error), limit int) requestBodySnapshot {
	if ctx.Err() != nil {
		return requestBodySnapshot{omission: "[omitted: request body snapshot canceled]"}
	}
	body, err := getBody()
	if body != nil {
		// Only the independent snapshot is closed here, never the API body.
		// Close must run concurrently with Read to unblock standard bodies.
		var closing atomic.Bool
		closeBody := func() {
			// Do not wait behind another Close: a custom closer can itself
			// block, and the cancellation callback must not strand a waiter.
			if closing.CompareAndSwap(false, true) {
				body.Close()
			}
		}
		stop := context.AfterFunc(ctx, closeBody)
		defer stop()
		defer closeBody()
	}
	if ctx.Err() != nil {
		// A GetBody that finishes after cancellation still transfers ownership:
		// close its result without reading it. Go cannot interrupt an arbitrary
		// GetBody or Close that never returns; cleanup completes when it returns.
		return requestBodySnapshot{omission: "[omitted: request body snapshot canceled]"}
	}
	if err != nil || body == nil {
		return requestBodySnapshot{omission: "[omitted: request body snapshot unavailable]"}
	}
	var reader io.Reader = body
	if limit > 0 {
		reader = io.LimitReader(body, int64(limit)+1)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return requestBodySnapshot{omission: "[omitted: request body snapshot failed]"}
	}
	if limit > 0 && len(data) > limit {
		return requestBodySnapshot{omission: "[omitted: JSON body exceeds 16 MiB capture limit]"}
	}
	return requestBodySnapshot{data: data}
}

func (t *tracer) responseBody(id uint64, req *http.Request, resp *http.Response) {
	if resp.Body == nil || resp.Body == http.NoBody {
		return
	}
	omit := ""
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Keep error diagnostics consistent with the safe Platform API error
		// contract: do not print arbitrary server prose from error responses.
		omit = "[omitted: non-success response body]"
	} else {
		omit = bodyOmission(resp.Header.Get("Content-Type"), resp.Header.Get("Content-Encoding"), req)
	}
	limit := captureLimit(t.level)
	if omit == "" && limit > 0 && resp.ContentLength > int64(limit) {
		omit = "[omitted: JSON body exceeds 16 MiB capture limit]"
	}
	if omit != "" {
		t.log(id, "response_body", "body=%q", omit)
		return
	}
	resp.Body = &observedBody{source: resp.Body, trace: t, id: id, contentType: resp.Header.Get("Content-Type"), expected: resp.ContentLength, limit: limit}
}

// observedBody copies only bytes the caller reads. It never pre-reads, drains or
// retries a stream. Underlying Read and Close run outside mu so Close can unblock
// a concurrent read (e.g. when the request context is canceled).
type observedBody struct {
	source      io.ReadCloser
	trace       *tracer
	id          uint64
	contentType string
	expected    int64
	limit       int
	mu          sync.Mutex
	data        bytes.Buffer
	total       int64
	done        bool
}

func (b *observedBody) Read(p []byte) (int, error) {
	n, err := b.source.Read(p)
	b.mu.Lock()
	message := ""
	if !b.done {
		b.total += int64(n)
		if b.limit > 0 && b.total > int64(b.limit) {
			message = b.finish(false, "JSON body exceeds 16 MiB capture limit")
		} else {
			b.data.Write(p[:n])
			if err == io.EOF {
				message = b.finish(true, "")
			} else if err != nil {
				message = b.finish(false, "response body read failed")
			}
		}
	}
	b.mu.Unlock()
	if message != "" {
		b.trace.log(b.id, "response_body", "body=%q", message)
	}
	return n, err
}

func (b *observedBody) Close() error {
	err := b.source.Close()
	b.mu.Lock()
	message := ""
	if !b.done {
		// Some consumers read exactly Content-Length bytes without asking for
		// EOF. A partially consumed or unknown-length body must not be printed.
		message = b.finish(err == nil && b.expected >= 0 && b.total == b.expected, "response body not fully read")
	}
	b.mu.Unlock()
	if message != "" {
		b.trace.log(b.id, "response_body", "body=%q", message)
	}
	return err
}

// finish is called with mu held and releases captured bytes after formatting.
func (b *observedBody) finish(complete bool, reason string) string {
	b.done = true
	message := "[omitted: " + reason + "]"
	if complete {
		message = formatBody(b.data.Bytes(), b.contentType, b.trace.level)
	}
	b.data = bytes.Buffer{}
	return message
}
