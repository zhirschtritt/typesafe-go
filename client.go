package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"
)

const (
	defaultBaseURL = "https://api.typesafe.ai"
	defaultModel   = "jev-latest"
	// Version identifies this SDK in TypeSafe API requests.
	Version = "0.1.0" // x-release-please-version
)

// Client communicates with the TypeSafe v1 API. The caller retains ownership of its HTTP client.
type Client struct {
	apiKey       string
	baseURL      string
	defaultModel string
	httpClient   *http.Client
	timeout      time.Duration
	retry        RetryPolicy
	headers      http.Header
	bodyLimit    int64
}

type clientConfig struct {
	apiKey       string
	baseURL      string
	defaultModel string
	httpClient   *http.Client
	timeout      time.Duration
	retry        RetryPolicy
	headers      http.Header
	bodyLimit    int64
}

// Option configures a Client. Invalid settings cause NewClient to return an error.
type Option func(*clientConfig) error

// RequestOption configures a single API call. Invalid settings cause that call to return an error.
type RequestOption func(*requestConfig) error

type requestConfig struct {
	model   string
	timeout time.Duration
	retry   RetryPolicy
	headers http.Header
}

// NewClient constructs a TypeSafe API client. Explicit options take precedence over environment variables, then SDK defaults.
func NewClient(opts ...Option) (*Client, error) {
	config := clientConfig{
		apiKey:       environment("TYPESAFE_API_KEY"),
		baseURL:      valueOr(environment("TYPESAFE_BASE_URL"), defaultBaseURL),
		defaultModel: valueOr(environment("TYPESAFE_DEFAULT_MODEL"), defaultModel),
		httpClient:   http.DefaultClient,
		timeout:      defaultAttemptTimeout,
		retry:        DefaultRetryPolicy(),
		headers:      make(http.Header),
		bodyLimit:    defaultResponseMaxSize,
	}
	for _, option := range opts {
		if option == nil {
			return nil, &TypeSafeError{Message: "typesafe: nil client option"}
		}
		if err := option(&config); err != nil {
			return nil, err
		}
	}
	if strings.TrimSpace(config.apiKey) == "" {
		return nil, &TypeSafeError{Message: "typesafe: API key is required; set TYPESAFE_API_KEY or use WithAPIKey"}
	}
	baseURL, err := normalizeBaseURL(config.baseURL)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(config.defaultModel) == "" {
		return nil, &TypeSafeError{Message: "typesafe: default model must not be empty"}
	}
	if config.httpClient == nil {
		return nil, &TypeSafeError{Message: "typesafe: HTTP client must not be nil"}
	}
	if config.timeout <= 0 {
		return nil, &TypeSafeError{Message: "typesafe: timeout must be positive"}
	}
	if config.bodyLimit <= 0 {
		return nil, &TypeSafeError{Message: "typesafe: response body limit must be positive"}
	}
	policy, err := validateRetryPolicy(config.retry)
	if err != nil {
		return nil, err
	}
	return &Client{config.apiKey, baseURL, config.defaultModel, config.httpClient, config.timeout, policy, config.headers.Clone(), config.bodyLimit}, nil
}

// WithAPIKey sets the API bearer token.
func WithAPIKey(key string) Option {
	return func(config *clientConfig) error { config.apiKey = key; return nil }
}

// WithBaseURL sets the API root URL.
func WithBaseURL(baseURL string) Option {
	return func(config *clientConfig) error { config.baseURL = baseURL; return nil }
}

// WithDefaultModel sets the model used unless a request supplies WithModel.
func WithDefaultModel(model string) Option {
	return func(config *clientConfig) error { config.defaultModel = model; return nil }
}

// WithHTTPClient sets the HTTP client used for requests. The caller retains ownership of it.
func WithHTTPClient(client *http.Client) Option {
	return func(config *clientConfig) error { config.httpClient = client; return nil }
}

// WithTimeout sets the timeout for each HTTP attempt.
func WithTimeout(timeout time.Duration) Option {
	return func(config *clientConfig) error {
		if timeout <= 0 {
			return &TypeSafeError{Message: "typesafe: timeout must be positive"}
		}
		config.timeout = timeout
		return nil
	}
}

// WithRetryPolicy sets the complete retry policy for the client.
func WithRetryPolicy(policy RetryPolicy) Option {
	return func(config *clientConfig) error {
		validated, err := validateRetryPolicy(policy)
		if err != nil {
			return err
		}
		config.retry = validated
		return nil
	}
}

// WithHeaders adds headers to every request. Protected TypeSafe headers cannot be overridden.
func WithHeaders(headers http.Header) Option {
	return func(config *clientConfig) error {
		if headers == nil {
			return &TypeSafeError{Message: "typesafe: headers must not be nil"}
		}
		config.headers = normalizedHeaders(headers)
		return nil
	}
}

