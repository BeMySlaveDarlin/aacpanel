package chat

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"testing"
)

// The fixture is a reply the collector actually wrote, not one made up here.
// What it holds is the whole shape of the schema: a field the collector starts
// sending and the types here do not know is a field that reaches the screen as
// nothing at all, and nothing about that is loud.
func TestBriefCarriesEveryFieldTheCollectorSends(t *testing.T) {
	raw, err := os.ReadFile("testdata/brief.json")
	if err != nil {
		t.Fatal(err)
	}

	var doc Brief
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the reply of the collector did not parse: %v", err)
	}
	back, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}

	var sent, kept any
	if err := json.Unmarshal(raw, &sent); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(back, &kept); err != nil {
		t.Fatal(err)
	}

	lost := missingPaths(sent, kept, "")
	if len(lost) > 0 {
		sort.Strings(lost)
		t.Errorf("these fields of the document never reach the screen: %v", lost)
	}
}

// missingPaths returns the paths present in sent and absent from kept.
func missingPaths(sent, kept any, at string) []string {
	switch got := sent.(type) {
	case map[string]any:
		mine, ok := kept.(map[string]any)
		if !ok {
			return []string{at}
		}
		var out []string
		for key, value := range got {
			here := at + "." + key
			found, there := mine[key]
			if !there {
				out = append(out, here)
				continue
			}
			out = append(out, missingPaths(value, found, here)...)
		}
		return out
	case []any:
		mine, ok := kept.([]any)
		if !ok || len(mine) != len(got) {
			return []string{at}
		}
		var out []string
		for i := range got {
			out = append(out, missingPaths(got[i], mine[i], at)...)
		}
		return out
	default:
		if !reflect.DeepEqual(sent, kept) {
			return []string{at}
		}
		return nil
	}
}

func TestBriefFixtureIsWorthTheName(t *testing.T) {
	raw, err := os.ReadFile("testdata/brief.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc Brief
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}

	if doc.ID == "" || doc.Title == "" || doc.Task == "" || doc.Why == "" || len(doc.Decisions) < 2 {
		t.Fatalf("the fixture stopped being a brief: %+v", doc)
	}
	if len(doc.Context) == 0 || doc.Context[0].Text == "" {
		t.Error("the fixture carries no context")
	}
	for _, d := range doc.Decisions {
		if len(d.Options) < 2 {
			t.Fatal("the fixture carries no choice")
		}
		for _, o := range d.Options {
			if o.Reason == "" || o.Cost == "" || o.Hours == 0 {
				t.Error("the fixture carries no reason or cost")
			}
		}
	}
	if !doc.Decisions[1].Multi {
		t.Error("the fixture carries no multiple choice")
	}
}
