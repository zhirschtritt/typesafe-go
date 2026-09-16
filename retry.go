package typesafe

import (
	"context"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	defaultMaxRetries      = 2
	defaultBackoffInitial  = 500 * time.Millisecond
	defaultBackoffMaximum  = 5 * time.Second
	defaultBackoffJitter   = 0.25
	defaultMaxRetryAfter   = time.Minute
	defaultAttemptTimeout  = 10 * time.Second
	defaultResponseMaxSize = int64(8 << 20)
)

// RetryPolicy controls which failed attempts are retried and how long the SDK waits.
type RetryPolicy struct {
	MaxRetries         int
	BackoffInitial     time.Duration
	BackoffMax         time.Duration
	BackoffJitter      float64
	HTTPStatuses       map[int]struct{}
	RespectRetryAfter  bool
	MaxRetryAfter      time.Duration
	RetryConnectionErr bool
	RetryTimeoutErr    bool
}

// DefaultRetryPolicy returns an independent copy of the SDK's default retry settings.
func DefaultRetryPolicy() RetryPolicy {
	statuses := make(map[int]struct{}, 102)
	statuses[http.StatusRequestTimeout] = struct{}{}
	statuses[http.StatusTooManyRequests] = struct{}{}
	for status := 500; status <= 599; status++ {
		statuses[status] = struct{}{}
	}
	return RetryPolicy{
		MaxRetries:         defaultMaxRetries,
		BackoffInitial:     defaultBackoffInitial,
		BackoffMax:         defaultBackoffMaximum,
		BackoffJitter:      defaultBackoffJitter,
		HTTPStatuses:       statuses,
		RespectRetryAfter:  true,
		MaxRetryAfter:      defaultMaxRetryAfter,
		RetryConnectionErr: true,
		RetryTimeoutErr:    true,
	}
}

func validateRetryPolicy(policy RetryPolicy) (RetryPolicy, error) {
	if policy.MaxRetries < 0 {
		return RetryPolicy{}, &TypeSafeError{Message: "typesafe: retry max retries must be non-negative"}
	}
	if policy.BackoffInitial < 0 || policy.BackoffMax < 0 || policy.MaxRetryAfter < 0 {
		return RetryPolicy{}, &TypeSafeError{Message: "typesafe: retry durations must be non-negative"}
	}
	if policy.BackoffJitter < 0 || policy.BackoffJitter > 1 {
		return RetryPolicy{}, &TypeSafeError{Message: "typesafe: retry backoff jitter must be between 0 and 1"}
	}
	if policy.HTTPStatuses == nil {
		policy.HTTPStatuses = make(map[int]struct{})
	}
	for status := range policy.HTTPStatuses {
		if status < 100 || status > 999 {
			return RetryPolicy{}, &TypeSafeError{Message: "typesafe: retry HTTP statuses must be valid HTTP status codes"}
		}
	}
	policy.HTTPStatuses = copyStatuses(policy.HTTPStatuses)
	return policy, nil
}

func copyStatuses(statuses map[int]struct{}) map[int]struct{} {
	copy := make(map[int]struct{}, len(statuses))
	for status := range statuses {
		copy[status] = struct{}{}
	}
	return copy
}

func retryDelay(attempt int, headers http.Header, policy RetryPolicy) time.Duration {
	if policy.RespectRetryAfter {
		if delay := parseRetryAfter(headers); delay >= 0 && delay <= policy.MaxRetryAfter {
			return delay
		}
	}
	delay := policy.BackoffInitial
	for range attempt {
		if delay >= policy.BackoffMax/2 {
			delay = policy.BackoffMax
			break
		}
		delay *= 2
	}
	if delay > policy.BackoffMax {
		delay = policy.BackoffMax
	}
	return time.Duration(float64(delay) * (1 - rand.Float64()*policy.BackoffJitter))
}

func parseRetryAfter(headers http.Header) time.Duration {
	if value := headers.Get("Retry-After-Ms"); value != "" {
		milliseconds, err := strconv.ParseFloat(value, 64)
		if err == nil && milliseconds >= 0 {
			return time.Duration(milliseconds * float64(time.Millisecond))
		}
	}
	value := strings.TrimSpace(headers.Get("Retry-After"))
	if value == "" {
		return -1
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil {
		if seconds >= 0 {
			return time.Duration(seconds * float64(time.Second))
		}
		return -1
	}
	if date, err := http.ParseTime(value); err == nil {
		return max(0, time.Until(date))
	}
	return -1
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return &APIUserAbortError{Err: ctx.Err()}
	case <-timer.C:
		return nil
	}
}
