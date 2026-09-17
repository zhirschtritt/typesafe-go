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
	raw       []byte
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
		Model   *string                    `json:"model"`
		Answers map[string]json.RawMessage `json:"answers"`
		Usage   *struct {
			InputTokens  *int `json:"input_tokens"`
			OutputTokens *int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return fmt.Errorf("typesafe: decode response: %w", err)
	}
	if wire.Model == nil || *wire.Model == "" {
		return fmt.Errorf("typesafe: response model must be a non-empty string")
	}
	if wire.Answers == nil {
		return fmt.Errorf("typesafe: response missing required field answers")
	}
	if wire.Usage == nil || wire.Usage.InputTokens == nil || wire.Usage.OutputTokens == nil {
		return fmt.Errorf("typesafe: response usage is missing required token counts")
	}
	if *wire.Usage.InputTokens < 0 || *wire.Usage.OutputTokens < 0 {
		return fmt.Errorf("typesafe: response usage must contain non-negative token counts")
	}
	r.Model = *wire.Model
	r.Usage = Usage{InputTokens: *wire.Usage.InputTokens, OutputTokens: *wire.Usage.OutputTokens}

	answers := make(map[string]Answer, len(wire.Answers))
	for id, raw := range wire.Answers {
		answer, err := DecodeAnswer(raw)
		if err != nil {
			return fmt.Errorf("typesafe: decode answer %q: %w", id, err)
		}
		answers[id] = answer
	}
	r.Answers = answers
	r.raw = data
	return nil
}

