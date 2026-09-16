package typesafe

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// TypeSafeError reports an SDK configuration or input error.
type TypeSafeError struct {
	Message string
	Err     error
}

func (e *TypeSafeError) Error() string { return e.Message }

// Unwrap returns the underlying error, when present.
func (e *TypeSafeError) Unwrap() error { return e.Err }

// APIError reports an unsuccessful HTTP response from the TypeSafe API.
type APIError struct {
	StatusCode int
	Body       any
	Headers    http.Header
	RequestID  string
}

// Error implements error.
func (e *APIError) Error() string { return describeAPIError(e.StatusCode, e.Body) }

// BadRequestError reports HTTP 400.
type BadRequestError struct{ *APIError }

// Unwrap returns the underlying API error.
func (e *BadRequestError) Unwrap() error { return e.APIError }

// AuthenticationError reports HTTP 401.
type AuthenticationError struct{ *APIError }

// Unwrap returns the underlying API error.
func (e *AuthenticationError) Unwrap() error { return e.APIError }

// PermissionDeniedError reports HTTP 403.
type PermissionDeniedError struct{ *APIError }

// Unwrap returns the underlying API error.
func (e *PermissionDeniedError) Unwrap() error { return e.APIError }

// NotFoundError reports HTTP 404.
type NotFoundError struct{ *APIError }

// Unwrap returns the underlying API error.
func (e *NotFoundError) Unwrap() error { return e.APIError }

// UnprocessableEntityError reports HTTP 422.
type UnprocessableEntityError struct{ *APIError }

// Unwrap returns the underlying API error.
func (e *UnprocessableEntityError) Unwrap() error { return e.APIError }

// RateLimitError reports HTTP 429 and includes a parsed retry delay when supplied.
type RateLimitError struct {
	*APIError
	RetryAfter time.Duration
}

// Unwrap returns the underlying API error.
func (e *RateLimitError) Unwrap() error { return e.APIError }

// InternalServerError reports HTTP 5xx.
type InternalServerError struct{ *APIError }

// Unwrap returns the underlying API error.
func (e *InternalServerError) Unwrap() error { return e.APIError }

// APIConnectionError reports a network or response-delivery failure.
type APIConnectionError struct {
	Err error
}

// Error implements error.
func (e *APIConnectionError) Error() string {
	if e.Err == nil {
		return "typesafe: connection error"
	}
	return "typesafe: connection error: " + e.Err.Error()
}

// Unwrap returns the underlying transport error.
func (e *APIConnectionError) Unwrap() error { return e.Err }

// APITimeoutError reports an attempt that exceeded its configured timeout.
type APITimeoutError struct {
	Timeout time.Duration
	Err     error
}

// Error implements error.
func (e *APITimeoutError) Error() string {
	return fmt.Sprintf("typesafe: request timed out after %s", e.Timeout)
}

// Unwrap makes timeout errors usable as APIConnectionError values through errors.As.
func (e *APITimeoutError) Unwrap() error { return &APIConnectionError{Err: e.Err} }

// APIUserAbortError reports cancellation by the request context.
type APIUserAbortError struct {
	Err error
}

// Error implements error.
func (e *APIUserAbortError) Error() string { return "typesafe: request canceled" }

// Unwrap returns the context cancellation error.
func (e *APIUserAbortError) Unwrap() error { return e.Err }

// ResponseValidationError reports an invalid successful API response.
type ResponseValidationError struct {
	RequestID string
	Body      []byte
	Err       error
}

// Error implements error.
func (e *ResponseValidationError) Error() string {
	return "typesafe: validate API response: " + e.Err.Error()
}

// Unwrap returns the response decoding or validation error.
func (e *ResponseValidationError) Unwrap() error { return e.Err }

// ResponseBodyTooLargeError reports a response whose body exceeded the configured limit.
type ResponseBodyTooLargeError struct {
	Limit int64
}

// Error implements error.
func (e *ResponseBodyTooLargeError) Error() string {
	return fmt.Sprintf("typesafe: response body exceeds limit of %d bytes", e.Limit)
}

func newAPIError(status int, body any, headers http.Header) error {
	apiErr := &APIError{
		StatusCode: status,
		Body:       body,
		Headers:    headers.Clone(),
		RequestID:  headers.Get("X-TypeSafe-Request-ID"),
	}
	switch {
	case status == http.StatusBadRequest:
		return &BadRequestError{apiErr}
	case status == http.StatusUnauthorized:
		return &AuthenticationError{apiErr}
	case status == http.StatusForbidden:
		return &PermissionDeniedError{apiErr}
	case status == http.StatusNotFound:
		return &NotFoundError{apiErr}
	case status == http.StatusUnprocessableEntity:
		return &UnprocessableEntityError{apiErr}
	case status == http.StatusTooManyRequests:
		delay := parseRetryAfter(headers)
		if delay < 0 {
			delay = 0
		}
		return &RateLimitError{APIError: apiErr, RetryAfter: delay}
	case status >= 500:
		return &InternalServerError{apiErr}
	default:
		return apiErr
	}
}

func decodeErrorBody(body []byte) any {
	if len(body) == 0 {
		return nil
	}
	var decoded any
	if json.Unmarshal(body, &decoded) == nil {
		return decoded
	}
	return string(body)
}

func describeAPIError(status int, body any) string {
	if message := errorMessage(body); message != "" {
		return fmt.Sprintf("%d %s", status, message)
	}
	if body == nil {
		return fmt.Sprintf("%d status code (no body)", status)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		raw = []byte(fmt.Sprint(body))
	}
	if len(raw) > 200 {
		raw = append(raw[:200], "…"...)
	}
	return fmt.Sprintf("%d %s", status, raw)
}

func errorMessage(body any) string {
	switch value := body.(type) {
	case string:
		return value
	case map[string]any:
		for _, key := range []string{"error", "message", "detail"} {
			if text, ok := value[key].(string); ok {
				return text
			}
			if nested, ok := value[key].(map[string]any); ok {
				if text, ok := nested["message"].(string); ok {
					return text
				}
			}
		}
		if details, ok := value["detail"].([]any); ok {
			parts := make([]string, 0, len(details))
			for _, detail := range details {
				entry, ok := detail.(map[string]any)
				if !ok {
					continue
				}
				message, ok := entry["msg"].(string)
				if !ok {
					continue
				}
				var location []string
				if loc, ok := entry["loc"].([]any); ok {
					for _, item := range loc {
						text := fmt.Sprint(item)
						if text != "body" {
							location = append(location, text)
						}
					}
				}
				if len(location) > 0 {
					parts = append(parts, strings.Join(location, ".")+": "+message)
				} else {
					parts = append(parts, message)
				}
			}
			return strings.Join(parts, "; ")
		}
	}
	return ""
}
