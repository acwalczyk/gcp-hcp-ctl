package httpdebug

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestConcurrentPayloadRecordsStayIsolated(t *testing.T) {
	for _, level := range []int{8, 9, 10} {
		t.Run(strconv.Itoa(level), func(t *testing.T) {
			var trace bytes.Buffer
			ctx := WithTrace(t.Context(), &trace, level)
			transport := Wrap(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				data, err := io.ReadAll(req.Body)
				req.Body.Close()
				if err != nil {
					return nil, err
				}
				return &http.Response{StatusCode: 201, Header: http.Header{"Content-Type": {"application/json"}}, ContentLength: int64(len(data)), Body: io.NopCloser(bytes.NewReader(data))}, nil
			}))
			const count = 12
			var wg sync.WaitGroup
			for i := range count {
				wg.Add(1)
				go func() {
					defer wg.Done()
					body := fmt.Sprintf(`{"name":"object-%d","token":"private-%d"}`, i, i)
					req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.com/objects", strings.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
					resp, err := transport.RoundTrip(req)
					if err != nil {
						t.Error(err)
						return
					}
					got, err := io.ReadAll(resp.Body)
					resp.Body.Close()
					if err != nil || string(got) != body {
						t.Errorf("response payload changed: %q %v", got, err)
					}
				}()
			}
			wg.Wait()
			if strings.Contains(trace.String(), "private-") {
				t.Fatal("payload secret leaked")
			}
			seen := map[string]map[string]int{}
			for _, line := range strings.Split(strings.TrimSpace(trace.String()), "\n") {
				fields := strings.Fields(line)
				if len(fields) < 4 || fields[1] != "http" || !strings.HasPrefix(fields[2], "request=") {
					t.Fatalf("interleaved line: %q", line)
				}
				id, event := fields[2], fields[3]
				if seen[id] == nil {
					seen[id] = map[string]int{}
				}
				seen[id][event]++
			}
			if len(seen) != count {
				t.Fatalf("request count: %d", len(seen))
			}
			for id, events := range seen {
				for _, event := range []string{"start", "request_headers", "request_body", "complete", "response_headers", "response_body"} {
					if events[event] != 1 {
						t.Errorf("%s has %d %s records", id, events[event], event)
					}
				}
			}
		})
	}
}

func TestHighVerbosityWriterFailurePreservesPayloads(t *testing.T) {
	const payload = `{"name":"unchanged","password":"private-password"}`
	req, _ := http.NewRequestWithContext(WithTrace(t.Context(), failedWriter{}, 10), http.MethodPost, "https://example.com", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	resp, err := Wrap(roundTripFunc(func(sent *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(sent.Body)
		sent.Body.Close()
		if err != nil || string(body) != payload {
			t.Fatal("failed logging changed request")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, ContentLength: int64(len(payload)), Body: io.NopCloser(strings.NewReader(payload))}, nil
	})).RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != payload {
		t.Fatal("failed logging changed response")
	}
	if err := resp.Body.Close(); err != nil {
		t.Fatal(err)
	}
}
