package platformapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
)

const checkBeforeRetrying = "; check the resource before retrying"

// uncertainOutcomeError adds fixed guidance without exposing a client-go error.
// Unwrap retains the safe HTTP or transport error for programmatic callers.
type uncertainOutcomeError struct{ cause error }

func (e *uncertainOutcomeError) Error() string { return e.cause.Error() + checkBeforeRetrying }
func (e *uncertainOutcomeError) Unwrap() error { return e.cause }

// IsUncertainOutcome reports whether a non-repeatable Platform API write may
// have taken effect and its resource should be inspected before retrying.
func IsUncertainOutcome(err error) bool {
	var uncertain *uncertainOutcomeError
	return errors.As(err, &uncertain)
}

// responseDecodeError is distinct from an HTTP failure: the server reported
// success, but the expected object could not be decoded. Decoder text and body
// bytes are deliberately discarded, not wrapped.
type responseDecodeError struct{ message string }

func (e *responseDecodeError) Error() string { return e.message }

// normalizeResult is the sole live conversion of a failed rest.Result into
// the safe Platform API error contract. The nonIdempotent flag is explicit at
// every call site, not derived from the HTTP verb.
func normalizeResult(result rest.Result, method, resource, name string, nonIdempotent bool) error {
	requestErr := result.Error()
	if requestErr == nil {
		return nil
	}

	var statusCode int
	result.StatusCode(&statusCode)
	body, _ := result.Raw() // Raw also returns requestErr on failure; only bytes are used.
	if statusCode == 0 {
		err := newTransportError(requestErr)
		if nonIdempotent {
			return &uncertainOutcomeError{cause: err}
		}
		return err
	}

	var status *metav1.Status
	var statusErr *apierrors.StatusError
	if errors.As(requestErr, &statusErr) {
		decoded := statusErr.Status()
		status = &decoded
	}
	err := newHTTPError(method, resource, name, statusCode, status, body)
	// A versioned PUT conflict is a definite rejection, not an ambiguous write.
	if nonIdempotent && (statusCode >= http.StatusInternalServerError || (statusCode == http.StatusConflict && method != http.MethodPut)) {
		return &uncertainOutcomeError{cause: err}
	}
	return err
}

// decodeResult handles a successful HTTP result separately from request
// failures. A successful DELETE does not call this; it needs no response body.
func decodeResult(result rest.Result, obj runtime.Object, method, resource, name string, nonIdempotent bool) error {
	if err := normalizeResult(result, method, resource, name, nonIdempotent); err != nil {
		return err
	}
	decodeErr := result.Into(obj)
	// client-go may decode a response declaring another registered Kind into a
	// different object and return nil, leaving obj empty. Inspect only the
	// response's type header; Into itself is still called exactly once.
	if decodeErr == nil {
		body, _ := result.Raw()
		var typeMeta metav1.TypeMeta
		if json.Unmarshal(body, &typeMeta) == nil && typeMeta.Kind != "" && typeMeta.Kind != reflect.TypeOf(obj).Elem().Name() {
			decodeErr = errors.New("unexpected response kind")
		}
	}
	if decodeErr != nil {
		message := "invalid response"
		if method != http.MethodGet {
			message = "server reported success, but its response was invalid"
		}
		decodeErr := &responseDecodeError{message: message}
		if nonIdempotent {
			return &uncertainOutcomeError{cause: decodeErr}
		}
		return decodeErr
	}
	return nil
}