// WithResponseBodyLimit sets the maximum response body size in bytes.
func WithResponseBodyLimit(limit int64) Option {
	return func(config *clientConfig) error {
		if limit <= 0 {
			return &TypeSafeError{Message: "typesafe: response body limit must be positive"}
		}
		config.bodyLimit = limit
		return nil
	}
}

// WithModel chooses the model for one SystemOne request.
func WithModel(model string) RequestOption {
	return func(config *requestConfig) error {
		if strings.TrimSpace(model) == "" {
			return &TypeSafeError{Message: "typesafe: model must not be empty"}
		}
		config.model = model
		return nil
	}
}

// WithRequestTimeout sets the timeout for each attempt of one API call.
func WithRequestTimeout(timeout time.Duration) RequestOption {
	return func(config *requestConfig) error {
		if timeout <= 0 {
			return &TypeSafeError{Message: "typesafe: request timeout must be positive"}
		}
		config.timeout = timeout
		return nil
	}
}

// WithRequestRetryPolicy sets the complete retry policy for one API call.
func WithRequestRetryPolicy(policy RetryPolicy) RequestOption {
	return func(config *requestConfig) error {
		validated, err := validateRetryPolicy(policy)
		if err != nil {
			return err
		}
		config.retry = validated
		return nil
	}
}

// WithRequestHeaders adds headers to one API call. Protected TypeSafe headers cannot be overridden.
func WithRequestHeaders(headers http.Header) RequestOption {
	return func(config *requestConfig) error {
		if headers == nil {
			return &TypeSafeError{Message: "typesafe: request headers must not be nil"}
		}
		config.headers = normalizedHeaders(headers)
		return nil
	}
}

// SystemOne answers named questions about state using a TypeSafe model.
func (c *Client) SystemOne(ctx context.Context, state Entry, questions map[string]Question, opts ...RequestOption) (*Response, error) {
	if err := validateInputEntry(state, false); err != nil {
		return nil, &TypeSafeError{Message: "typesafe: state " + err.Error()}
	}
	if err := validateQuestions(questions); err != nil {
		return nil, err
	}
	config, err := c.requestConfig(opts)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(struct {
		State     Entry               `json:"state"`
		Questions map[string]Question `json:"questions"`
		Model     string              `json:"model"`
	}{state, questions, config.model})
	if err != nil {
		return nil, &TypeSafeError{Message: "typesafe: encode system one request: " + err.Error()}
	}
	var response Response
	requestID, err := c.do(ctx, http.MethodPost, "/v1/systemone", body, config, &response)
	if err != nil {
		return nil, err
	}
	response.RequestID = requestID
	if err := validateResponseForQuestions(&response, questions); err != nil {
		return nil, &ResponseValidationError{RequestID: requestID, Body: bytes.Clone(response.raw), Err: err}
	}
	response.raw = nil
	return &response, nil
}

// ListModels returns models and aliases available to the authenticated account.
func (c *Client) ListModels(ctx context.Context, opts ...RequestOption) (*ModelsResponse, error) {
	config, err := c.requestConfig(opts)
	if err != nil {
		return nil, err
	}
	var response ModelsResponse
	requestID, err := c.do(ctx, http.MethodGet, "/v1/models", nil, config, &response)
	if err != nil {
		return nil, err
	}
	response.RequestID = requestID
	return &response, nil
}

func (c *Client) requestConfig(opts []RequestOption) (requestConfig, error) {
	config := requestConfig{model: c.defaultModel, timeout: c.timeout, retry: c.retry, headers: make(http.Header)}
	for _, option := range opts {
		if option == nil {
			return requestConfig{}, &TypeSafeError{Message: "typesafe: nil request option"}
		}
		if err := option(&config); err != nil {
			return requestConfig{}, err
		}
	}
	return config, nil
}

func (c *Client) do(ctx context.Context, method, path string, body []byte, config requestConfig, destination any) (string, error) {
	if ctx == nil {
		return "", &TypeSafeError{Message: "typesafe: context must not be nil"}
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", &APIUserAbortError{Err: err}
		}
		response, err := c.attempt(ctx, method, path, body, config, attempt)
		if err == nil {
			if err := json.Unmarshal(response.body, destination); err != nil {
				return "", &ResponseValidationError{RequestID: response.requestID, Body: bytes.Clone(response.body), Err: err}
			}
			return response.requestID, nil
		}
		if attempt >= config.retry.MaxRetries || !shouldRetry(err, config.retry) {
			return "", err
		}
		if err := waitForRetry(ctx, retryDelay(attempt, responseHeaders(err), config.retry)); err != nil {
			return "", err
		}
	}
}

type receivedResponse struct {
	body      []byte
	requestID string
}

