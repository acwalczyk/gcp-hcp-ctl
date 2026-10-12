// Package auth provides Google identity token acquisition for API authentication.
//
// The Go SDK (Application Default Credentials) is the primary auth path and
// supports service_account and external_account (WIF with SA impersonation)
// credential types. The gcloud CLI is used as a fallback when ADC credentials
// are absent or of authorized_user type (personal gcloud auth login sessions).
//
// Credential selection happens once at TokenSource construction via chooseFetcher,
// which reads the ADC credential file to determine the type without making any
// network calls.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"google.golang.org/api/idtoken"
)

const (
	// defaultTokenLifetime is the assumed lifetime of a Google identity token.
	defaultTokenLifetime = 55 * time.Minute
	// tokenRefreshTimeout bounds how long a single token fetch may take,
	// preventing a hung subprocess or slow network call from blocking all callers.
	tokenRefreshTimeout = 15 * time.Second
	// TokenEarlyRefresh is how far before expiry a cached token is considered
	// stale. Long-lived TokenSource instances (e.g. HTTP transports) apply this
	// margin internally. Exec-plugin callers should subtract it from the
	// expirationTimestamp they write to ExecCredential so that kubectl triggers a
	// refresh with the same lead time.
	TokenEarlyRefresh = 30 * time.Second

	// PlatformAPIAudience is the OAuth2 client ID configured in the platform
	// API's ESPv2 proxy (x-google-audiences). Both user (gcloud authorized_user)
	// and service account / WIF identity tokens must use this as the audience so
	// that ESPv2 accepts them. Using the API endpoint URL as the audience instead
	// would cause ESPv2 to reject CI/WIF tokens with 401.
	PlatformAPIAudience = "32555940559.apps.googleusercontent.com"
)

// ErrBareWIF is returned when a WIF (external_account) credential is missing
// the service_account_impersonation_url required for identity token generation.
// Callers can match it with errors.Is.
var ErrBareWIF = errors.New("WIF credential does not support identity tokens — " +
	"add service_account_impersonation_url to your credential file")

// ErrGcloudAuthentication identifies a failure to obtain an identity token
// from the gcloud user-credential fallback. Callers can match it with
// errors.Is and replace gcloud's underlying output with a safe, actionable
// diagnostic.
var ErrGcloudAuthentication = errors.New("gcloud authentication failed")

// tokenFetcher abstracts credential retrieval so tests can inject fakes.
// FetchIdentityToken returns the token string, its expiry (zero if unknown),
// and any error.
type tokenFetcher interface {
	FetchIdentityToken(ctx context.Context) (token string, expiry time.Time, err error)
	FetchAccountEmail(ctx context.Context) (string, error)
}

// ── TokenSource ───────────────────────────────────────────────────────────────

// TokenSource provides Google identity tokens and the authenticated user's email
// for API authentication. Tokens are cached and refreshed automatically.
type TokenSource struct {
	mu        sync.Mutex
	token     string
	userEmail string
	expiry    time.Time
	fetcher   tokenFetcher
}

// NewTokenSource returns a TokenSource for the given audience (API endpoint URL).
//
// The Go SDK (ADC) is used when a service_account or external_account credential
// is configured. The gcloud CLI is used as a fallback for authorized_user
// credentials or when no ADC credentials are configured.
func NewTokenSource(audience string) *TokenSource {
	return &TokenSource{fetcher: chooseFetcher(audience)}
}

// Token returns a valid Google identity token and the authenticated user's email,
// refreshing both if the cached values are expired or absent.
// The context controls cancellation of any underlying network or subprocess calls.
func (ts *TokenSource) Token(ctx context.Context) (token, userEmail string, err error) {
	token, userEmail, _, err = ts.TokenWithExpiry(ctx)
	return
}

