package typesafe_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/zhirschtritt/typesafe-go"
)

func TestQuestionsMarshalSystemOneWireShape(t *testing.T) {

	questions := map[string]typesafe.Question{
		"urgent": typesafe.Noul("Is this urgent?", &typesafe.NoulCriteria{
			True:  "Requires an immediate response",
			False: "Can wait",
		}),
		"team": typesafe.Choice("Who should own this?", map[string]typesafe.Entry{
			"billing":   "Payments and invoices",
			"technical": "Product defects",
		}),
		"severity": typesafe.Score("How severe is this?", "Low", "Medium", "High"),
	}

	got, err := json.Marshal(questions)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(got, &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	want := map[string]any{
		"urgent": map[string]any{
			"type":         "noul",
			"instructions": "Is this urgent?",
			"criteria": map[string]any{
				"true":  "Requires an immediate response",
				"false": "Can wait",
			},
		},
		"team": map[string]any{
			"type":         "choice",
			"instructions": "Who should own this?",
			"criteria": map[string]any{
				"billing":   "Payments and invoices",
				"technical": "Product defects",
			},
		},
		"severity": map[string]any{
			"type":         "score",
			"instructions": "How severe is this?",
			"criteria":     []any{"Low", "Medium", "High"},
		},
	}
	if !reflect.DeepEqual(decoded, want) {
		t.Errorf("System One question JSON = %#v, want %#v", decoded, want)
	}
}

func TestNoulWithoutCriteriaOmitsCriteria(t *testing.T) {

	got, err := json.Marshal(typesafe.Noul("Is this ready?", nil))
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if string(got) != `{"type":"noul","instructions":"Is this ready?"}` {
		t.Errorf("Marshal() = %s, want criteria omitted", got)
	}
}
