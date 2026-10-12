package platformapi

import (
	"context"
	"errors"

	"github.com/openshift-online/gcp-hcp-ctl/pkg/auth"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const maxStoredResourceNameBytes = 256

// HTTPError describes a usable Platform API HTTP failure. Error returns only a
// fixed label: status codes, verbs, resource names, server prose, and inferred
// policy advice are deliberately omitted from normal CLI output. Request
// metadata is available through accessors for callers that need it.
type HTTPError struct {
	statusCode int
	method     string
	resource   string
	name       string
	message    string
}

func newHTTPError(method, resource, name string, statusCode int, status *metav1.Status, body []byte) *HTTPError {
	if len(name) > maxStoredResourceNameBytes {
		name = name[:maxStoredResourceNameBytes]
	}
	return &HTTPError{
		statusCode: statusCode,
		method:     method,
		resource:   resource,
		name:       name,
		message:    displayMessage(statusCode, status, body),
	}
}

func (e *HTTPError) Error() string { return e.message }

// StatusCode returns the received HTTP status without adding it to normal output.
func (e *HTTPError) StatusCode() int { return e.statusCode }

// Method returns the request's HTTP method.
func (e *HTTPError) Method() string { return e.method }

// Resource returns the API resource targeted by the request.
func (e *HTTPError) Resource() string { return e.resource }

// Name returns the bounded name of a targeted resource, or an empty string.
func (e *HTTPError) Name() string { return e.name }

// TransportError describes a failure for which no usable Platform API JSON
// response is available. This includes connection/auth-token failures and
// responses client-go cannot interpret because of their content type. It has
// no HTTP status and never exposes the underlying error's text.
type TransportError struct {
	message  string
	sentinel error
}

func newTransportError(err error) *TransportError {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return &TransportError{message: "request timed out", sentinel: context.DeadlineExceeded}
	case errors.Is(err, context.Canceled):
		return &TransportError{message: "request canceled", sentinel: context.Canceled}
	case errors.Is(err, auth.ErrGcloudAuthentication):
		return &TransportError{message: "not authenticated; run gcloud auth login", sentinel: auth.ErrGcloudAuthentication}
	}

	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		return &TransportError{message: "request timed out", sentinel: context.DeadlineExceeded}
	}
	return &TransportError{message: "request failed"}
}

func (e *TransportError) Error() string { return e.message }

// Unwrap preserves timeout and cancellation classification without exposing
// the raw URL, credential-source, or response error as a printable cause.
func (e *TransportError) Unwrap() error { return e.sentinel }
