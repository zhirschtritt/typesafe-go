package typesafe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// Answer is a typed System One answer.
type Answer interface {
	AnswerType() string
}

// NoulAnswer is the probability that a Noul question is true.
type NoulAnswer struct {
	Type string  `json:"type"`
	Noul float64 `json:"noul"`
}

// AnswerType returns "noul".
func (NoulAnswer) AnswerType() string { return "noul" }

// ChoiceAnswer is the selected option and its probability distribution.
type ChoiceAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice"`
	Confidence    float64            `json:"confidence"`
	Probabilities map[string]float64 `json:"probabilities"`
}

// AnswerType returns "choice".
func (ChoiceAnswer) AnswerType() string { return "choice" }

// ScoreAnswer is a probability-weighted score and its level distribution.
type ScoreAnswer struct {
	Type          string             `json:"type"`
	Score         float64            `json:"score"`
	Confidence    float64            `json:"confidence"`
	Legend        map[string]Entry   `json:"legend"`
	Probabilities map[string]float64 `json:"probabilities"`
}

// AnswerType returns "score".
func (ScoreAnswer) AnswerType() string { return "score" }

// UnknownAnswer preserves an answer whose type is not yet known by this SDK.
type UnknownAnswer struct {
	Type string
	Raw  json.RawMessage
}

// AnswerType returns the unrecognized wire discriminator.
func (a UnknownAnswer) AnswerType() string { return a.Type }

// MarshalJSON returns the original unknown answer payload.
func (a UnknownAnswer) MarshalJSON() ([]byte, error) {
	if !json.Valid(a.Raw) {
		return nil, fmt.Errorf("typesafe: unknown answer %q has invalid raw JSON", a.Type)
	}
	return a.Raw, nil
}

// Usage reports token consumption for a System One request.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// Response is the result of a System One request.
type Response struct {
	Model     string            `json:"model"`
	Answers   map[string]Answer `json:"answers"`
	Usage     Usage             `json:"usage"`
	RequestID string            `json:"-"`
}

// NoulAnswer returns the Noul answer with id, if it is a Noul answer.
func (r *Response) NoulAnswer(id string) (*NoulAnswer, bool) {
	answer, ok := r.Answers[id]
	if !ok {
		return nil, false
	}
	result, ok := answer.(*NoulAnswer)
	return result, ok
}

// ChoiceAnswer returns the Choice answer with id, if it is a Choice answer.
func (r *Response) ChoiceAnswer(id string) (*ChoiceAnswer, bool) {
	answer, ok := r.Answers[id]
	if !ok {
		return nil, false
	}
	result, ok := answer.(*ChoiceAnswer)
	return result, ok
}

// ScoreAnswer returns the Score answer with id, if it is a Score answer.
func (r *Response) ScoreAnswer(id string) (*ScoreAnswer, bool) {
	answer, ok := r.Answers[id]
	if !ok {
		return nil, false
	}
	result, ok := answer.(*ScoreAnswer)
	return result, ok
}

// UnknownAnswer returns the unknown answer with id, if its type is not known by this SDK.
func (r *Response) UnknownAnswer(id string) (*UnknownAnswer, bool) {
	answer, ok := r.Answers[id]
	if !ok {
		return nil, false
	}
	result, ok := answer.(*UnknownAnswer)
	return result, ok
}

