package typesafe

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestQuestionValidationRejectsInvalidDefinitions(t *testing.T) {
	tests := []struct {
		name     string
		question Question
		want     string
	}{
		{"noul instructions", Noul(true, nil), "noul instructions"},
		{"noul true criterion", Noul("ready?", &NoulCriteria{True: true}), "noul true criterion"},
		{"noul false criterion", Noul("ready?", &NoulCriteria{False: true}), "noul false criterion"},
		{"choice instructions", Choice(true, map[string]Entry{"yes": nil}), "choice instructions"},
		{"choice without criteria", Choice("pick", nil), "between one and 255 criteria"},
		{"choice empty name", Choice("pick", map[string]Entry{"": nil}), "name must not be empty"},
		{"choice invalid criterion", Choice("pick", map[string]Entry{"yes": true}), `criterion "yes"`},
		{"score instructions", Score(true, "low", "high"), "score instructions"},
		{"score without enough levels", Score("rate", "only"), "between two and 10 criteria"},
		{"score invalid criterion", Score("rate", "low", true), "score criterion 1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := test.question.validate(); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validate() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestValidateQuestionsRejectsInvalidIdentity(t *testing.T) {
	var nilQuestion *NoulQuestion
	tests := []struct {
		name      string
		questions map[string]Question
		want      string
	}{
		{"empty id", map[string]Question{"": Noul("ready?", nil)}, "id must not be empty"},
		{"nil interface", map[string]Question{"ready": nil}, `question "ready" must not be nil`},
		{"typed nil", map[string]Question{"ready": nilQuestion}, `question "ready" must not be nil`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateQuestions(test.questions); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("validateQuestions() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestValidateInputEntryAcceptsJSONContainersAndNullCriteria(t *testing.T) {
	var nilMap map[string]any
	var nilSlice []any
	var nilPointer *string

	accepted := []Entry{"text", struct{ Value string }{"text"}, map[string]any{"value": 1}, []any{"value"}, [1]string{"value"}, nilMap, nilSlice, nilPointer, nil}
	for _, value := range accepted {
		if err := validateInputEntry(value, true); err != nil {
			t.Errorf("validateInputEntry(%#v, true) error = %v", value, err)
		}
	}

	for _, value := range []Entry{nilMap, nilSlice, nilPointer, nil, 1, true} {
		if err := validateInputEntry(value, false); err == nil {
			t.Errorf("validateInputEntry(%#v, false) error = nil", value)
		}
	}
}

func TestQuestionMarshalReportsUnsupportedEntry(t *testing.T) {
	questions := []Question{
		Noul(make(chan int), nil),
		Choice("pick", map[string]Entry{"bad": make(chan int)}),
		Score("rate", "low", make(chan int)),
	}
	for _, question := range questions {
		if _, err := json.Marshal(question); err == nil {
			t.Errorf("json.Marshal(%T) error = nil", question)
		}
	}
}
