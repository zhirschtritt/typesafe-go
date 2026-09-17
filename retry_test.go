package typesafe

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestDefaultRetryPolicyReturnsIndependentStatusMaps(t *testing.T) {
	first := DefaultRetryPolicy()
	second := DefaultRetryPolicy()
	delete(first.HTTPStatuses, http.StatusTooManyRequests)

	if _, ok := second.HTTPStatuses[http.StatusTooManyRequests]; !ok {
		t.Fatal("mutating one default policy changed another policy")
	}
	if first.MaxRetries != defaultMaxRetries || first.BackoffInitial != defaultBackoffInitial || first.BackoffMax != defaultBackoffMaximum || first.BackoffJitter != defaultBackoffJitter || !first.RespectRetryAfter || first.MaxRetryAfter != defaultMaxRetryAfter || !first.RetryConnectionErr || !first.RetryTimeoutErr {
		t.Errorf("default retry policy = %#v", first)
	}
}

func TestValidateRetryPolicy(t *testing.T) {
	valid := DefaultRetryPolicy()
	tests := []struct {
		name   string
		policy RetryPolicy
		valid  bool
	}{
		{name: "valid", policy: valid, valid: true},
		{name: "negative retries", policy: RetryPolicy{MaxRetries: -1}},
		{name: "negative initial backoff", policy: RetryPolicy{BackoffInitial: -time.Nanosecond}},
		{name: "negative maximum backoff", policy: RetryPolicy{BackoffMax: -time.Nanosecond}},
		{name: "negative maximum retry after", policy: RetryPolicy{MaxRetryAfter: -time.Nanosecond}},
		{name: "negative jitter", policy: RetryPolicy{BackoffJitter: -0.01}},
		{name: "jitter above one", policy: RetryPolicy{BackoffJitter: 1.01}},
		{name: "invalid status below range", policy: RetryPolicy{HTTPStatuses: map[int]struct{}{99: {}}}},
		{name: "invalid status above range", policy: RetryPolicy{HTTPStatuses: map[int]struct{}{1000: {}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := validateRetryPolicy(test.policy)
			if test.valid {
				if err != nil {
					t.Fatalf("validateRetryPolicy() error = %v", err)
				}
				if got.MaxRetries != test.policy.MaxRetries || len(got.HTTPStatuses) != len(test.policy.HTTPStatuses) {
					t.Errorf("validated policy = %#v", got)
				}
				return
			}
			var configErr *TypeSafeError
			if !errors.As(err, &configErr) {
				t.Errorf("validateRetryPolicy() error = %T %[1]v, want TypeSafeError", err)
			}
		})
	}
}

func TestValidateRetryPolicyCopiesStatusesAndInitializesNilMap(t *testing.T) {
	statuses := map[int]struct{}{http.StatusServiceUnavailable: {}}
	validated, err := validateRetryPolicy(RetryPolicy{HTTPStatuses: statuses})
	if err != nil {
		t.Fatalf("validateRetryPolicy() error = %v", err)
	}
	delete(statuses, http.StatusServiceUnavailable)
	if _, ok := validated.HTTPStatuses[http.StatusServiceUnavailable]; !ok {
		t.Fatal("validated policy aliases caller status map")
	}

	validated, err = validateRetryPolicy(RetryPolicy{})
	if err != nil || validated.HTTPStatuses == nil {
		t.Fatalf("nil status map validation = %#v, %v", validated, err)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name    string
		headers http.Header
		want    time.Duration
	}{
		{name: "milliseconds take precedence", headers: http.Header{"Retry-After-Ms": {"12.5"}, "Retry-After": {"9"}}, want: 12500 * time.Microsecond},
		{name: "seconds", headers: http.Header{"Retry-After": {"1.5"}}, want: 1500 * time.Millisecond},
		{name: "invalid milliseconds falls back to seconds", headers: http.Header{"Retry-After-Ms": {"no"}, "Retry-After": {"2"}}, want: 2 * time.Second},
		{name: "negative milliseconds falls back to seconds", headers: http.Header{"Retry-After-Ms": {"-1"}, "Retry-After": {"0"}}, want: 0},
		{name: "negative seconds rejected", headers: http.Header{"Retry-After": {"-1"}}, want: -1},
		{name: "invalid values rejected", headers: http.Header{"Retry-After": {"tomorrow"}}, want: -1},
		{name: "past HTTP date", headers: http.Header{"Retry-After": {"Mon, 02 Jan 2006 15:04:05 GMT"}}, want: 0},
		{name: "absent", headers: http.Header{}, want: -1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseRetryAfter(test.headers); got != test.want {
				t.Errorf("parseRetryAfter(%v) = %s, want %s", test.headers, got, test.want)
			}
		})
	}
}

func TestRetryDelayUsesRetryAfterAndBackoffBounds(t *testing.T) {
	policy := RetryPolicy{BackoffInitial: 10 * time.Millisecond, BackoffMax: 25 * time.Millisecond, BackoffJitter: 0, RespectRetryAfter: true, MaxRetryAfter: 20 * time.Millisecond}
	tests := []struct {
		name    string
		attempt int
		headers http.Header
		want    time.Duration
	}{
		{name: "accepted retry after", headers: http.Header{"Retry-After-Ms": {"15"}}, want: 15 * time.Millisecond},
		{name: "retry after at limit", headers: http.Header{"Retry-After-Ms": {"20"}}, want: 20 * time.Millisecond},
		{name: "retry after over limit falls back", headers: http.Header{"Retry-After-Ms": {"21"}}, want: 10 * time.Millisecond},
		{name: "first backoff", want: 10 * time.Millisecond},
		{name: "second backoff", attempt: 1, want: 20 * time.Millisecond},
		{name: "backoff capped", attempt: 2, want: 25 * time.Millisecond},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := retryDelay(test.attempt, test.headers, policy); got != test.want {
				t.Errorf("retryDelay() = %s, want %s", got, test.want)
			}
		})
	}
}

func TestWaitForRetryHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := waitForRetry(ctx, time.Hour)
	var aborted *APIUserAbortError
	if !errors.As(err, &aborted) || !errors.Is(err, context.Canceled) {
		t.Errorf("waitForRetry() error = %v, want canceled APIUserAbortError", err)
	}
	if err := waitForRetry(context.Background(), 0); err != nil {
		t.Errorf("waitForRetry(0) error = %v", err)
	}
}
