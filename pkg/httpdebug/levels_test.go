package httpdebug

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestTraceLevelThreshold(t *testing.T) {
	for _, level := range []int{0, 5, 6, 7, 8, 9, 10} {
		var trace bytes.Buffer
		req, _ := http.NewRequestWithContext(WithTrace(context.Background(), &trace, level), http.MethodGet, "https://example.com", nil)
		_, err := Wrap(roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
		})).RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		if (strings.Contains(trace.String(), " start ")) != (level >= 6) {
			t.Errorf("level %d produced unexpected trace: %s", level, trace.String())
		}
	}
}
