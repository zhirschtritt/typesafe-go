package typesafe

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"testing"
)

func BenchmarkValidateQuestions(b *testing.B) {
	for _, fixture := range benchmarkQuestionFixtures() {
		b.Run(fixture.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if err := validateQuestions(fixture.questions); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkResponseUnmarshalJSON(b *testing.B) {
	for _, count := range []int{1, 10, 100} {
		data := benchmarkResponseJSON(count)
		b.Run(fmt.Sprintf("answers=%d", count), func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				var response Response
				if err := response.UnmarshalJSON(data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkSystemOne(b *testing.B) {
	questions := map[string]Question{
		"ready": Noul("Is the release ready?", nil),
		"team": Choice("Which team owns this?", map[string]Entry{
			"billing":   "Payments and invoices",
			"support":   "General customer help",
			"technical": "Product defects and integrations",
		}),
		"severity": Score("How severe is the issue?", "Low", "Medium", "High"),
	}
	body := []byte(`{"model":"jev-latest","answers":{"ready":{"type":"noul","noul":0.91},"team":{"type":"choice","choice":"technical","confidence":0.82,"probabilities":{"billing":0.08,"support":0.10,"technical":0.82}},"severity":{"type":"score","score":1.60,"confidence":0.70,"legend":{"0":"Low","1":"Medium","2":"High"},"probabilities":{"0":0.10,"1":0.20,"2":0.70}}},"usage":{"input_tokens":42,"output_tokens":9}}`)
	client, err := NewClient(
		WithAPIKey("benchmark-key"),
		WithHTTPClient(&http.Client{Transport: benchmarkRoundTripper{body: body}}),
		WithRetryPolicy(benchmarkNoRetryPolicy()),
	)
	if err != nil {
		b.Fatal(err)
	}
	state := map[string]any{"ticket": "Export crashes in Safari", "customer_tier": "enterprise"}
	ctx := context.Background()

	b.ReportAllocs()
	for range b.N {
		if _, err := client.SystemOne(ctx, state, questions); err != nil {
			b.Fatal(err)
		}
	}
}

type benchmarkQuestionFixture struct {
	name      string
	questions map[string]Question
}

func benchmarkQuestionFixtures() []benchmarkQuestionFixture {
	mixed := make(map[string]Question, 10)
	for index := range 10 {
		id := strconv.Itoa(index)
		switch index % 3 {
		case 0:
			mixed[id] = Noul(map[string]any{"question": "Is this relevant?", "index": index}, nil)
		case 1:
			mixed[id] = Choice("Choose a route", map[string]Entry{"a": nil, "b": nil, "c": nil})
		case 2:
			mixed[id] = Score("Rate severity", "low", "medium", "high")
		}
	}
	maximumChoice := make(map[string]Entry, 255)
	for index := range 255 {
		maximumChoice[strconv.Itoa(index)] = nil
	}
	return []benchmarkQuestionFixture{
		{name: "single-noul", questions: map[string]Question{"ready": Noul("Is this ready?", nil)}},
		{name: "mixed-10", questions: mixed},
		{name: "choice-255", questions: map[string]Question{"choice": Choice("Choose one", maximumChoice)}},
	}
}

func benchmarkResponseJSON(count int) []byte {
	var body bytes.Buffer
	body.WriteString(`{"model":"jev-latest","answers":{`)
	for index := range count {
		if index > 0 {
			body.WriteByte(',')
		}
		fmt.Fprintf(&body, `%q:`, strconv.Itoa(index))
		switch index % 3 {
		case 0:
			body.WriteString(`{"type":"noul","noul":0.9}`)
		case 1:
			body.WriteString(`{"type":"choice","choice":"a","confidence":0.7,"probabilities":{"a":0.7,"b":0.2,"c":0.1}}`)
		case 2:
			body.WriteString(`{"type":"score","score":4.5,"confidence":0.5,"legend":{"0":"0","1":"1","2":"2","3":"3","4":"4","5":"5","6":"6","7":"7","8":"8","9":"9"},"probabilities":{"0":0.1,"1":0.1,"2":0.1,"3":0.1,"4":0.1,"5":0.1,"6":0.1,"7":0.1,"8":0.1,"9":0.1}}`)
		}
	}
	body.WriteString(`},"usage":{"input_tokens":1,"output_tokens":1}}`)
	return body.Bytes()
}

type benchmarkRoundTripper struct {
	body []byte
}

func (transport benchmarkRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":          {"application/json"},
			"X-TypeSafe-Request-ID": {"req_benchmark"},
		},
		Body: io.NopCloser(bytes.NewReader(transport.body)),
	}, nil
}

func benchmarkNoRetryPolicy() RetryPolicy {
	policy := DefaultRetryPolicy()
	policy.MaxRetries = 0
	policy.HTTPStatuses = map[int]struct{}{}
	policy.RetryConnectionErr = false
	policy.RetryTimeoutErr = false
	return policy
}