// TokenWithExpiry is like Token but also returns the token expiry time.
// The expiry reflects the actual token lifetime when the fetcher provides it,
// or a default 55-minute lifetime when no expiry is available (e.g. gcloud fallback).
func (ts *TokenSource) TokenWithExpiry(ctx context.Context) (token, userEmail string, expiry time.Time, err error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.token != "" && time.Now().Add(TokenEarlyRefresh).Before(ts.expiry) {
		return ts.token, ts.userEmail, ts.expiry, nil
	}

	refreshCtx, cancel := context.WithTimeout(ctx, tokenRefreshTimeout)
	defer cancel()

	newToken, newExpiry, err := ts.fetcher.FetchIdentityToken(refreshCtx)
	if err != nil {
		return "", "", time.Time{}, err
	}

	// Email is fetched once and cached for the lifetime of this TokenSource.
	// It does not change between token refreshes, so we skip re-fetching it on
	// every renewal. On the first acquisition (userEmail == ""), we fetch and
	// commit it atomically with the token so callers always see a consistent pair.
	if ts.userEmail == "" {
		newEmail, emailErr := ts.fetcher.FetchAccountEmail(refreshCtx)
		if emailErr != nil {
			// Token not committed — cache stays consistent.
			return "", "", time.Time{}, emailErr
		}
		ts.userEmail = newEmail
	}

	ts.token = newToken
	if !newExpiry.IsZero() {
		ts.expiry = newExpiry
	} else {
		ts.expiry = time.Now().Add(defaultTokenLifetime)
	}
	return ts.token, ts.userEmail, ts.expiry, nil
}

// ── fetcher selection ─────────────────────────────────────────────────────────

// chooseFetcher reads the ADC credential file to determine the credential type
// and returns the appropriate tokenFetcher. No network calls are made.
//
//   - service_account / external_account → goSDKFetcher (SDK handles both)
//   - authorized_user                    → gcloudFetcher (SDK does not support identity tokens for this type)
//   - no ADC credentials                 → gcloudFetcher (user may have a gcloud session)
func chooseFetcher(audience string) tokenFetcher {
	credType, err := detectADCCredentialType()
	if err != nil {
		// No ADC credentials — fall back to gcloud.
		fmt.Fprintf(os.Stderr, "warning: ADC credential detection failed, falling back to gcloud: %v\n", err)
		return gcloudFetcher{}
	}
	if credType == "authorized_user" {
		return gcloudFetcher{}
	}
	return goSDKFetcher{audience: audience}
}

// detectADCCredentialType returns the "type" field from the ADC credential file.
func detectADCCredentialType() (string, error) {
	data, err := readADCFile()
	if err != nil {
		return "", err
	}
	var cred struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &cred); err != nil {
		return "", fmt.Errorf("parsing ADC credential file: %w", err)
	}
	if cred.Type == "" {
		return "", fmt.Errorf("ADC credential file has no type field")
	}
	return cred.Type, nil
}

// ── goSDKFetcher ─────────────────────────────────────────────────────────────

// goSDKFetcher retrieves identity tokens via Application Default Credentials
// using the Google SDK. Supports service_account and external_account credential
// types that include service_account_impersonation_url.
type goSDKFetcher struct {
	audience string
}

func (g goSDKFetcher) FetchIdentityToken(ctx context.Context) (string, time.Time, error) {
	// Pre-flight: bare WIF credentials (no service_account_impersonation_url)
	// cannot produce identity tokens. Surface an actionable error before making
	// any network calls — the SDK error message is opaque.
	if data, err := readADCFile(); err == nil {
		var cred credFileJSON
		if json.Unmarshal(data, &cred) == nil &&
			cred.Type == "external_account" && cred.ServiceAccountImpersonationURL == "" {
			return "", time.Time{}, ErrBareWIF
		}
	}

	ts, err := idtoken.NewTokenSource(ctx, g.audience)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("creating identity token source: %w", err)
	}
	tok, err := ts.Token()
	if err != nil {
		return "", time.Time{}, fmt.Errorf("fetching identity token: %w", err)
	}
	return tok.AccessToken, tok.Expiry, nil
}

func (g goSDKFetcher) FetchAccountEmail(_ context.Context) (string, error) {
	data, err := readADCFile()
	if err != nil {
		return "", err
	}
	return saEmailFromCredJSON(data)
}

// ── gcloudFetcher ────────────────────────────────────────────────────────────

// gcloudFetcher retrieves tokens and account info via the gcloud CLI.
// Used when ADC credentials are absent or of authorized_user type.
type gcloudFetcher struct {
	// commandOutput is supplied by tests. Production requests use gcloud
	// directly through output.
	commandOutput func(context.Context, ...string) ([]byte, error)
}

func (g gcloudFetcher) output(ctx context.Context, args ...string) ([]byte, error) {
	if g.commandOutput != nil {
		return g.commandOutput(ctx, args...)
	}
	return exec.CommandContext(ctx, "gcloud", args...).Output()
}

