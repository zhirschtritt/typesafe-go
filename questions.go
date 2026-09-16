package typesafe

import (
	"fmt"
	"reflect"
)

// Question describes a typed System One question.
type Question interface {
	validate() error
}

// NoulCriteria optionally clarifies what true and false mean for a Noul question.
type NoulCriteria struct {
	True  Entry `json:"true,omitempty"`
	False Entry `json:"false,omitempty"`
}

// NoulQuestion asks a yes-or-no question.
type NoulQuestion struct {
	Type         string        `json:"type"`
	Instructions Entry         `json:"instructions"`
	Criteria     *NoulCriteria `json:"criteria,omitempty"`
}

// Noul creates a Noul question. A nil criteria omits criteria from the request.
func Noul(instructions Entry, criteria *NoulCriteria) NoulQuestion {
	return NoulQuestion{
		Type:         "noul",
		Instructions: instructions,
		Criteria:     criteria,
	}
}

func (q NoulQuestion) validate() error {
	if q.Type != "noul" {
		return &TypeSafeError{Message: fmt.Sprintf("typesafe: noul question has type %q", q.Type)}
	}
	return nil
}

// ChoiceQuestion asks the model to select one named option.
type ChoiceQuestion struct {
	Type         string           `json:"type"`
	Instructions Entry            `json:"instructions"`
	Criteria     map[string]Entry `json:"criteria"`
}

// Choice creates a Choice question. criteria must contain at least one option.
func Choice(instructions Entry, criteria map[string]Entry) ChoiceQuestion {
	return ChoiceQuestion{
		Type:         "choice",
		Instructions: instructions,
		Criteria:     criteria,
	}
}

func (q ChoiceQuestion) validate() error {
	if q.Type != "choice" {
		return &TypeSafeError{Message: fmt.Sprintf("typesafe: choice question has type %q", q.Type)}
	}
	if len(q.Criteria) == 0 {
		return &TypeSafeError{Message: "typesafe: choice question requires at least one criterion"}
	}
	return nil
}

// ScoreQuestion asks the model to score state against ordered criteria.
type ScoreQuestion struct {
	Type         string  `json:"type"`
	Instructions Entry   `json:"instructions"`
	Criteria     []Entry `json:"criteria"`
}

// Score creates a Score question. criteria must contain at least two ordered levels.
func Score(instructions Entry, criteria ...Entry) ScoreQuestion {
	return ScoreQuestion{
		Type:         "score",
		Instructions: instructions,
		Criteria:     criteria,
	}
}

func (q ScoreQuestion) validate() error {
	if q.Type != "score" {
		return &TypeSafeError{Message: fmt.Sprintf("typesafe: score question has type %q", q.Type)}
	}
	if len(q.Criteria) < 2 {
		return &TypeSafeError{Message: "typesafe: score question requires at least two criteria"}
	}
	return nil
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