// UnmarshalJSON decodes a System One response and its discriminated answers.
func (r *Response) UnmarshalJSON(data []byte) error {
	var wire struct {
		Model   json.RawMessage            `json:"model"`
		Answers map[string]json.RawMessage `json:"answers"`
		Usage   json.RawMessage            `json:"usage"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("typesafe: decode response: %w", err)
	}
	if missingJSONField(wire.Model) {
		return fmt.Errorf("typesafe: response missing required field model")
	}
	if err := json.Unmarshal(wire.Model, &r.Model); err != nil || r.Model == "" {
		return fmt.Errorf("typesafe: response model must be a non-empty string")
	}
	if wire.Answers == nil {
		return fmt.Errorf("typesafe: response missing required field answers")
	}
	if missingJSONField(wire.Usage) {
		return fmt.Errorf("typesafe: response missing required field usage")
	}
	var usageFields map[string]json.RawMessage
	if err := json.Unmarshal(wire.Usage, &usageFields); err != nil {
		return fmt.Errorf("typesafe: decode response usage: %w", err)
	}
	if missingJSONField(usageFields["input_tokens"]) || missingJSONField(usageFields["output_tokens"]) {
		return fmt.Errorf("typesafe: response usage is missing required token counts")
	}
	if err := json.Unmarshal(wire.Usage, &r.Usage); err != nil || r.Usage.InputTokens < 0 || r.Usage.OutputTokens < 0 {
		return fmt.Errorf("typesafe: response usage must contain non-negative token counts")
	}

	answers := make(map[string]Answer, len(wire.Answers))
	for id, raw := range wire.Answers {
		answer, err := DecodeAnswer(raw)
		if err != nil {
			return fmt.Errorf("typesafe: decode answer %q: %w", id, err)
		}
		answers[id] = answer
	}
	r.Answers = answers
	return nil
}

// DecodeAnswer decodes one answer selected by its type discriminator.
func DecodeAnswer(data []byte) (Answer, error) {
	var discriminator struct {
		Type json.RawMessage `json:"type"`
	}
	if err := json.Unmarshal(data, &discriminator); err != nil {
		return nil, fmt.Errorf("decode answer: %w", err)
	}
	if missingJSONField(discriminator.Type) {
		return nil, fmt.Errorf("answer missing required field type")
	}
	var answerType string
	if err := json.Unmarshal(discriminator.Type, &answerType); err != nil || answerType == "" {
		return nil, fmt.Errorf("answer type must be a non-empty string")
	}

	switch answerType {
	case "noul":
		var answer NoulAnswer
		if err := decodeRequiredAnswer(data, &answer, "noul"); err != nil {
			return nil, err
		}
		if !unitInterval(answer.Noul) {
			return nil, fmt.Errorf("noul must be between 0 and 1")
		}
		return &answer, nil
	case "choice":
		var answer ChoiceAnswer
		if err := decodeRequiredAnswer(data, &answer, "choice", "confidence", "probabilities"); err != nil {
			return nil, err
		}
		if answer.Choice == "" {
			return nil, fmt.Errorf("choice must be a non-empty string")
		}
		if !unitInterval(answer.Confidence) {
			return nil, fmt.Errorf("confidence must be between 0 and 1")
		}
		if _, ok := answer.Probabilities[answer.Choice]; !ok {
			return nil, fmt.Errorf("choice %q is missing from probabilities", answer.Choice)
		}
		if err := validateProbabilities(answer.Probabilities); err != nil {
			return nil, err
		}
		return &answer, nil
	case "score":
		var answer ScoreAnswer
		if err := decodeRequiredAnswer(data, &answer, "score", "confidence", "legend", "probabilities"); err != nil {
			return nil, err
		}
		if !finite(answer.Score) {
			return nil, fmt.Errorf("score must be finite")
		}
		if !unitInterval(answer.Confidence) {
			return nil, fmt.Errorf("confidence must be between 0 and 1")
		}
		minimum, maximum, err := validateScoreLevels(answer.Legend, answer.Probabilities)
		if err != nil {
			return nil, err
		}
		if answer.Score < minimum || answer.Score > maximum {
			return nil, fmt.Errorf("score must be between %v and %v", minimum, maximum)
		}
		if err := validateProbabilities(answer.Probabilities); err != nil {
			return nil, err
		}
		return &answer, nil
	default:
		return &UnknownAnswer{Type: answerType, Raw: bytes.Clone(data)}, nil
	}
}

func decodeRequiredAnswer(data []byte, destination any, valueField string, requiredFields ...string) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	fields := append([]string{valueField}, requiredFields...)
	for _, field := range fields {
		if missingJSONField(raw[field]) {
			return fmt.Errorf("answer missing required field %s", field)
		}
	}
	if err := json.Unmarshal(data, destination); err != nil {
		return err
	}
	return nil
}

func missingJSONField(raw json.RawMessage) bool {
	return len(raw) == 0 || bytes.Equal(raw, []byte("null"))
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func unitInterval(value float64) bool { return finite(value) && value >= 0 && value <= 1 }

func validateProbabilities(probabilities map[string]float64) error {
	if len(probabilities) == 0 {
		return fmt.Errorf("probabilities must contain at least one value")
	}
	for key, probability := range probabilities {
		if !unitInterval(probability) {
			return fmt.Errorf("probability for %q must be between 0 and 1", key)
		}
	}
	return nil
}

func validateScoreLevels(legend map[string]Entry, probabilities map[string]float64) (float64, float64, error) {
	if len(legend) == 0 {
		return 0, 0, fmt.Errorf("legend must contain at least one level")
	}
	if len(legend) != len(probabilities) {
		return 0, 0, fmt.Errorf("legend and probabilities must have the same levels")
	}
	minimum := math.Inf(1)
	maximum := math.Inf(-1)
	for level := range legend {
		value, err := strconv.Atoi(level)
		if err != nil || value < 0 {
			return 0, 0, fmt.Errorf("legend level %q must be a non-negative integer", level)
		}
		if _, ok := probabilities[level]; !ok {
			return 0, 0, fmt.Errorf("legend level %q is missing from probabilities", level)
		}
		minimum = min(minimum, float64(value))
		maximum = max(maximum, float64(value))
	}
	for level := range probabilities {
		if _, ok := legend[level]; !ok {
			return 0, 0, fmt.Errorf("probability level %q is missing from legend", level)
		}
	}
	return minimum, maximum, nil
}

// Model describes a model available to the authenticated account.
type Model struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ReleaseDate string `json:"release_date"`
}

// ModelsResponse is the response from the models endpoint.
type ModelsResponse struct {
	Models    []Model `json:"models"`
	RequestID string  `json:"-"`
}
