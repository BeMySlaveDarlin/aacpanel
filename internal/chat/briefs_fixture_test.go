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

	if doc.ID == "" || doc.Title == "" || len(doc.Questions) < 2 {
		t.Fatalf("the fixture stopped being a brief: %+v", doc)
	}
	q := doc.Questions[0]
	switch {
	case len(q.Facts) == 0 || q.Facts[len(q.Facts)-1].Src == "":
		t.Error("the fixture carries no fact with a source, and that is half of what a brief is")
	case len(q.Options) < 2 || q.Options[0].Note == "":
		t.Error("the fixture carries no option with a cost")
	case len(q.Read) == 0:
		t.Error("the fixture carries no reading of the session")
	case q.Capture == nil || q.Capture.Note.Placeholder == "":
		t.Error("the fixture carries no free field")
	case q.Answered == nil || len(q.Answered.Picks) == 0:
		t.Error("the fixture carries no answer the session already knew")
	}
	if doc.Questions[1].Kind != "none" {
		t.Error("the fixture has no block that asks nothing, so nothing holds that path")
	}
}
