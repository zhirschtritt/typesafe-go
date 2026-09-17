package typesafe

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestNewClientValidatesOptionsAndNormalizesBaseURL(t *testing.T) {
	tests := []struct {
		name string
		opts []Option
	}{
		{name: "nil option", opts: []Option{nil}},
		{name: "empty key", opts: []Option{WithAPIKey(" \t ")}},
		{name: "relative base URL", opts: []Option{WithBaseURL("/v1")}},
		{name: "empty default model", opts: []Option{WithDefaultModel(" ")}},
		{name: "nil HTTP client", opts: []Option{WithHTTPClient(nil)}},
		{name: "zero timeout", opts: []Option{WithTimeout(0)}},
		{name: "negative response limit", opts: []Option{WithResponseBodyLimit(-1)}},
		{name: "nil headers", opts: []Option{WithHeaders(nil)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewClient(append([]Option{WithAPIKey("key")}, test.opts...)...)
			var configErr *TypeSafeError
			if !errors.As(err, &configErr) {
				t.Errorf("NewClient() error = %T %[1]v, want TypeSafeError", err)
			}
		})
	}

	client, err := NewClient(WithAPIKey("key"), WithBaseURL(" https://example.test/api/// "))
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client.baseURL != "https://example.test/api" {
		t.Errorf("baseURL = %q, want normalized URL", client.baseURL)
	}
}

func TestClientAndRequestOptionsValidateAndRequestTakesPrecedence(t *testing.T) {
	policy := DefaultRetryPolicy()
	policy.MaxRetries = 7
	client, err := NewClient(
		WithAPIKey("key"),
		WithDefaultModel("client-model"),
		WithTimeout(time.Second),
		WithRetryPolicy(policy),
		WithHeaders(http.Header{"X-Client": {"default"}}),
	)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	requestPolicy := DefaultRetryPolicy()
	requestPolicy.MaxRetries = 1
	config, err := client.requestConfig([]RequestOption{
		WithModel("request-model"),
		WithRequestTimeout(2 * time.Second),
		WithRequestRetryPolicy(requestPolicy),
		WithRequestHeaders(http.Header{"X-Request": {"override"}}),
	})
	if err != nil {
		t.Fatalf("requestConfig() error = %v", err)
	}
	if config.model != "request-model" || config.timeout != 2*time.Second || config.retry.MaxRetries != 1 || config.headers.Get("X-Request") != "override" || config.headers.Get("X-Client") != "" {
		t.Errorf("request config = %#v", config)
	}
	delete(requestPolicy.HTTPStatuses, http.StatusTooManyRequests)
	if _, ok := config.retry.HTTPStatuses[http.StatusTooManyRequests]; !ok {
		t.Error("request retry policy aliases caller map")
	}

	invalid := []struct {
		name string
		opt  RequestOption
	}{
		{name: "nil", opt: nil},
		{name: "empty model", opt: WithModel(" ")},
		{name: "zero timeout", opt: WithRequestTimeout(0)},
		{name: "nil headers", opt: WithRequestHeaders(nil)},
		{name: "invalid retry policy", opt: WithRequestRetryPolicy(RetryPolicy{MaxRetries: -1})},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			_, err := client.requestConfig([]RequestOption{test.opt})
			var configErr *TypeSafeError
			if !errors.As(err, &configErr) {
				t.Errorf("requestConfig() error = %T %[1]v, want TypeSafeError", err)
			}
		})
	}
}

func TestProtectedHeadersMergeAndProtectProtocolValues(t *testing.T) {
	defaults := http.Header{
		"x-shared":               {"default"},
		"X-Client-Multiple":      {"one", "two"},
		"authorization":          {"attacker"},
		"X-TypeSafe-Retry-Count": {"99"},
	}
	request := http.Header{
		"X-Shared":           {"request"},
		"x-request-multiple": {"one", "two"},
		"Accept":             {"text/plain"},
		"Content-Type":       {"text/plain"},
	}
	headers := protectedHeaders(defaults, request, "real-key", true, 2)
	if got := headers.Values("X-Shared"); len(got) != 1 || got[0] != "request" {
		t.Errorf("shared header = %q, want request value", got)
	}
	if got := headers.Values("X-Request-Multiple"); len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Errorf("multiple request header = %q", got)
	}
	if headers.Get("Authorization") != "Bearer real-key" || headers.Get("Accept") != "application/json" || headers.Get("Content-Type") != "application/json" || headers.Get("X-TypeSafe-Retry-Count") != "2" {
		t.Errorf("protected headers = %v", headers)
	}
	withoutBody := protectedHeaders(defaults, nil, "real-key", false, 0)
	if withoutBody.Get("Content-Type") != "" || withoutBody.Get("X-TypeSafe-Retry-Count") != "" {
		t.Errorf("bodyless first-attempt headers retain protocol values: %v", withoutBody)
	}
	if got := defaults["x-shared"]; len(got) != 1 || got[0] != "default" {
		t.Errorf("protectedHeaders mutated default headers: %v", defaults)
	}
}

func TestReadBoundedPreservesAllowedBodyAndClassifiesReadFailures(t *testing.T) {
	body, err := readBounded(strings.NewReader("exact"), 5)
	if err != nil || string(body) != "exact" {
		t.Fatalf("readBounded exact body = %q, %v", body, err)
	}
	_, err = readBounded(strings.NewReader("oversize"), 3)
	var tooLarge *ResponseBodyTooLargeError
	if !errors.As(err, &tooLarge) || tooLarge.Limit != 3 {
		t.Errorf("readBounded oversize error = %v, want size limit error", err)
	}
	readErr := errors.New("read failed")
	_, err = readBounded(errorReader{err: readErr}, 10)
	if !errors.Is(err, readErr) {
		t.Errorf("readBounded read failure = %v, want original error", err)
	}
}

func TestAttemptClassifiesConnectionFailureAndDecodeFailure(t *testing.T) {
	connectionErr := errors.New("network unavailable")
	client, err := NewClient(WithAPIKey("key"), WithHTTPClient(&http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, connectionErr
	})}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.attempt(context.Background(), http.MethodGet, "/v1/models", nil, requestConfig{timeout: time.Second}, 0)
	var connection *APIConnectionError
	if !errors.As(err, &connection) || !errors.Is(err, connectionErr) {
		t.Errorf("attempt connection error = %v, want wrapped transport failure", err)
	}

	client.httpClient = &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString("not json"))}, nil
	})}
	var destination struct{}
	_, err = client.do(context.Background(), http.MethodGet, "/v1/models", nil, requestConfig{timeout: time.Second, retry: RetryPolicy{}}, &destination)
	var validation *ResponseValidationError
	if !errors.As(err, &validation) || string(validation.Body) != "not json" {
		t.Errorf("do decode error = %#v, want response validation error with body", err)
	}
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
