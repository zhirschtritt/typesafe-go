package typesafe_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/zhirschtritt/typesafe-go"
)

func TestSystemOneSendsWirePayloadAndProtectsHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/systemone" {
			t.Errorf("request = %s %s, want POST /v1/systemone", r.Method, r.URL.Path)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q, want protected bearer token", got)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		if got := r.Header.Get("X-Customer-Header"); got != "kept" {
			t.Errorf("custom header = %q, want kept", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
		if got := r.Header.Get("X-TypeSafe-Retry-Count"); got != "" {
			t.Errorf("retry count = %q, want absent on first attempt", got)
		}
		body, _ := io.ReadAll(r.Body)
		const want = `{"state":{"text":"hello"},"questions":{"ready":{"type":"noul","instructions":"Is it ready?"}},"model":"jev-latest"}`
		if string(body) != want {
			t.Errorf("request body = %s, want %s", body, want)
		}
		w.Header().Set("X-TypeSafe-Request-ID", "req_123")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model":"jev-latest","answers":{"ready":{"type":"noul","noul":0.9}},"usage":{"input_tokens":3,"output_tokens":2}}`)
	}))
	defer server.Close()

	client := mustClient(t, typesafe.WithBaseURL(server.URL), typesafe.WithHeaders(http.Header{
		"authorization":          {"Bearer attacker"},
		"accept":                 {"text/plain"},
		"content-type":           {"text/plain"},
		"X-Customer-Header":      {"kept"},
		"X-TypeSafe-Retry-Count": {"999"},
	}))
	response, err := client.SystemOne(context.Background(), map[string]any{"text": "hello"}, map[string]typesafe.Question{
		"ready": typesafe.Noul("Is it ready?", nil),
	})
	if err != nil {
		t.Fatalf("SystemOne() error = %v", err)
	}
	if response.RequestID != "req_123" {
		t.Errorf("RequestID = %q, want req_123", response.RequestID)
	}
}

func TestSystemOneDecodesMixedAnswersAndUnknownForwardType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-TypeSafe-Request-ID", "req_mixed")
		_, _ = io.WriteString(w, `{"model":"jev-2026","answers":{"safe":{"type":"noul","noul":0.92,"extra":true},"team":{"type":"choice","choice":"technical","confidence":0.82,"probabilities":{"billing":0.08,"technical":0.92}},"severity":{"type":"score","score":1.6,"confidence":0.78,"legend":{"0":"Low","1":"Medium","2":"High"},"probabilities":{"0":0.1,"1":0.2,"2":0.7}},"future":{"type":"ranking","ranked":["a","b"]}},"usage":{"input_tokens":312,"output_tokens":48},"future_field":true}`)
	}))
	defer server.Close()
	client := mustClient(t, typesafe.WithBaseURL(server.URL))
	response, err := client.SystemOne(context.Background(), "state", map[string]typesafe.Question{
		"safe": typesafe.Noul("safe?", nil), "team": typesafe.Choice("team?", map[string]typesafe.Entry{"billing": nil, "technical": nil}), "severity": typesafe.Score("severity?", "Low", "Medium", "High"), "future": typesafe.Noul("future?", nil),
	})
	if err != nil {
		t.Fatalf("SystemOne() error = %v", err)
	}
	if answer, ok := response.NoulAnswer("safe"); !ok || answer.Noul != 0.92 {
		t.Errorf("NoulAnswer() = %#v, %t", answer, ok)
	}
	if answer, ok := response.ChoiceAnswer("team"); !ok || answer.Choice != "technical" || answer.Probabilities["technical"] != 0.92 {
		t.Errorf("ChoiceAnswer() = %#v, %t", answer, ok)
	}
	if answer, ok := response.ScoreAnswer("severity"); !ok || answer.Score != 1.6 || answer.Legend["2"] != "High" {
		t.Errorf("ScoreAnswer() = %#v, %t", answer, ok)
	}
	unknown, ok := response.UnknownAnswer("future")
	if !ok || unknown.AnswerType() != "ranking" || !strings.Contains(string(unknown.Raw), `"ranked"`) {
		t.Errorf("UnknownAnswer() = %#v, %t", unknown, ok)
	}
	if response.RequestID != "req_mixed" || response.Usage.InputTokens != 312 {
		t.Errorf("response metadata = %#v", response)
	}
}

