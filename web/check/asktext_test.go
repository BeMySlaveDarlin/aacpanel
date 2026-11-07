package check

import (
	"reflect"
	"strings"
	"testing"
)

type questionProseSeen struct {
	Paragraphs int      `json:"paragraphs"`
	Breaks     int      `json:"breaks"`
	Bold       []string `json:"bold"`
	Code       []string `json:"code"`
	Items      []string `json:"items"`
}

func checkQuestionProse(t *testing.T, got questionProseSeen) {
	t.Helper()
	if got.Paragraphs < 3 || got.Breaks != 1 || !reflect.DeepEqual(got.Bold, []string{"Change plan"}) ||
		!reflect.DeepEqual(got.Code, []string{"status_id 0"}) ||
		!reflect.DeepEqual(got.Items, []string{"keep active deals.", "unset empty status_id.", "merge arrays."}) {
		t.Errorf("the question lost its markdown or inline list: %+v", got)
	}
}

func TestQuestionListsSplitOnlyConsecutiveItemsAfterPunctuation(t *testing.T) {
	plan := "Fix plan (review: round 1 BLOCK, round 2 PASS). Changes: 1) UpdateLeadHandler checks null === $lead; 0 new queries. 2) AmoTrigger MK unsets empty status_id. 3) AmoLeadUpdateSubscriber uses 4x array_merge. Tests: helper (4 methods + transfer with a hidden student). Defaults: status_id 0 stays queued. Confirm?"
	cases := []struct{ name, in, want string }{
		{"a plan with test counts", plan, strings.NewReplacer("Changes: 1)", "Changes:\n1)", "queries. 2)", "queries.\n2)", "status_id. 3)", "status_id.\n3)").Replace(plan)},
		{"semicolons", "Changes: 1) first; 2) second; 3) third", "Changes:\n1) first;\n2) second;\n3) third"},
		{"two lists", "One: 1) a. 2) b. Two: 1) c; 2) d", "One:\n1) a.\n2) b. Two:\n1) c;\n2) d"},
		{"a short question", "Which file first?", "Which file first?"},
		{"counts", "Tests (4 methods + transfer), status_id 0, version 2.1, 777 stays", "Tests (4 methods + transfer), status_id 0, version 2.1, 777 stays"},
		{"one item", "Changes: 1) first", "Changes: 1) first"},
		{"missing second item", "Changes: 1) first. 3) third", "Changes: 1) first. 3) third"},
		{"no punctuation", "Changes 1) first 2) second", "Changes 1) first 2) second"},
		{"second has no punctuation", "Changes: 1) first 2) second", "Changes: 1) first 2) second"},
		{"different paragraphs", "Changes: 1) first.\n\nNotes: 2) second", "Changes: 1) first.\n\nNotes: 2) second"},
		{"an existing list", "Changes:\n1) first\n2) second", "Changes:\n1) first\n2) second"},
		{"inline code", "Example `Steps: 1) first. 2) second` stays", "Example `Steps: 1) first. 2) second` stays"},
		{"a code fence", "```text\nSteps: 1) first. 2) second\n```", "```text\nSteps: 1) first. 2) second\n```"},
		{"code inside a list", "Changes: 1) keep `status_id 0`. 2) keep `(4 methods)`", "Changes:\n1) keep `status_id 0`.\n2) keep `(4 methods)`"},
	}
	calls := make([][]any, len(cases))
	for i, c := range cases {
		calls[i] = []any{c.in}
	}
	got := runModuleJS(t, "src/screens/asktext.js", "splitQuestionLists", calls)
	for i, c := range cases {
		if got[i] != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got[i], c.want)
		}
	}
}

// Question nodes are read before mounting: these checks need no browser or
// extra fixture page, and a raw text interpolation loses the same elements.
func TestQuestionTextRendersMarkdownAndPreservesShortQuestions(t *testing.T) {
	got := runModuleJS(t, "src/screens/asktext.js", "AskText", [][]any{
		{map[string]any{"text": "Question **scope**:\nChoose `status_id`.\n\nChanges: 1) keep active deals. 2) unset empty status_id. 3) merge arrays.\n\nConfirm?"}},
		{map[string]any{"text": "Which file first?"}},
	})
	tags := map[string][]string{}
	questionNodes(got[0], tags)
	if !reflect.DeepEqual(tags["b"], []string{"scope"}) || !reflect.DeepEqual(tags["code"], []string{"status_id"}) ||
		len(tags["p"]) != 3 || len(tags["br"]) != 1 || len(tags["ol"]) != 1 ||
		!reflect.DeepEqual(tags["li"], []string{"keep active deals.", "unset empty status_id.", "merge arrays."}) {
		t.Errorf("question prose loses its structure: %+v", tags)
	}
	short := map[string][]string{}
	if text := questionNodes(got[1], short); text != "Which file first?" || !reflect.DeepEqual(short["p"], []string{text}) {
		t.Errorf("a short question loses its words: %q, %+v", text, short)
	}
}

func questionNodes(node any, tags map[string][]string) string {
	switch n := node.(type) {
	case string:
		return n
	case []any:
		var text strings.Builder
		for _, child := range n {
			text.WriteString(questionNodes(child, tags))
		}
		return text.String()
	case map[string]any:
		props, _ := n["props"].(map[string]any)
		text := questionNodes(props["children"], tags)
		if tag, ok := n["type"].(string); ok {
			tags[tag] = append(tags[tag], text)
		}
		return text
	}
	return ""
}
