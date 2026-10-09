package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/config"
)

// fakeFetcher returns a fixed manifest per URL and counts calls so tests can
// assert caching behavior. When a URL is missing it returns an error.
type fakeFetcher struct {
	manifestsByURL map[string]*Manifest
	callsByURL     map[string]int
	err            error
}

func (fetcher *fakeFetcher) Fetch(_ context.Context, manifestURL string) (*Manifest, error) {
	if fetcher.callsByURL == nil {
		fetcher.callsByURL = map[string]int{}
	}
	fetcher.callsByURL[manifestURL]++
	if fetcher.err != nil {
		return nil, fetcher.err
	}
	manifest, ok := fetcher.manifestsByURL[manifestURL]
	if !ok {
		return nil, fmt.Errorf("no manifest for %s", manifestURL)
	}
	return manifest, nil
}

func manifestWith(region, apiEndpoint, oidcIssuer string) *Manifest {
	return &Manifest{
		SchemaVersion: manifestSchemaVersion,
		Environment:   "integration",
		Regions: map[string]RegionInfo{
			region: {OIDCIssuer: oidcIssuer, PlatformAPIEndpoint: apiEndpoint},
		},
	}
}

func TestEnvLabel(t *testing.T) {
	cases := map[string]string{
		// Known environments map to their short label.
		"dev":         "dev",
		"integration": "int",
		"int":         "int",
		"stage":       "stg",
		"staging":     "stg",
		"production":  "prd",
		"prod":        "prd",
		"prd":         "prd",
		"INT":         "int",
		"  stage ":    "stg",
		// Anything else is a discovery subdomain, used verbatim (normalized).
		"sandbox.dev":          "sandbox.dev",
		"bff2ec66.platform-ci": "bff2ec66.platform-ci",
		"CUSTOM":               "custom",
	}
	for input, want := range cases {
		if got := envLabel(input); got != want {
			t.Errorf("envLabel(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestManifestURL(t *testing.T) {
	cases := map[string]string{
		"integration":          "https://discovery.int.gcp-hcp.devshift.net/v1/regions.json",
		"bff2ec66.platform-ci": "https://discovery.bff2ec66.platform-ci.gcp-hcp.devshift.net/v1/regions.json",
		"sandbox.dev":          "https://discovery.sandbox.dev.gcp-hcp.devshift.net/v1/regions.json",
	}
	for env, want := range cases {
		got, err := ManifestURL(env)
		if err != nil {
			t.Fatalf("ManifestURL(%q): unexpected error: %v", env, err)
		}
		if got != want {
			t.Errorf("ManifestURL(%q) = %q, want %q", env, got, want)
		}
	}

	// Empty and labels that are not valid DNS subdomains are rejected, so a
	// pass-through --env can only ever extend the discovery hostname.
	for _, env := range []string{"", "foo@example.com", "foo/bar", "foo bar", "foo..bar", "-foo", "foo-"} {
		if _, err := ManifestURL(env); err == nil {
			t.Errorf("expected error for invalid env %q", env)
		}
	}
}

func TestEndpointRequiredError(t *testing.T) {
	// The message names the missing input without exposing manifest internals.
	// An explicit endpoint is an escape hatch even when an environment is set.
	noJargon := []string{"platform API endpoint", "OIDC issuer"}
	tests := []struct {
		name         string
		kind         Kind
		environment  string
		wantContains []string
		wantAbsent   []string
	}{
		{
			name:         "env set offers region or explicit endpoint",
			environment:  "dev",
			wantContains: []string{"--region is required", "GCPHCPCTL_REGION", "--api-endpoint", "dev"},
			wantAbsent:   noJargon,
		},
		{
			name:         "no env offers discovery or an explicit API endpoint",
			wantContains: []string{"--env", "--region", "--api-endpoint", "GCPHCPCTL_API_ENDPOINT", "api_endpoint"},
			wantAbsent:   noJargon,
		},
		{
			name:         "no env, OIDC kind names the OIDC escape hatch, not the API one",
			kind:         KindOIDC,
			wantContains: []string{"--env", "--oidc-endpoint", "GCPHCPCTL_OIDC_ENDPOINT", "oidc_endpoint"},
			wantAbsent:   append([]string{"GCPHCPCTL_API_ENDPOINT"}, noJargon...),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := EndpointRequiredError(tc.kind, tc.environment)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q should contain %q", err.Error(), want)
				}
			}
			for _, absent := range tc.wantAbsent {
				if strings.Contains(err.Error(), absent) {
					t.Errorf("error %q should not contain %q", err.Error(), absent)
				}
			}
		})
	}
}

