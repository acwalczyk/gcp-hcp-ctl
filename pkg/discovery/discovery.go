// Package discovery resolves the platform API and OIDC issuer endpoints for a
// (environment, region) pair by fetching a static endpoint-discovery manifest
// published per environment (see gcp-hcp-infra GCP-1147):
//
//	https://discovery.{env}.gcp-hcp.devshift.net/v1/regions.json
//
// {env} is a short label: a known environment name maps to its abbreviation
// (integration -> int), while any other value — a shared dev sector, an
// ephemeral CI run — is used verbatim. That lets one --env value target any
// discovery manifest under gcp-hcp.devshift.net without a separate URL override.
//
// The manifest is advisory, not authoritative: it tells a client which endpoints
// to try, but the platform API server still enforces access. Callers that
// already know their endpoints bypass discovery by supplying explicit endpoints
// (flags, environment variables, or the config file).
package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/config"
)

// manifestSchemaVersion is the only manifest schema version this client
// understands. The manifest path is also versioned (/v1/regions.json), so a
// breaking change ships as a new path plus a new value here.
const manifestSchemaVersion = "v1"

// defaultFetchTimeout bounds a single manifest fetch. Discovery sits on the
// cluster-create critical path, so a hung DNS/CDN lookup must fail fast rather
// than block the command indefinitely.
const defaultFetchTimeout = 10 * time.Second

// maxManifestBytes caps the manifest body read so a hostile or misbehaving
// origin cannot stream an unbounded response into memory.
const maxManifestBytes = 1 << 20

// discoveryDomain is the trusted parent domain for both the manifest URL and
// every endpoint the manifest advertises. It is fixed so that neither an --env
// value nor a manifest entry can redirect the CLI off this domain.
const discoveryDomain = "gcp-hcp.devshift.net"

// Kind selects which endpoint of a region entry to resolve.
type Kind int

const (
	// KindAPI resolves the platform API endpoint (platform_api_endpoint).
	KindAPI Kind = iota
	// KindOIDC resolves the OIDC issuer base URL (oidc_issuer).
	KindOIDC
)

func (kind Kind) String() string {
	switch kind {
	case KindAPI:
		return "platform API endpoint"
	case KindOIDC:
		return "OIDC issuer"
	default:
		return "unknown endpoint"
	}
}

// RegionInfo is a single region entry in the discovery manifest.
type RegionInfo struct {
	OIDCIssuer          string `json:"oidc_issuer" yaml:"oidc_issuer"`
	PlatformAPIEndpoint string `json:"platform_api_endpoint" yaml:"platform_api_endpoint"`
}

// Manifest is the parsed endpoint-discovery document. Keys of Regions are the
// public GCP region names (e.g. "us-central1").
type Manifest struct {
	SchemaVersion string                `json:"schema_version"`
	Environment   string                `json:"environment"`
	Regions       map[string]RegionInfo `json:"regions"`
}

// Fetcher retrieves and parses a discovery manifest from a URL. It is an
// interface so tests can supply a manifest without network access.
type Fetcher interface {
	Fetch(ctx context.Context, manifestURL string) (*Manifest, error)
}

// knownEnvLabels maps friendly environment names (and their common synonyms) to
// the short label used in the discovery hostname. These mirror the stable
// environments in gcp-hcp-infra's terraform/metadata/environments.yaml.
var knownEnvLabels = map[string]string{
	"dev":         "dev",
	"integration": "int",
	"int":         "int",
	"stage":       "stg",
	"staging":     "stg",
	"stg":         "stg",
	"production":  "prd",
	"prod":        "prd",
	"prd":         "prd",
}

