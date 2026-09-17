package typesafe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
)

// Question describes a typed System One question.
type Question interface {
	questionType() string
	validate() error
}

// NoulCriteria optionally clarifies what true and false mean for a Noul question.
type NoulCriteria struct {
	True  Entry `json:"true,omitempty"`
	False Entry `json:"false,omitempty"`
}

// NoulQuestion asks a yes-or-no question.
type NoulQuestion struct {
	Instructions Entry         `json:"instructions"`
	Criteria     *NoulCriteria `json:"criteria,omitempty"`
}

// Noul creates a Noul question. A nil criteria omits criteria from the request.
func Noul(instructions Entry, criteria *NoulCriteria) NoulQuestion {
	return NoulQuestion{Instructions: instructions, Criteria: criteria}
}

func (NoulQuestion) questionType() string { return "noul" }

func (q NoulQuestion) validate() error {
	if err := validateInputEntry(q.Instructions, true); err != nil {
		return &TypeSafeError{Message: "typesafe: noul instructions " + err.Error()}
	}
	if q.Criteria != nil {
		if err := validateInputEntry(q.Criteria.True, true); err != nil {
			return &TypeSafeError{Message: "typesafe: noul true criterion " + err.Error()}
		}
		if err := validateInputEntry(q.Criteria.False, true); err != nil {
			return &TypeSafeError{Message: "typesafe: noul false criterion " + err.Error()}
		}
	}
	return nil
}

// MarshalJSON adds the immutable Noul wire discriminator.
func (q NoulQuestion) MarshalJSON() ([]byte, error) {
	type payload NoulQuestion
	return json.Marshal(struct {
		Type string `json:"type"`
		payload
	}{Type: q.questionType(), payload: payload(q)})
}

// ChoiceQuestion asks the model to select one named option.
type ChoiceQuestion struct {
	Instructions Entry            `json:"instructions"`
	Criteria     map[string]Entry `json:"criteria"`
}

// Choice creates a Choice question. criteria must contain between one and 255 options.
func Choice(instructions Entry, criteria map[string]Entry) ChoiceQuestion {
	return ChoiceQuestion{Instructions: instructions, Criteria: criteria}
}

func (ChoiceQuestion) questionType() string { return "choice" }

func (q ChoiceQuestion) validate() error {
	if err := validateInputEntry(q.Instructions, true); err != nil {
		return &TypeSafeError{Message: "typesafe: choice instructions " + err.Error()}
	}
	if len(q.Criteria) == 0 || len(q.Criteria) > 255 {
		return &TypeSafeError{Message: "typesafe: choice question requires between one and 255 criteria"}
	}
	for option, criterion := range q.Criteria {
		if option == "" {
			return &TypeSafeError{Message: "typesafe: choice criterion name must not be empty"}
		}
		if err := validateInputEntry(criterion, true); err != nil {
			return &TypeSafeError{Message: fmt.Sprintf("typesafe: choice criterion %q %s", option, err)}
		}
	}
	return nil
}

// MarshalJSON adds the immutable Choice wire discriminator.
func (q ChoiceQuestion) MarshalJSON() ([]byte, error) {
	type payload ChoiceQuestion
	return json.Marshal(struct {
		Type string `json:"type"`
		payload
	}{Type: q.questionType(), payload: payload(q)})
}

// ScoreQuestion asks the model to score state against ordered criteria.
type ScoreQuestion struct {
	Instructions Entry   `json:"instructions"`
	Criteria     []Entry `json:"criteria"`
}

// Score creates a Score question. criteria must contain between two and 10 ordered levels.
func Score(instructions Entry, criteria ...Entry) ScoreQuestion {
	return ScoreQuestion{Instructions: instructions, Criteria: criteria}
}

func (ScoreQuestion) questionType() string { return "score" }

func (q ScoreQuestion) validate() error {
	if err := validateInputEntry(q.Instructions, true); err != nil {
		return &TypeSafeError{Message: "typesafe: score instructions " + err.Error()}
	}
	if len(q.Criteria) < 2 || len(q.Criteria) > 10 {
		return &TypeSafeError{Message: "typesafe: score question requires between two and 10 criteria"}
	}
	for level, criterion := range q.Criteria {
		if err := validateInputEntry(criterion, true); err != nil {
			return &TypeSafeError{Message: fmt.Sprintf("typesafe: score criterion %d %s", level, err)}
		}
	}
	return nil
}

// MarshalJSON adds the immutable Score wire discriminator.
func (q ScoreQuestion) MarshalJSON() ([]byte, error) {
	type payload ScoreQuestion
	return json.Marshal(struct {
		Type string `json:"type"`
		payload
	}{Type: q.questionType(), payload: payload(q)})
}

func validateQuestions(questions map[string]Question) error {
	if len(questions) == 0 {
		return &TypeSafeError{Message: "typesafe: at least one question is required"}
	}
	for id, question := range questions {
		if id == "" {
			return &TypeSafeError{Message: "typesafe: question id must not be empty"}
		}
		if question == nil || (reflect.ValueOf(question).Kind() == reflect.Pointer && reflect.ValueOf(question).IsNil()) {
			return &TypeSafeError{Message: fmt.Sprintf("typesafe: question %q must not be nil", id)}
		}
		if err := question.validate(); err != nil {
			return &TypeSafeError{Message: fmt.Sprintf("typesafe: question %q: %s", id, err)}
		}
	}
	return nil
}

func validateInputEntry(value Entry, allowNull bool) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("must be JSON-compatible: %w", err)
	}
	encoded = bytes.TrimSpace(encoded)
	if allowNull && bytes.Equal(encoded, []byte("null")) {
		return nil
	}
	if len(encoded) == 0 || (encoded[0] != '"' && encoded[0] != '{' && encoded[0] != '[') {
		return fmt.Errorf("must be a JSON-compatible string, object, or array")
	}
	return nil
}