func TestListModelsUsesDefaultModelAndReturnsRequestID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("models request did not authenticate")
		}
		w.Header().Set("X-TypeSafe-Request-ID", "req_models")
		_, _ = io.WriteString(w, `{"models":[{"name":"jev-latest","description":"General","release_date":"2026-09-15"}]}`)
	}))
	defer server.Close()
	client := mustClient(t, typesafe.WithBaseURL(server.URL))
	response, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if response.RequestID != "req_models" || len(response.Models) != 1 || response.Models[0].Name != "jev-latest" {
		t.Errorf("models response = %#v", response)
	}
}

func TestSystemOneRetriesRetryAfterAndCancellationStopsRetrying(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt := attempts.Add(1)
		if attempt == 1 {
			w.Header().Set("Retry-After-Ms", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":"slow down"}`)
			return
		}
		if r.Header.Get("X-TypeSafe-Retry-Count") != "1" {
			t.Errorf("retry header = %q", r.Header.Get("X-TypeSafe-Retry-Count"))
		}
		_, _ = io.WriteString(w, `{"model":"jev-latest","answers":{"ready":{"type":"noul","noul":1}},"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer server.Close()
	policy := typesafe.DefaultRetryPolicy()
	policy.MaxRetries = 1
	policy.BackoffJitter = 0
	client := mustClient(t, typesafe.WithBaseURL(server.URL), typesafe.WithRetryPolicy(policy))
	_, err := client.SystemOne(context.Background(), "state", map[string]typesafe.Question{"ready": typesafe.Noul("ready?", nil)})
	if err != nil || attempts.Load() != 2 {
		t.Errorf("retry result error=%v attempts=%d, want nil and 2", err, attempts.Load())
	}

	attempts.Store(0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.SystemOne(ctx, "state", map[string]typesafe.Question{"ready": typesafe.Noul("ready?", nil)})
	var aborted *typesafe.APIUserAbortError
	if !errors.As(err, &aborted) || attempts.Load() != 0 {
		t.Errorf("canceled result error=%v attempts=%d", err, attempts.Load())
	}
}

func TestSystemOneReturnsBoundedTypedHTTPErrorAndRejectsInvalidInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-TypeSafe-Request-ID", "req_denied")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"invalid API key"}`)
	}))
	defer server.Close()
	client := mustClient(t, typesafe.WithBaseURL(server.URL), typesafe.WithResponseBodyLimit(1024), typesafe.WithRetryPolicy(noRetryPolicy()))
	_, err := client.SystemOne(context.Background(), "state", map[string]typesafe.Question{"ready": typesafe.Noul("ready?", nil)})
	var auth *typesafe.AuthenticationError
	if !errors.As(err, &auth) || auth.StatusCode != http.StatusUnauthorized || auth.RequestID != "req_denied" || auth.Headers.Get("X-TypeSafe-Request-ID") != "req_denied" {
		t.Errorf("HTTP error = %#v", err)
	}
	_, err = client.SystemOne(context.Background(), "state", map[string]typesafe.Question{})
	if err == nil {
		t.Error("SystemOne(empty questions) error = nil")
	}
	_, err = client.SystemOne(context.Background(), "state", map[string]typesafe.Question{"bad": typesafe.Score("score?", "only one")})
	if err == nil {
		t.Error("SystemOne(invalid score) error = nil")
	}
	_, err = client.SystemOne(context.Background(), nil, map[string]typesafe.Question{"ready": typesafe.Noul("ready?", nil)})
	if err == nil {
		t.Error("SystemOne(nil state) error = nil")
	}
	_, err = client.SystemOne(context.Background(), 42, map[string]typesafe.Question{"ready": typesafe.Noul("ready?", nil)})
	if err == nil {
		t.Error("SystemOne(number state) error = nil")
	}
	_, err = client.SystemOne(context.Background(), "state", map[string]typesafe.Question{"ready": typesafe.Noul(true, nil)})
	if err == nil {
		t.Error("SystemOne(boolean instructions) error = nil")
	}
	choiceCriteria := make(map[string]typesafe.Entry, 256)
	for index := range 256 {
		choiceCriteria[fmt.Sprint(index)] = nil
	}
	_, err = client.SystemOne(context.Background(), "state", map[string]typesafe.Question{"choice": typesafe.Choice("pick", choiceCriteria)})
	if err == nil {
		t.Error("SystemOne(256 choice options) error = nil")
	}
	_, err = client.SystemOne(context.Background(), "state", map[string]typesafe.Question{"score": typesafe.Score("rate", "0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10")})
	if err == nil {
		t.Error("SystemOne(11 score levels) error = nil")
	}
	if _, err := typesafe.NewClient(typesafe.WithAPIKey("key"), typesafe.WithResponseBodyLimit(0)); err == nil {
		t.Error("NewClient(invalid body limit) error = nil")
	}
	_, err = client.SystemOne(context.Background(), "state", map[string]typesafe.Question{"ready": typesafe.Noul("ready?", nil)}, typesafe.WithModel(""))
	if err == nil {
		t.Error("SystemOne(empty model) error = nil")
	}
}

