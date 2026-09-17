package typesafe_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/zhirschtritt/typesafe-go"
)

func ExampleClient_SystemOne() {
	transport := roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     func() http.Header { h := make(http.Header); h.Set("X-TypeSafe-Request-ID", "req_example"); return h }(),
			Body: io.NopCloser(strings.NewReader(`{
				"model":"jev-latest",
				"answers":{
					"safe":{"type":"noul","noul":0.97},
					"audience":{"type":"choice","choice":"engineering","confidence":0.88,"probabilities":{"engineering":0.88,"customers":0.12}},
					"confidence":{"type":"score","score":1.7,"confidence":0.81,"legend":{"0":"Low","1":"Medium","2":"High"},"probabilities":{"0":0.1,"1":0.1,"2":0.8}}
				},
				"usage":{"input_tokens":42,"output_tokens":9}
			}`)),
		}, nil
	})
	client, err := typesafe.NewClient(
		typesafe.WithAPIKey("example-key"),
		typesafe.WithHTTPClient(&http.Client{Transport: transport}),
	)
	if err != nil {
		panic(err)
	}

	response, err := client.SystemOne(context.Background(),
		map[string]any{"draft": "Ship the migration Friday."},
		map[string]typesafe.Question{
			"safe": typesafe.Noul("Is the draft safe to send?", nil),
			"audience": typesafe.Choice("Who is the best audience?", map[string]typesafe.Entry{
				"engineering": "Technical stakeholders",
				"customers":   "External customers",
			}),
			"confidence": typesafe.Score("How likely is Friday?", "Low", "Medium", "High"),
		},
	)
	if err != nil {
		panic(err)
	}

	safe, _ := response.NoulAnswer("safe")
	audience, _ := response.ChoiceAnswer("audience")
	confidence, _ := response.ScoreAnswer("confidence")
	fmt.Printf("%.2f %s %.1f %s\n", safe.Noul, audience.Choice, confidence.Score, response.RequestID)
	// Output: 0.97 engineering 1.7 req_example
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}
