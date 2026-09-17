package typesafe

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewAPIErrorMapsStatusAndPreservesResponseMetadata(t *testing.T) {
	tests := []struct {
		name   string
		status int
		check  func(*testing.T, error)
	}{
		{"bad request", http.StatusBadRequest, func(t *testing.T, err error) { var target *BadRequestError; assertAPIErrorType(t, err, &target) }},
		{"authentication", http.StatusUnauthorized, func(t *testing.T, err error) { var target *AuthenticationError; assertAPIErrorType(t, err, &target) }},
		{"permission", http.StatusForbidden, func(t *testing.T, err error) { var target *PermissionDeniedError; assertAPIErrorType(t, err, &target) }},
		{"not found", http.StatusNotFound, func(t *testing.T, err error) { var target *NotFoundError; assertAPIErrorType(t, err, &target) }},
		{"unprocessable", http.StatusUnprocessableEntity, func(t *testing.T, err error) {
			var target *UnprocessableEntityError
			assertAPIErrorType(t, err, &target)
		}},
		{"rate limit", http.StatusTooManyRequests, func(t *testing.T, err error) {
			var target *RateLimitError
			if !errors.As(err, &target) || target.RetryAfter != 1500*time.Millisecond {
				t.Errorf("newAPIError() = %#v, want rate limit with 1.5s retry", err)
			}
		}},
		{"server error", http.StatusInternalServerError, func(t *testing.T, err error) { var target *InternalServerError; assertAPIErrorType(t, err, &target) }},
		{"unmapped status", http.StatusTeapot, func(t *testing.T, err error) {
			var target *APIError
			if !errors.As(err, &target) || fmt.Sprintf("%T", err) != "*typesafe.APIError" {
				t.Errorf("newAPIError() = %T, want *APIError", err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			headers := make(http.Header)
			headers.Set("X-TypeSafe-Request-ID", "req_123")
			headers.Set("Retry-After-Ms", "1500")
			err := newAPIError(test.status, map[string]any{"message": "denied"}, headers)
			headers.Set("X-TypeSafe-Request-ID", "changed")
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != test.status || apiErr.RequestID != "req_123" || apiErr.Headers.Get("X-TypeSafe-Request-ID") != "req_123" {
				t.Fatalf("newAPIError() metadata = %#v", err)
			}
			test.check(t, err)
		})
	}
	limited := newAPIError(http.StatusTooManyRequests, nil, http.Header{"Retry-After": {"not-a-delay"}})
	var rateLimit *RateLimitError
	if !errors.As(limited, &rateLimit) || rateLimit.RetryAfter != 0 {
		t.Errorf("newAPIError(invalid retry header) = %#v, want zero retry delay", limited)
	}
}

func assertAPIErrorType[T error](t *testing.T, err error, target *T) {
	t.Helper()
	if !errors.As(err, target) {
		t.Errorf("newAPIError() = %T, want %T", err, *target)
	}
}

func TestDecodeErrorBodyAndFormattingContracts(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want any
	}{
		{"empty body", nil, nil},
		{"JSON object", []byte(`{"error":{"message":"invalid key"}}`), map[string]any{"error": map[string]any{"message": "invalid key"}}},
		{"JSON array", []byte(`["first","second"]`), []any{"first", "second"}},
		{"plain text", []byte("gateway unavailable"), "gateway unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := decodeErrorBody(test.body)
			if fmt.Sprintf("%#v", got) != fmt.Sprintf("%#v", test.want) {
				t.Errorf("decodeErrorBody() = %#v, want %#v", got, test.want)
			}
		})
	}

	formatTests := []struct {
		name string
		body any
		want string
	}{
		{"top level message", map[string]any{"message": "bad input"}, "400 bad input"},
		{"nested error message", map[string]any{"error": map[string]any{"message": "bad key"}}, "401 bad key"},
		{"validation details exclude body", map[string]any{"detail": []any{map[string]any{"loc": []any{"body", "items", 0}, "msg": "required"}}}, "422 items.0: required"},
		{"no body", nil, "503 status code (no body)"},
		{"structured fallback", map[string]any{"code": "bad"}, `400 {"code":"bad"}`},
	}
	for _, test := range formatTests {
		t.Run(test.name, func(t *testing.T) {
			if got := describeAPIError(statusForExpected(test.want), test.body); got != test.want {
				t.Errorf("describeAPIError() = %q, want %q", got, test.want)
			}
		})
	}
}

func statusForExpected(message string) int {
	var status int
	_, _ = fmt.Sscanf(message, "%d", &status)
	return status
}

func TestErrorValuesExposeMessagesAndCauses(t *testing.T) {
	cause := errors.New("network down")
	connection := &APIConnectionError{Err: cause}
	if connection.Error() != "typesafe: connection error: network down" || !errors.Is(connection, cause) {
		t.Errorf("connection error = %q, unwrap=%v", connection, errors.Unwrap(connection))
	}
	if got := (&APIConnectionError{}).Error(); got != "typesafe: connection error" {
		t.Errorf("empty connection Error() = %q", got)
	}

	timeout := &APITimeoutError{Timeout: 2 * time.Second, Err: cause}
	var unwrappedConnection *APIConnectionError
	if timeout.Error() != "typesafe: request timed out after 2s" || !errors.As(timeout, &unwrappedConnection) || !errors.Is(timeout, cause) {
		t.Errorf("timeout behavior = error %q, connection %#v", timeout, unwrappedConnection)
	}

	abort := &APIUserAbortError{Err: cause}
	validation := &ResponseValidationError{Err: cause}
	if abort.Error() != "typesafe: request canceled" || !errors.Is(abort, cause) {
		t.Errorf("abort behavior = %q", abort)
	}
	if validation.Error() != "typesafe: validate API response: network down" || !errors.Is(validation, cause) {
		t.Errorf("validation behavior = %q", validation)
	}
	if got := (&ResponseBodyTooLargeError{Limit: 1024}).Error(); got != "typesafe: response body exceeds limit of 1024 bytes" {
		t.Errorf("ResponseBodyTooLargeError.Error() = %q", got)
	}
	if got := (&TypeSafeError{Message: "invalid configuration", Err: cause}).Error(); got != "invalid configuration" {
		t.Errorf("TypeSafeError.Error() = %q", got)
	}
}

func TestDescribeAPIErrorTruncatesOpaqueBodies(t *testing.T) {
	body := map[string]any{"opaque": strings.Repeat("x", 300)}
	message := describeAPIError(500, body)
	if !strings.HasPrefix(message, `500 {"opaque":"`) || !strings.HasSuffix(message, "…") || len(message) != len(`500 `)+203 {
		t.Errorf("describeAPIError(long body) = %q", message)
	}
}