func gcloudAuthenticationError(operation string) error {
	return fmt.Errorf("%w: %s\n\n  Ensure gcloud is installed and authenticated:\n    gcloud auth login", ErrGcloudAuthentication, operation)
}

func (g gcloudFetcher) FetchIdentityToken(ctx context.Context) (string, time.Time, error) {
	out, err := g.output(ctx, "auth", "print-identity-token")
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", time.Time{}, ctxErr
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", time.Time{}, gcloudAuthenticationError("failed to get identity token")
		}
		return "", time.Time{}, fmt.Errorf("failed to get identity token: %w\n\n"+
			"  Ensure gcloud is installed and authenticated:\n"+
			"    gcloud auth login", err)
	}
	token := strings.TrimSpace(string(out))
	if token == "" {
		return "", time.Time{}, fmt.Errorf("%w: gcloud returned an empty identity token", ErrGcloudAuthentication)
	}
	// gcloud does not expose the token expiry; defaultTokenLifetime will be used.
	return token, time.Time{}, nil
}

func (g gcloudFetcher) FetchAccountEmail(ctx context.Context) (string, error) {
	out, err := g.output(ctx, "config", "get-value", "account")
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", gcloudAuthenticationError("failed to get account email")
		}
		return "", fmt.Errorf("failed to get account email: %w", err)
	}
	email := strings.TrimSpace(string(out))
	if email == "" || email == "(unset)" {
		return "", fmt.Errorf("%w: no active gcloud account", ErrGcloudAuthentication)
	}
	return email, nil
}

// ── ADC helpers ───────────────────────────────────────────────────────────────

// readADCFile returns the raw bytes of the Application Default Credentials file.
// It checks GOOGLE_APPLICATION_CREDENTIALS first, then the well-known path.
func readADCFile() ([]byte, error) {
	if f := os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"); f != "" {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("reading credential file %s: %w", f, err)
		}
		return data, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("no ADC credentials configured")
	}
	wellKnown := filepath.Join(home, ".config", "gcloud", "application_default_credentials.json")
	data, err := os.ReadFile(wellKnown)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no ADC credentials configured")
		}
		return nil, fmt.Errorf("reading well-known credential file: %w", err)
	}
	return data, nil
}

// credFileJSON is the minimal structure needed to extract the SA email from a
// Google credential file.
type credFileJSON struct {
	Type                           string `json:"type"`
	ClientEmail                    string `json:"client_email"`
	ServiceAccountImpersonationURL string `json:"service_account_impersonation_url"`
}

// saEmailFromCredJSON extracts the service account email from credential JSON.
//
//   - service_account:  reads client_email directly.
//   - external_account: parses the SA email from service_account_impersonation_url.
func saEmailFromCredJSON(data []byte) (string, error) {
	var cred credFileJSON
	if err := json.Unmarshal(data, &cred); err != nil {
		return "", fmt.Errorf("parsing credential JSON: %w", err)
	}
	switch cred.Type {
	case "service_account":
		if cred.ClientEmail == "" {
			return "", fmt.Errorf("client_email missing from service_account credential")
		}
		return cred.ClientEmail, nil
	case "external_account":
		if cred.ServiceAccountImpersonationURL == "" {
			return "", fmt.Errorf(
				"service_account_impersonation_url missing from external_account credential: " +
					"identity tokens require SA impersonation")
		}
		return saEmailFromImpersonationURL(cred.ServiceAccountImpersonationURL)
	default:
		return "", fmt.Errorf("unsupported credential type %q for email extraction", cred.Type)
	}
}

// saEmailFromImpersonationURL extracts the SA email from a service account
// impersonation URL of the form:
//
//	https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts/<EMAIL>:generateIdToken
func saEmailFromImpersonationURL(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid service account impersonation URL: %w", err)
	}
	const marker = "serviceAccounts/"
	idx := strings.LastIndex(u.Path, marker)
	if idx == -1 {
		return "", fmt.Errorf("invalid service account impersonation URL: serviceAccounts/ segment not found")
	}
	rest := u.Path[idx+len(marker):]
	email := strings.TrimSuffix(rest, ":generateIdToken")
	if !strings.Contains(email, "@") {
		return "", fmt.Errorf("invalid service account impersonation URL: extracted value is not a valid email")
	}
	return email, nil
}