func (c *Client) attempt(ctx context.Context, method, path string, body []byte, config requestConfig, attempt int) (receivedResponse, error) {
	attemptContext, cancel := context.WithTimeout(ctx, config.timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(attemptContext, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return receivedResponse{}, &TypeSafeError{Message: "typesafe: create request: " + err.Error()}
	}
	request.Header = protectedHeaders(c.headers, config.headers, c.apiKey, body != nil, attempt)
	response, err := c.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return receivedResponse{}, &APIUserAbortError{Err: ctx.Err()}
		}
		if attemptContext.Err() != nil {
			return receivedResponse{}, &APITimeoutError{Timeout: config.timeout, Err: err}
		}
		return receivedResponse{}, &APIConnectionError{Err: err}
	}
	defer response.Body.Close()
	bodyBytes, err := readBounded(response.Body, c.bodyLimit)
	if err != nil {
		if ctx.Err() != nil {
			return receivedResponse{}, &APIUserAbortError{Err: ctx.Err()}
		}
		if attemptContext.Err() != nil {
			return receivedResponse{}, &APITimeoutError{Timeout: config.timeout, Err: err}
		}
		if _, tooLarge := err.(*ResponseBodyTooLargeError); tooLarge {
			return receivedResponse{}, err
		}
		return receivedResponse{}, &APIConnectionError{Err: err}
	}
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return receivedResponse{}, newAPIError(response.StatusCode, decodeErrorBody(bodyBytes), response.Header)
	}
	return receivedResponse{bodyBytes, response.Header.Get("X-TypeSafe-Request-ID")}, nil
}

func shouldRetry(err error, policy RetryPolicy) bool {
	switch error := err.(type) {
	case *APITimeoutError:
		return policy.RetryTimeoutErr
	case *APIConnectionError:
		return policy.RetryConnectionErr
	case *APIError:
		_, ok := policy.HTTPStatuses[error.StatusCode]
		return ok
	case *BadRequestError, *AuthenticationError, *PermissionDeniedError, *NotFoundError, *UnprocessableEntityError, *RateLimitError, *InternalServerError:
		apiError := unwrapAPIError(error)
		if apiError == nil {
			return false
		}
		_, ok := policy.HTTPStatuses[apiError.StatusCode]
		return ok
	default:
		return false
	}
}

func unwrapAPIError(err error) *APIError {
	switch error := err.(type) {
	case *BadRequestError:
		return error.APIError
	case *AuthenticationError:
		return error.APIError
	case *PermissionDeniedError:
		return error.APIError
	case *NotFoundError:
		return error.APIError
	case *UnprocessableEntityError:
		return error.APIError
	case *RateLimitError:
		return error.APIError
	case *InternalServerError:
		return error.APIError
	}
	return nil
}
func responseHeaders(err error) http.Header {
	if api := unwrapAPIError(err); api != nil {
		return api.Headers
	}
	if api, ok := err.(*APIError); ok {
		return api.Headers
	}
	return nil
}
func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit))
	if err != nil {
		return nil, err
	}
	var extra [1]byte
	n, err := reader.Read(extra[:])
	if n > 0 {
		return nil, &ResponseBodyTooLargeError{Limit: limit}
	}
	if err != nil && err != io.EOF {
		return nil, err
	}
	return data, nil
}
func normalizedHeaders(source http.Header) http.Header {
	headers := make(http.Header, len(source))
	for name, values := range source {
		canonicalName := http.CanonicalHeaderKey(name)
		headers[canonicalName] = append([]string(nil), values...)
	}
	return headers
}

func protectedHeaders(defaults, requestHeaders http.Header, key string, hasBody bool, attempt int) http.Header {
	headers := normalizedHeaders(defaults)
	for name, values := range normalizedHeaders(requestHeaders) {
		headers.Del(name)
		for index, value := range values {
			if index == 0 {
				headers.Set(name, value)
			} else {
				headers.Add(name, value)
			}
		}
	}
	headers.Set("Authorization", "Bearer "+key)
	headers.Set("Accept", "application/json")
	headers.Set("User-Agent", "typesafe-go/"+Version)
	headers.Set("X-TypeSafe-SDK", "typesafe-go/"+Version)
	headers.Set("X-TypeSafe-Runtime", runtime.Version()+" ("+runtime.GOOS+"; "+runtime.GOARCH+")")
	if hasBody {
		headers.Set("Content-Type", "application/json")
	} else {
		headers.Del("Content-Type")
	}
	headers.Del("X-TypeSafe-Retry-Count")
	if attempt > 0 {
		headers.Set("X-TypeSafe-Retry-Count", fmt.Sprint(attempt))
	}
	return headers
}
func normalizeBaseURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", &TypeSafeError{Message: "typesafe: base URL must be an absolute URL"}
	}
	return raw, nil
}
func environment(name string) string { return strings.TrimSpace(os.Getenv(name)) }
func valueOr(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