func TestParseManifest(t *testing.T) {
	valid := []byte(`{"schema_version":"v1","environment":"integration","regions":{"us-central1":{"oidc_issuer":"https://oidc.int.gcp-hcp.devshift.net","platform_api_endpoint":"https://api.int.gcp-hcp.devshift.net"}}}`)
	manifest, err := ParseManifest(valid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if manifest.Regions["us-central1"].PlatformAPIEndpoint != "https://api.int.gcp-hcp.devshift.net" {
		t.Errorf("unexpected api endpoint: %q", manifest.Regions["us-central1"].PlatformAPIEndpoint)
	}

	// An ephemeral-CI manifest (self-reported environment "ci", endpoints under
	// platform-ci.gcp-hcp.devshift.net) is accepted: endpoint validation is tied
	// to the shared discovery domain, not a per-environment label.
	ci := []byte(`{"schema_version":"v1","environment":"ci","regions":{"us-west1":{"oidc_issuer":"https://oidc.e2e-reg-us-w1-x.platform-ci.gcp-hcp.devshift.net","platform_api_endpoint":"https://platform-api-us-west1-x.platform-ci.gcp-hcp.devshift.net"}}}`)
	if _, err := ParseManifest(ci); err != nil {
		t.Errorf("CI manifest should parse: %v", err)
	}

	if _, err := ParseManifest([]byte(`{"schema_version":"v2","regions":{}}`)); err == nil {
		t.Error("expected error for unsupported schema_version")
	}
	if _, err := ParseManifest([]byte(`not json`)); err == nil {
		t.Error("expected error for invalid JSON")
	}
	// Every advertised endpoint must be HTTPS, carry no credentials, and sit under
	// the trusted discovery domain.
	for _, data := range []string{
		`{"schema_version":"v1","environment":"integration","regions":{"us-central1":{"oidc_issuer":"http://oidc.int.gcp-hcp.devshift.net","platform_api_endpoint":"https://api.int.gcp-hcp.devshift.net"}}}`,
		`{"schema_version":"v1","environment":"integration","regions":{"us-central1":{"oidc_issuer":"https://oidc.int.gcp-hcp.devshift.net","platform_api_endpoint":"https://user@api.int.gcp-hcp.devshift.net"}}}`,
		`{"schema_version":"v1","environment":"integration","regions":{"us-central1":{"oidc_issuer":"https://oidc.int.gcp-hcp.devshift.net","platform_api_endpoint":"https://attacker.example.com"}}}`,
	} {
		if _, err := ParseManifest([]byte(data)); err == nil {
			t.Errorf("expected error for malformed manifest %s", data)
		}
	}
}

func TestHTTPFetcher(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/valid":
			fmt.Fprint(w, `{"schema_version":"v1","environment":"integration","regions":{"us-central1":{"oidc_issuer":"https://oidc.int.gcp-hcp.devshift.net","platform_api_endpoint":"https://api.int.gcp-hcp.devshift.net"}}}`)
		case "/large":
			fmt.Fprint(w, strings.Repeat("x", maxManifestBytes+1))
		case "/redirect":
			http.Redirect(w, r, "http://example.com/manifest", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	fetcher := NewHTTPFetcher(0)
	fetcher.httpClient.Transport = server.Client().Transport
	if _, err := fetcher.Fetch(context.Background(), server.URL+"/valid"); err != nil {
		t.Fatalf("valid HTTPS manifest: %v", err)
	}
	for _, target := range []string{"http://example.com/manifest", server.URL + "/large", server.URL + "/redirect"} {
		if _, err := fetcher.Fetch(context.Background(), target); err == nil {
			t.Errorf("expected error for %s", target)
		}
	}
}

func TestRegion(t *testing.T) {
	derivedURL := "https://discovery.int.gcp-hcp.devshift.net/v1/regions.json"
	newFetcher := func() *fakeFetcher {
		return &fakeFetcher{manifestsByURL: map[string]*Manifest{
			derivedURL: manifestWith("us-central1", "https://api.int.gcp-hcp.devshift.net", "https://oidc.int.gcp-hcp.devshift.net"),
		}}
	}

	t.Run("no environment returns the explicit values without fetching", func(t *testing.T) {
		fetcher := newFetcher()
		got, err := newResolver(fetcher).Region(context.Background(), &config.Config{
			Region:       "us-central1",
			APIEndpoint:  "https://explicit-api",
			OIDCEndpoint: "https://explicit-oidc",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.PlatformAPIEndpoint != "https://explicit-api" || got.OIDCIssuer != "https://explicit-oidc" {
			t.Errorf("got %+v, want the explicit values", got)
		}
		if len(fetcher.callsByURL) != 0 {
			t.Errorf("expected no fetch, got %v", fetcher.callsByURL)
		}
	})

	t.Run("explicit API combines with discovered OIDC", func(t *testing.T) {
		fetcher := newFetcher()
		got, err := newResolver(fetcher).Region(context.Background(), &config.Config{
			Region: "us-central1", Environment: "integration", APIEndpoint: "https://explicit-api",
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.PlatformAPIEndpoint != "https://explicit-api" || got.OIDCIssuer != "https://oidc.int.gcp-hcp.devshift.net" {
			t.Errorf("got %+v, want explicit API and discovered OIDC", got)
		}
		if calls := fetcher.callsByURL[derivedURL]; calls != 1 {
			t.Errorf("expected one fetch, got %d", calls)
		}
	})

	t.Run("explicit OIDC combines with discovered API", func(t *testing.T) {
		fetcher := newFetcher()
		got, err := newResolver(fetcher).Region(context.Background(), &config.Config{
			Region: "us-central1", Environment: "integration", OIDCEndpoint: "https://explicit-oidc",
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.PlatformAPIEndpoint != "https://api.int.gcp-hcp.devshift.net" || got.OIDCIssuer != "https://explicit-oidc" {
			t.Errorf("got %+v, want discovered API and explicit OIDC", got)
		}
		if calls := fetcher.callsByURL[derivedURL]; calls != 1 {
			t.Errorf("expected one fetch, got %d", calls)
		}
	})

	t.Run("both explicit endpoints skip discovery", func(t *testing.T) {
		fetcher := newFetcher()
		got, err := newResolver(fetcher).Region(context.Background(), &config.Config{
			Environment: "integration", APIEndpoint: "https://explicit-api", OIDCEndpoint: "https://explicit-oidc",
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.PlatformAPIEndpoint != "https://explicit-api" || got.OIDCIssuer != "https://explicit-oidc" {
			t.Errorf("got %+v, want both explicit endpoints", got)
		}
		if len(fetcher.callsByURL) != 0 {
			t.Errorf("both explicit endpoints should skip fetch, got %v", fetcher.callsByURL)
		}
	})

	t.Run("environment and region discover both endpoints in one fetch", func(t *testing.T) {
		fetcher := newFetcher()
		got, err := newResolver(fetcher).Region(context.Background(), &config.Config{Region: "us-central1", Environment: "integration"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.PlatformAPIEndpoint != "https://api.int.gcp-hcp.devshift.net" || got.OIDCIssuer != "https://oidc.int.gcp-hcp.devshift.net" {
			t.Errorf("got %+v, want the discovered endpoints", got)
		}
		if calls := fetcher.callsByURL[derivedURL]; calls != 1 {
			t.Errorf("expected 1 fetch, got %d", calls)
		}
	})

	t.Run("environment without region returns empty without fetching", func(t *testing.T) {
		fetcher := newFetcher()
		got, err := newResolver(fetcher).Region(context.Background(), &config.Config{Environment: "integration"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.PlatformAPIEndpoint != "" || got.OIDCIssuer != "" {
			t.Errorf("got %+v, want empty", got)
		}
		if len(fetcher.callsByURL) != 0 {
			t.Errorf("expected no fetch, got %v", fetcher.callsByURL)
		}
	})

	t.Run("a region may advertise only one of the endpoints", func(t *testing.T) {
		fetcher := &fakeFetcher{manifestsByURL: map[string]*Manifest{
			derivedURL: manifestWith("us-central1", "https://api.int.gcp-hcp.devshift.net", ""),
		}}
		got, err := newResolver(fetcher).Region(context.Background(), &config.Config{Region: "us-central1", Environment: "integration"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.PlatformAPIEndpoint == "" || got.OIDCIssuer != "" {
			t.Errorf("got %+v, want api set and oidc empty", got)
		}
	})

	t.Run("fetch failure is returned, not swallowed", func(t *testing.T) {
		_, err := newResolver(&fakeFetcher{err: fmt.Errorf("network down")}).
			Region(context.Background(), &config.Config{Region: "us-central1", Environment: "integration"})
		if err == nil || !strings.Contains(err.Error(), "network down") {
			t.Errorf("expected wrapped fetch error, got %v", err)
		}
	})

	t.Run("region not in manifest is an error pointing at regions list", func(t *testing.T) {
		_, err := newResolver(newFetcher()).Region(context.Background(), &config.Config{Region: "europe-west1", Environment: "integration"})
		if err == nil {
			t.Fatal("expected error for region not in manifest")
		}
		if !strings.Contains(err.Error(), "europe-west1") || !strings.Contains(err.Error(), "gcphcpctl regions list --env integration") {
			t.Errorf("error should name the region and suggest the regions list command, got %v", err)
		}
	})
}

func TestFetchManifest(t *testing.T) {
	derivedURL := "https://discovery.int.gcp-hcp.devshift.net/v1/regions.json"

	t.Run("unresolved discovery hostname gives a formatted error", func(t *testing.T) {
		for _, tc := range []struct {
			environment string
			devHint     string
		}{
			{environment: "dev", devHint: "\nFor shared dev environments, use --env <infra-id>.dev."},
			{environment: "integration"},
		} {
			t.Run(tc.environment, func(t *testing.T) {
				manifestURL, err := ManifestURL(tc.environment)
				if err != nil {
					t.Fatal(err)
				}
				host, err := url.Parse(manifestURL)
				if err != nil {
					t.Fatal(err)
				}
				dnsErr := &net.DNSError{Err: "no such host", Name: host.Hostname(), IsNotFound: true}
				fetcher := &fakeFetcher{err: fmt.Errorf("fetching discovery manifest %s: %w", manifestURL, &url.Error{
					Op: "Get", URL: manifestURL, Err: dnsErr,
				})}
				_, err = newResolver(fetcher).FetchManifest(context.Background(), tc.environment)
				if err == nil {
					t.Fatal("expected DNS error")
				}
				want := fmt.Sprintf("Could not load endpoint discovery for environment %q.\n  Host: %s\n  Cause: DNS lookup returned %q.\n\nCheck the environment name and whether its discovery DNS record is available.%s", tc.environment, host.Hostname(), dnsErr.Err, tc.devHint)
				if err.Error() != want {
					t.Errorf("error = %q, want %q", err, want)
				}
				if !errors.Is(err, dnsErr) {
					t.Errorf("error does not wrap the DNS cause: %v", err)
				}
			})
		}
	})

	t.Run("derives URL from environment", func(t *testing.T) {
		fetcher := &fakeFetcher{manifestsByURL: map[string]*Manifest{
			derivedURL: manifestWith("us-central1", "https://api.int.gcp-hcp.devshift.net", "https://oidc.int.gcp-hcp.devshift.net"),
		}}
		manifest, err := newResolver(fetcher).FetchManifest(context.Background(), "integration")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if manifest.Regions["us-central1"].PlatformAPIEndpoint != "https://api.int.gcp-hcp.devshift.net" {
			t.Errorf("unexpected manifest: %+v", manifest)
		}
	})

	t.Run("shares the resolver cache with Endpoints", func(t *testing.T) {
		fetcher := &fakeFetcher{manifestsByURL: map[string]*Manifest{
			derivedURL: manifestWith("us-central1", "https://api.int.gcp-hcp.devshift.net", "https://oidc.int.gcp-hcp.devshift.net"),
		}}
		resolver := newResolver(fetcher)
		if _, err := resolver.FetchManifest(context.Background(), "integration"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, err := resolver.Region(context.Background(), &config.Config{Region: "us-central1", Environment: "integration"}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if calls := fetcher.callsByURL[derivedURL]; calls != 1 {
			t.Errorf("expected 1 fetch (cached), got %d", calls)
		}
	})

	t.Run("empty environment is an error", func(t *testing.T) {
		if _, err := newResolver(&fakeFetcher{}).FetchManifest(context.Background(), ""); err == nil {
			t.Error("expected error when environment is empty")
		}
	})
}