// DecodeAnswer decodes one answer selected by its type discriminator.
func DecodeAnswer(data []byte) (Answer, error) {
	var wire struct {
		Type          string             `json:"type"`
		Noul          *float64           `json:"noul"`
		Choice        *string            `json:"choice"`
		Score         *float64           `json:"score"`
		Confidence    *float64           `json:"confidence"`
		Legend        map[string]Entry   `json:"legend"`
		Probabilities map[string]float64 `json:"probabilities"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, fmt.Errorf("decode answer: %w", err)
	}
	if wire.Type == "" {
		return nil, fmt.Errorf("answer type must be a non-empty string")
	}

	switch wire.Type {
	case "noul":
		if wire.Noul == nil {
			return nil, fmt.Errorf("answer missing required field noul")
		}
		if !unitInterval(*wire.Noul) {
			return nil, fmt.Errorf("noul must be between 0 and 1")
		}
		return &NoulAnswer{Type: wire.Type, Noul: *wire.Noul}, nil
	case "choice":
		if wire.Choice == nil {
			return nil, fmt.Errorf("answer missing required field choice")
		}
		if wire.Confidence == nil {
			return nil, fmt.Errorf("answer missing required field confidence")
		}
		if wire.Probabilities == nil {
			return nil, fmt.Errorf("answer missing required field probabilities")
		}
		if *wire.Choice == "" {
			return nil, fmt.Errorf("choice must be a non-empty string")
		}
		if !unitInterval(*wire.Confidence) {
			return nil, fmt.Errorf("confidence must be between 0 and 1")
		}
		if _, ok := wire.Probabilities[*wire.Choice]; !ok {
			return nil, fmt.Errorf("choice %q is missing from probabilities", *wire.Choice)
		}
		if err := validateProbabilities(wire.Probabilities); err != nil {
			return nil, err
		}
		return &ChoiceAnswer{Type: wire.Type, Choice: *wire.Choice, Confidence: *wire.Confidence, Probabilities: wire.Probabilities}, nil
	case "score":
		if wire.Score == nil {
			return nil, fmt.Errorf("answer missing required field score")
		}
		if wire.Confidence == nil {
			return nil, fmt.Errorf("answer missing required field confidence")
		}
		if wire.Legend == nil {
			return nil, fmt.Errorf("answer missing required field legend")
		}
		if wire.Probabilities == nil {
			return nil, fmt.Errorf("answer missing required field probabilities")
		}
		if !finite(*wire.Score) {
			return nil, fmt.Errorf("score must be finite")
		}
		if !unitInterval(*wire.Confidence) {
			return nil, fmt.Errorf("confidence must be between 0 and 1")
		}
		minimum, maximum, err := validateScoreLevels(wire.Legend, wire.Probabilities)
		if err != nil {
			return nil, err
		}
		if *wire.Score < minimum || *wire.Score > maximum {
			return nil, fmt.Errorf("score must be between %v and %v", minimum, maximum)
		}
		if err := validateProbabilities(wire.Probabilities); err != nil {
			return nil, err
		}
		return &ScoreAnswer{Type: wire.Type, Score: *wire.Score, Confidence: *wire.Confidence, Legend: wire.Legend, Probabilities: wire.Probabilities}, nil
	default:
		return &UnknownAnswer{Type: wire.Type, Raw: bytes.Clone(data)}, nil
	}
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

const responseRoundingError = 0.005

func validateResponseForQuestions(response *Response, questions map[string]Question) error {
	if len(response.Answers) != len(questions) {
		return fmt.Errorf("expected exactly one answer for each question")
	}
	for id, question := range questions {
		answer, ok := response.Answers[id]
		if !ok {
			return fmt.Errorf("question %q is missing an answer", id)
		}
		if _, unknown := answer.(*UnknownAnswer); unknown {
			continue
		}
		if answer.AnswerType() != question.questionType() {
			return fmt.Errorf("question %q returned answer type %q, want %q", id, answer.AnswerType(), question.questionType())
		}
		switch typedQuestion := question.(type) {
		case ChoiceQuestion:
			if err := validateChoiceAnswerForQuestion(response.Answers[id].(*ChoiceAnswer), typedQuestion); err != nil {
				return fmt.Errorf("question %q: %w", id, err)
			}
		case *ChoiceQuestion:
			if err := validateChoiceAnswerForQuestion(response.Answers[id].(*ChoiceAnswer), *typedQuestion); err != nil {
				return fmt.Errorf("question %q: %w", id, err)
			}
		case ScoreQuestion:
			if err := validateScoreAnswerForQuestion(response.Answers[id].(*ScoreAnswer), typedQuestion); err != nil {
				return fmt.Errorf("question %q: %w", id, err)
			}
		case *ScoreQuestion:
			if err := validateScoreAnswerForQuestion(response.Answers[id].(*ScoreAnswer), *typedQuestion); err != nil {
				return fmt.Errorf("question %q: %w", id, err)
			}
		}
	}
	return nil
}

func validateChoiceAnswerForQuestion(answer *ChoiceAnswer, question ChoiceQuestion) error {
	if len(answer.Probabilities) != len(question.Criteria) {
		return fmt.Errorf("probabilities must contain exactly the requested options")
	}
	selected, ok := answer.Probabilities[answer.Choice]
	if !ok {
		return fmt.Errorf("choice %q is not a requested option", answer.Choice)
	}
	for option, probability := range answer.Probabilities {
		if _, ok := question.Criteria[option]; !ok {
			return fmt.Errorf("probability contains unknown option %q", option)
		}
		if probability > selected+1e-9 {
			return fmt.Errorf("choice %q is not a highest-probability option", answer.Choice)
		}
	}
	return validateProbabilitySum(answer.Probabilities)
}

func validateScoreAnswerForQuestion(answer *ScoreAnswer, question ScoreQuestion) error {
	if len(answer.Probabilities) != len(question.Criteria) || len(answer.Legend) != len(question.Criteria) {
		return fmt.Errorf("legend and probabilities must contain exactly the requested levels")
	}
	mean := 0.0
	meanRoundingError := responseRoundingError
	for level := range question.Criteria {
		key := strconv.Itoa(level)
		probability, ok := answer.Probabilities[key]
		if !ok {
			return fmt.Errorf("probabilities are missing level %q", key)
		}
		if _, ok := answer.Legend[key]; !ok {
			return fmt.Errorf("legend is missing level %q", key)
		}
		mean += float64(level) * probability
		meanRoundingError += float64(level) * responseRoundingError
	}
	if answer.Score < 0 || answer.Score > float64(len(question.Criteria)-1) {
		return fmt.Errorf("score must be between 0 and %d", len(question.Criteria)-1)
	}
	if math.Abs(answer.Score-mean) > meanRoundingError+1e-9 {
		return fmt.Errorf("score must equal the probability-weighted mean within response rounding precision")
	}
	return validateProbabilitySum(answer.Probabilities)
}

func validateProbabilitySum(probabilities map[string]float64) error {
	sum := 0.0
	for _, probability := range probabilities {
		sum += probability
	}
	if math.Abs(sum-1) > float64(len(probabilities))*responseRoundingError+1e-9 {
		return fmt.Errorf("probabilities must sum to 1 within response rounding precision")
	}
	return nil
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