// envLabelPattern matches one or more dot-separated DNS labels (lowercase
// alphanumeric and hyphens). It bounds a pass-through --env value so it can only
// extend the discovery hostname — never alter the scheme, the domain suffix, or
// the path.
var envLabelPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*$`)

// envLabel resolves an --env value to its discovery subdomain label. A known
// environment name maps to its short form; any other value is used verbatim, so
// a shared dev sector or an ephemeral CI run is reachable with a single --env:
//
//	integration          -> discovery.int.gcp-hcp.devshift.net
//	<infra-id>.dev       -> discovery.<infra-id>.dev.gcp-hcp.devshift.net
//	<run-id>.platform-ci -> discovery.<run-id>.platform-ci.gcp-hcp.devshift.net
func envLabel(environment string) string {
	normalized := strings.ToLower(strings.TrimSpace(environment))
	if label, ok := knownEnvLabels[normalized]; ok {
		return label
	}
	return normalized
}

// ManifestURL builds the discovery manifest URL for an --env value. The
// discoveryDomain suffix and /v1/regions.json path are fixed, so the result is
// always an HTTPS URL for a subdomain of the trusted discovery domain.
func ManifestURL(environment string) (string, error) {
	label := envLabel(environment)
	if label == "" {
		return "", fmt.Errorf("env is required to build the discovery manifest URL")
	}
	if !envLabelPattern.MatchString(label) {
		return "", fmt.Errorf("invalid env %q: must be an environment name or a discovery subdomain label", environment)
	}
	return fmt.Sprintf("https://discovery.%s.%s/v1/regions.json", label, discoveryDomain), nil
}

// EndpointRequiredError explains how to supply the endpoint a command needs.
// With an environment selected, the caller needs a region to discover a missing
// endpoint, or it can supply that endpoint explicitly. With no environment,
// discovery cannot run, so the message offers both paths.
func EndpointRequiredError(kind Kind, environment string) error {
	endpointFlag := "--api-endpoint"
	if kind == KindOIDC {
		endpointFlag = "--oidc-endpoint"
	}
	if environment != "" {
		return fmt.Errorf("--region is required for endpoint discovery with --env %q (set --region, GCPHCPCTL_REGION, or region in config; or use %s to bypass discovery)", environment, endpointFlag)
	}
	endpointHint := "GCPHCPCTL_API_ENDPOINT or api_endpoint in config"
	if kind == KindOIDC {
		endpointHint = "GCPHCPCTL_OIDC_ENDPOINT or oidc_endpoint in config"
	}
	return fmt.Errorf("no endpoint available: set --env and --region for discovery (or GCPHCPCTL_ENVIRONMENT / GCPHCPCTL_REGION or config), or set an explicit endpoint via %s, %s", endpointFlag, endpointHint)
}

// Resolver resolves endpoints via endpoint discovery, caching each fetched
// manifest so resolving both the API and OIDC endpoints for one command issues
// at most one network request per manifest URL.
type Resolver struct {
	fetcher Fetcher

	cacheMutex     sync.Mutex
	manifestsByURL map[string]*Manifest
}

// NewResolver returns a Resolver that fetches discovery manifests over HTTPS.
func NewResolver() *Resolver {
	return newResolver(NewHTTPFetcher(defaultFetchTimeout))
}

// newResolver builds a Resolver around a specific Fetcher. Tests use it to inject
// a fetcher that serves canned manifests without a network call.
func newResolver(fetcher Fetcher) *Resolver {
	return &Resolver{
		fetcher:        fetcher,
		manifestsByURL: make(map[string]*Manifest),
	}
}

// Region returns the configured and discovered endpoints for a region. Explicit
// values from cfg take precedence; the manifest supplies missing values.
//
// With no environment or no region, it returns the available explicit values
// without fetching; the caller raises EndpointRequiredError if one is needed.
func (resolver *Resolver) Region(ctx context.Context, cfg *config.Config) (RegionInfo, error) {
	info := RegionInfo{PlatformAPIEndpoint: cfg.APIEndpoint, OIDCIssuer: cfg.OIDCEndpoint}
	if info.PlatformAPIEndpoint != "" && info.OIDCIssuer != "" {
		return info, nil
	}
	if cfg.Environment == "" || cfg.Region == "" {
		return info, nil
	}

	manifest, err := resolver.FetchManifest(ctx, cfg.Environment)
	if err != nil {
		return RegionInfo{}, err
	}
	entry, found := manifest.Regions[cfg.Region]
	if !found {
		// Point the user at the single actionable next step — the command that
		// lists valid regions — rather than inlining the region list.
		return RegionInfo{}, fmt.Errorf("region %q is not available in env %q; run 'gcphcpctl regions list --env %s' to see available regions",
			cfg.Region, cfg.Environment, cfg.Environment)
	}
	if info.PlatformAPIEndpoint == "" {
		info.PlatformAPIEndpoint = entry.PlatformAPIEndpoint
	}
	if info.OIDCIssuer == "" {
		info.OIDCIssuer = entry.OIDCIssuer
	}
	return info, nil
}

// FetchManifest returns the full discovery manifest for an --env value. Results
// are cached per URL, so a caller may mix FetchManifest and Region on one
// Resolver without issuing duplicate network requests.
func (resolver *Resolver) FetchManifest(ctx context.Context, environment string) (*Manifest, error) {
	manifestURL, err := ManifestURL(environment)
	if err != nil {
		return nil, err
	}
	manifest, err := resolver.cachedManifest(ctx, manifestURL)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			parsedURL, _ := url.Parse(manifestURL) // ManifestURL always returns a valid URL.
			return nil, &discoveryHostNotFoundError{
				environment: environment,
				host:        parsedURL.Hostname(),
				reason:      dnsErr.Err,
				cause:       err,
			}
		}
		return nil, err
	}
	return manifest, nil
}

// discoveryHostNotFoundError keeps the DNS cause available to callers while
// showing a concise, actionable message at the CLI.
type discoveryHostNotFoundError struct {
	environment string
	host        string
	reason      string
	cause       error
}

func (err *discoveryHostNotFoundError) Error() string {
	message := fmt.Sprintf("Could not load endpoint discovery for environment %q.\n  Host: %s\n  Cause: DNS lookup returned %q.\n\nCheck the environment name and whether its discovery DNS record is available.", err.environment, err.host, err.reason)
	if envLabel(err.environment) == "dev" {
		message += "\nFor shared dev environments, use --env <infra-id>.dev."
	}
	return message
}

func (err *discoveryHostNotFoundError) Unwrap() error { return err.cause }

// cachedManifest returns the manifest for a URL, fetching it once and reusing
// the result for subsequent lookups against the same URL.
func (resolver *Resolver) cachedManifest(ctx context.Context, manifestURL string) (*Manifest, error) {
	resolver.cacheMutex.Lock()
	defer resolver.cacheMutex.Unlock()

	if cached, ok := resolver.manifestsByURL[manifestURL]; ok {
		return cached, nil
	}

	manifest, err := resolver.fetcher.Fetch(ctx, manifestURL)
	if err != nil {
		return nil, err
	}
	resolver.manifestsByURL[manifestURL] = manifest
	return manifest, nil
}

// HTTPFetcher fetches a discovery manifest over HTTPS. The manifest is public
// (unauthenticated), so no credentials are attached.
type HTTPFetcher struct {
	httpClient *http.Client
}

// NewHTTPFetcher returns an HTTPFetcher. A zero timeout uses the default.
func NewHTTPFetcher(timeout time.Duration) *HTTPFetcher {
	if timeout <= 0 {
		timeout = defaultFetchTimeout
	}
	return &HTTPFetcher{httpClient: &http.Client{
		Timeout: timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

// Fetch retrieves and parses the manifest at manifestURL.
func (fetcher *HTTPFetcher) Fetch(ctx context.Context, manifestURL string) (*Manifest, error) {
	if err := validateHTTPSURL(manifestURL); err != nil {
		return nil, fmt.Errorf("invalid discovery manifest URL: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating discovery request for %s: %w", manifestURL, err)
	}

	response, err := fetcher.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetching discovery manifest %s: %w", manifestURL, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching discovery manifest %s: HTTP %d", manifestURL, response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxManifestBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading discovery manifest %s: %w", manifestURL, err)
	}
	if len(body) > maxManifestBytes {
		return nil, fmt.Errorf("discovery manifest %s exceeds %d bytes", manifestURL, maxManifestBytes)
	}

	manifest, err := ParseManifest(body)
	if err != nil {
		return nil, fmt.Errorf("parsing discovery manifest %s: %w", manifestURL, err)
	}
	return manifest, nil
}

// ParseManifest decodes and validates a manifest document. Every advertised
// endpoint must be HTTPS under the trusted discovery domain, so a tampered or
// misconfigured manifest cannot redirect the CLI to an arbitrary host.
func ParseManifest(data []byte) (*Manifest, error) {
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, err
	}
	if manifest.SchemaVersion != manifestSchemaVersion {
		return nil, fmt.Errorf("unsupported manifest schema_version %q (want %q)", manifest.SchemaVersion, manifestSchemaVersion)
	}
	for region, endpoints := range manifest.Regions {
		if endpoints.PlatformAPIEndpoint != "" {
			if err := validateDiscoveredEndpoint(endpoints.PlatformAPIEndpoint); err != nil {
				return nil, fmt.Errorf("region %q has invalid platform_api_endpoint: %w", region, err)
			}
		}
		if endpoints.OIDCIssuer != "" {
			if err := validateDiscoveredEndpoint(endpoints.OIDCIssuer); err != nil {
				return nil, fmt.Errorf("region %q has invalid oidc_issuer: %w", region, err)
			}
		}
	}
	return &manifest, nil
}

// validateHTTPSURL requires a well-formed HTTPS URL with a host and no embedded
// credentials or fragment.
func validateHTTPSURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.Fragment != "" {
		return fmt.Errorf("must be an HTTPS URL with a host and no credentials or fragment")
	}
	return nil
}

// validateDiscoveredEndpoint ensures a manifest-advertised endpoint is HTTPS and
// a subdomain of (or equal to) the trusted discovery domain.
func validateDiscoveredEndpoint(rawURL string) error {
	if err := validateHTTPSURL(rawURL); err != nil {
		return err
	}
	parsed, _ := url.Parse(rawURL)
	host := strings.ToLower(parsed.Hostname())
	if host != discoveryDomain && !strings.HasSuffix(host, "."+discoveryDomain) {
		return fmt.Errorf("host must be under %s", discoveryDomain)
	}
	return nil
}
