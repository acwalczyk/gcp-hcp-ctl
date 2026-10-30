package httpdebug

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestProgressiveHTTPDetails(t *testing.T) {
	const requestBody = `{"spec":{"subject":"person@example.com","roleRef":{"kind":"PlatformRole","name":"cluster-admin"}},"token":"private-request-token"}`
	const responseBody = `{"result":"created","password":"private-response-password"}`
	for _, level := range []int{6, 7, 8, 9, 10} {
		t.Run(strconv.Itoa(level), func(t *testing.T) {
			var trace bytes.Buffer
			req, _ := http.NewRequestWithContext(WithTrace(context.Background(), &trace, level), http.MethodPost, "https://example.com/rolebindings", strings.NewReader(requestBody))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer private-auth-token")
			req.Header.Set("Cookie", "private-cookie")
			original := req.Body
			transport := Wrap(roundTripFunc(func(sent *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(sent.Body)
				if err != nil || string(body) != requestBody || sent.Header.Get("Authorization") != "Bearer private-auth-token" {
					t.Fatalf("request changed: body=%s, err=%v", body, err)
				}
				sent.Body.Close()
				return &http.Response{StatusCode: 201, Header: http.Header{"Content-Type": {"application/json"}, "Set-Cookie": {"private-response-cookie"}}, ContentLength: int64(len(responseBody)), Body: io.NopCloser(strings.NewReader(responseBody))}, nil
			}))
			resp, err := transport.RoundTrip(req)
			if err != nil {
				t.Fatal(err)
			}
			if req.Body != original {
				t.Fatal("tracer mutated original request body")
			}
			if strings.Contains(trace.String(), "response_body") {
				t.Fatal("response body must not be consumed before the caller reads it")
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil || string(body) != responseBody {
				t.Fatalf("response changed: %s, %v", body, err)
			}
			resp.Body.Close()
			for _, event := range []struct {
				name    string
				minimum int
			}{
				{"request_headers", 7}, {"response_headers", 8},
				{"request_body", 8}, {"response_body", 8},
				{"curl", 9}, {"timing", 9},
			} {
				if strings.Contains(trace.String(), " "+event.name+" ") != (level >= event.minimum) {
					t.Errorf("event %s at level %d: %s", event.name, level, trace.String())
				}
			}
			if level >= 8 && (!strings.Contains(trace.String(), "person@example.com") || !strings.Contains(trace.String(), "cluster-admin") || !strings.Contains(trace.String(), "created")) {
				t.Errorf("missing actual JSON payload: %s", trace.String())
			}
			if strings.Contains(trace.String(), "private-") {
				t.Errorf("credential leaked: %s", trace.String())
			}
			if strings.Count(trace.String(), " start ") != 1 || strings.Count(trace.String(), " complete ") != 1 {
				t.Errorf("duplicate attempts: %s", trace.String())
			}
		})
	}
}
