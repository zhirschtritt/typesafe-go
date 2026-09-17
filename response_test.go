package typesafe

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestDecodeAnswerKnownAndUnknownWireShapes(t *testing.T) {
	tests := []struct {
		name  string
		data  string
		check func(*testing.T, Answer)
	}{
		{
			name: "noul at interval boundary",
			data: `{"type":"noul","noul":0}`,
			check: func(t *testing.T, answer Answer) {
				t.Helper()
				got, ok := answer.(*NoulAnswer)
				if !ok || got.Noul != 0 || got.AnswerType() != "noul" {
					t.Errorf("DecodeAnswer() = %#v, want noul answer at zero", answer)
				}
			},
		},
		{
			name: "choice preserves selected distribution",
			data: `{"type":"choice","choice":"yes","confidence":1,"probabilities":{"yes":1}}`,
			check: func(t *testing.T, answer Answer) {
				t.Helper()
				got, ok := answer.(*ChoiceAnswer)
				if !ok || got.Choice != "yes" || got.Confidence != 1 || got.Probabilities["yes"] != 1 {
					t.Errorf("DecodeAnswer() = %#v, want choice wire data", answer)
				}
			},
		},
		{
			name: "score accepts sparse numeric levels",
			data: `{"type":"score","score":2,"confidence":0.5,"legend":{"0":"low","2":"high"},"probabilities":{"0":0.25,"2":0.75}}`,
			check: func(t *testing.T, answer Answer) {
				t.Helper()
				got, ok := answer.(*ScoreAnswer)
				if !ok || got.Score != 2 || got.Legend["2"] != "high" {
					t.Errorf("DecodeAnswer() = %#v, want score wire data", answer)
				}
			},
		},
		{
			name: "future type retains exact payload",
			data: `{"type":"ranking","ranked":["a","b"],"metadata":{"stable":true}}`,
			check: func(t *testing.T, answer Answer) {
				t.Helper()
				got, ok := answer.(*UnknownAnswer)
				if !ok || got.AnswerType() != "ranking" {
					t.Fatalf("DecodeAnswer() = %#v, want unknown ranking answer", answer)
				}
				encoded, err := json.Marshal(got)
				if err != nil || string(encoded) != `{"type":"ranking","ranked":["a","b"],"metadata":{"stable":true}}` {
					t.Errorf("MarshalJSON() = %s, %v; want original unknown payload", encoded, err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			answer, err := DecodeAnswer([]byte(test.data))
			if err != nil {
				t.Fatalf("DecodeAnswer() error = %v", err)
			}
			test.check(t, answer)
		})
	}
}

func TestDecodeAnswerRejectsInvalidWireData(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{"malformed JSON", `{`, "decode answer"},
		{"missing type", `{"noul":1}`, "type must be a non-empty string"},
		{"noul missing value", `{"type":"noul"}`, "missing required field noul"},
		{"noul outside interval", `{"type":"noul","noul":1.1}`, "noul must be between 0 and 1"},
		{"choice missing selection", `{"type":"choice","confidence":1,"probabilities":{"yes":1}}`, "missing required field choice"},
		{"choice missing from distribution", `{"type":"choice","choice":"yes","confidence":1,"probabilities":{"no":1}}`, "missing from probabilities"},
		{"choice invalid probability", `{"type":"choice","choice":"yes","confidence":1,"probabilities":{"yes":-0.1}}`, "probability for \"yes\""},
		{"score nonnumeric level", `{"type":"score","score":0,"confidence":1,"legend":{"low":"low"},"probabilities":{"low":1}}`, "legend level \"low\""},
		{"score mismatched levels", `{"type":"score","score":0,"confidence":1,"legend":{"0":"low"},"probabilities":{"1":1}}`, "missing from probabilities"},
		{"score outside levels", `{"type":"score","score":2,"confidence":1,"legend":{"0":"low","1":"high"},"probabilities":{"0":0,"1":1}}`, "score must be between 0 and 1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeAnswer([]byte(test.data))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("DecodeAnswer() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestResponseUnmarshalAndTypedAccessors(t *testing.T) {
	var response Response
	data := []byte(`{"model":"jev","answers":{"n":{"type":"noul","noul":1},"c":{"type":"choice","choice":"a","confidence":1,"probabilities":{"a":1}},"s":{"type":"score","score":0,"confidence":1,"legend":{"0":"low"},"probabilities":{"0":1}},"u":{"type":"future","value":true}},"usage":{"input_tokens":2,"output_tokens":3}}`)
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if response.Model != "jev" || response.Usage != (Usage{InputTokens: 2, OutputTokens: 3}) || string(response.raw) != string(data) {
		t.Errorf("response metadata = %#v", response)
	}
	for _, test := range []struct {
		name string
		got  func() (Answer, bool)
	}{
		{"noul", func() (Answer, bool) { answer, ok := response.NoulAnswer("n"); return answer, ok }},
		{"choice", func() (Answer, bool) { answer, ok := response.ChoiceAnswer("c"); return answer, ok }},
		{"score", func() (Answer, bool) { answer, ok := response.ScoreAnswer("s"); return answer, ok }},
		{"unknown", func() (Answer, bool) { answer, ok := response.UnknownAnswer("u"); return answer, ok }},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, ok := test.got(); !ok {
				t.Errorf("matching accessor did not return %s answer", test.name)
			}
		})
	}
	if answer, ok := response.NoulAnswer("c"); ok || answer != nil {
		t.Errorf("NoulAnswer(choice) = %#v, %t; want nil, false", answer, ok)
	}
	if answer, ok := response.ScoreAnswer("missing"); ok || answer != nil {
		t.Errorf("ScoreAnswer(missing) = %#v, %t; want nil, false", answer, ok)
	}
}

func TestResponseUnmarshalRejectsInvalidEnvelope(t *testing.T) {
	tests := []struct{ name, data, want string }{
		{"invalid JSON", `{`, "decode response"},
		{"missing model", `{"answers":{},"usage":{"input_tokens":0,"output_tokens":0}}`, "model must be a non-empty string"},
		{"missing answers", `{"model":"jev","usage":{"input_tokens":0,"output_tokens":0}}`, "missing required field answers"},
		{"missing usage count", `{"model":"jev","answers":{},"usage":{"input_tokens":0}}`, "missing required token counts"},
		{"negative usage", `{"model":"jev","answers":{},"usage":{"input_tokens":-1,"output_tokens":0}}`, "non-negative token counts"},
		{"invalid nested answer", `{"model":"jev","answers":{"q":{"type":"noul","noul":2}},"usage":{"input_tokens":0,"output_tokens":0}}`, "decode answer \"q\""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var response Response
			err := response.UnmarshalJSON([]byte(test.data))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("Unmarshal() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestUnknownAnswerAndResponseValidationBoundaries(t *testing.T) {
	if _, err := (UnknownAnswer{Type: "future", Raw: []byte(`not JSON`)}).MarshalJSON(); err == nil {
		t.Error("UnknownAnswer.MarshalJSON() error = nil for invalid raw JSON")
	}
	if err := validateProbabilities(map[string]float64{"a": math.NaN()}); err == nil {
		t.Error("validateProbabilities(NaN) error = nil")
	}

	questions := map[string]Question{"team": Choice("team?", map[string]Entry{"a": nil, "b": nil})}
	response := &Response{Answers: map[string]Answer{"team": &ChoiceAnswer{Type: "choice", Choice: "a", Probabilities: map[string]float64{"a": 0.5, "b": 0.5}}}}
	if err := validateResponseForQuestions(response, questions); err != nil {
		t.Errorf("validateResponseForQuestions() error = %v, want nil", err)
	}
	response.Answers["team"] = &ChoiceAnswer{Type: "choice", Choice: "a", Probabilities: map[string]float64{"a": 0.4, "b": 0.6}}
	if err := validateResponseForQuestions(response, questions); err == nil || !strings.Contains(err.Error(), "not a highest-probability") {
		t.Errorf("validateResponseForQuestions() error = %v, want choice consistency error", err)
	}
}