func TestSystemOneRejectsResponsesThatContradictQuestions(t *testing.T) {
	tests := []struct {
		name      string
		questions map[string]typesafe.Question
		response  string
	}{
		{
			name:      "missing answer",
			questions: map[string]typesafe.Question{"ready": typesafe.Noul("ready?", nil)},
			response:  `{"model":"jev-latest","answers":{},"usage":{"input_tokens":1,"output_tokens":1}}`,
		},
		{
			name:      "wrong answer type",
			questions: map[string]typesafe.Question{"ready": typesafe.Noul("ready?", nil)},
			response:  `{"model":"jev-latest","answers":{"ready":{"type":"choice","choice":"yes","confidence":1,"probabilities":{"yes":1}}},"usage":{"input_tokens":1,"output_tokens":1}}`,
		},
		{
			name:      "unknown choice option",
			questions: map[string]typesafe.Question{"team": typesafe.Choice("team?", map[string]typesafe.Entry{"billing": nil, "technical": nil})},
			response:  `{"model":"jev-latest","answers":{"team":{"type":"choice","choice":"sales","confidence":1,"probabilities":{"billing":0,"sales":1}}},"usage":{"input_tokens":1,"output_tokens":1}}`,
		},
		{
			name:      "invalid probability sum",
			questions: map[string]typesafe.Question{"team": typesafe.Choice("team?", map[string]typesafe.Entry{"billing": nil, "technical": nil})},
			response:  `{"model":"jev-latest","answers":{"team":{"type":"choice","choice":"billing","confidence":1,"probabilities":{"billing":0.8,"technical":0.1}}},"usage":{"input_tokens":1,"output_tokens":1}}`,
		},
		{
			name:      "inconsistent score mean",
			questions: map[string]typesafe.Question{"severity": typesafe.Score("severity?", "low", "high")},
			response:  `{"model":"jev-latest","answers":{"severity":{"type":"score","score":0.9,"confidence":1,"legend":{"0":"low","1":"high"},"probabilities":{"0":0.9,"1":0.1}}},"usage":{"input_tokens":1,"output_tokens":1}}`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("X-TypeSafe-Request-ID", "req_invalid")
				_, _ = io.WriteString(w, test.response)
			}))
			defer server.Close()
			_, err := mustClient(t, typesafe.WithBaseURL(server.URL)).SystemOne(context.Background(), "state", test.questions)
			var validation *typesafe.ResponseValidationError
			if !errors.As(err, &validation) || validation.RequestID != "req_invalid" || len(validation.Body) == 0 {
				t.Fatalf("SystemOne() error = %#v, want response validation error with request context", err)
			}
		})
	}
}

func TestSystemOneRejectsResponseLargerThanConfiguredLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, strings.Repeat("x", 20)) }))
	defer server.Close()
	client := mustClient(t, typesafe.WithBaseURL(server.URL), typesafe.WithResponseBodyLimit(10), typesafe.WithRetryPolicy(noRetryPolicy()))
	_, err := client.SystemOne(context.Background(), "state", map[string]typesafe.Question{"ready": typesafe.Noul("ready?", nil)})
	var tooLarge *typesafe.ResponseBodyTooLargeError
	if !errors.As(err, &tooLarge) || tooLarge.Limit != 10 {
		t.Errorf("error = %v, want response limit error", err)
	}
}

func mustClient(t *testing.T, options ...typesafe.Option) *typesafe.Client {
	t.Helper()
	client, err := typesafe.NewClient(append([]typesafe.Option{typesafe.WithAPIKey("test-key"), typesafe.WithDefaultModel("jev-latest")}, options...)...)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client
}

func noRetryPolicy() typesafe.RetryPolicy {
	policy := typesafe.DefaultRetryPolicy()
	policy.MaxRetries = 0
	policy.BackoffInitial = 0
	policy.BackoffMax = 0
	policy.BackoffJitter = 0
	policy.HTTPStatuses = map[int]struct{}{}
	policy.RespectRetryAfter = false
	policy.MaxRetryAfter = 0
	policy.RetryConnectionErr = false
	policy.RetryTimeoutErr = false
	return policy
}
