package httpdebug

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

type tokenSourceFunc func() (*oauth2.Token, error)

func (f tokenSourceFunc) Token() (*oauth2.Token, error) { return f() }

func TestOAuthTraceRunsOnlyAfterAuthentication(t *testing.T) {
	for _, level := range []int{6, 8, 9, 10} {
		for _, authenticated := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/level%d", map[bool]string{false: "failure", true: "success"}[authenticated], level), func(t *testing.T) {
				var trace bytes.Buffer
				var sent bool
				authError := errors.New("private-auth-error")
				base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
					sent = true
					if r.Header.Get("Authorization") != "Bearer private-token" {
						t.Error("authentication not preserved")
					}
					if !strings.Contains(trace.String(), "start") {
						t.Error("missing trace before actual request")
					}
					return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
				})
				authTransport := &oauth2.Transport{Base: base, Source: tokenSourceFunc(func() (*oauth2.Token, error) {
					if trace.Len() != 0 {
						t.Error("trace started before authentication completed")
					}
					if !authenticated {
						return nil, authError
					}
					return &oauth2.Token{AccessToken: "private-token"}, nil
				})}
				transport := Wrap(authTransport)
				req, _ := http.NewRequestWithContext(WithTrace(context.Background(), &trace, level), http.MethodGet, "https://example.com", nil)
				_, err := transport.RoundTrip(req)
				if authenticated {
					if err != nil || !sent || strings.Count(trace.String(), " start ") != 1 || strings.Count(trace.String(), " complete ") != 1 {
						t.Fatalf("authenticated request: sent=%v err=%v trace=%s", sent, err, trace.String())
					}
				} else if !errors.Is(err, authError) || sent || trace.Len() != 0 {
					t.Fatalf("auth failure logged as a sent request: sent=%v err=%v trace=%s", sent, err, trace.String())
				}
				if strings.Contains(trace.String(), "private-") || req.Header.Get("Authorization") != "" {
					t.Error("leaked secret or mutated request")
				}
				if _, ok := authTransport.Base.(roundTripFunc); !ok {
					t.Error("Wrap mutated shared OAuth transport")
				}
			})
		}
	}
}
