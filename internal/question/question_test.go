package question

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestVerdictRequiresReadableFieldsAndBoundsTheForm(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"questions":null,"action":"","what":""}`, `{"questions":{},"action":"","what":""}`, `{"questions":[],"action":7,"what":""}`, `{"questions":[],"action":"run","what":""}`, `{"questions":[{"question":"Name?"}],"action":"","what":""}`, `{"questions":[],"action":"","what":""} trailing`, "```json\n{}\n```"} {
		if _, err := ParseVerdict([]byte(raw)); err == nil {
			t.Errorf("invalid verdict accepted: %s", raw)
		}
	}
	if v, err := ParseVerdict([]byte(`{"questions":[],"action":"","what":"unused"}`)); err != nil || v.What != "" || v.Questions == nil {
		t.Fatalf("negative verdict: %+v %v", v, err)
	}
	options := []Option{{Label: " "}, {Label: "  Publish\n now ", Description: "  ready\n today "}, {Label: "Publish now"}}
	for range 6 {
		options = append(options, Option{Label: strings.Repeat("é", 41) + string(rune('a'+len(options))), Description: strings.Repeat("é", 121)})
	}
	questions := []Question{{Question: " "}}
	for range 6 {
		questions = append(questions, Question{Question: "  " + strings.Repeat("é", 201), Header: strings.Repeat("é", 13), Options: options, Multi: true})
	}
	raw, err := json.Marshal(Verdict{Questions: questions, Action: "  " + strings.Repeat("é", 141), What: "\n" + strings.Repeat("é", 141)})
	if err != nil {
		t.Fatal(err)
	}
	v, err := ParseVerdict(raw)
	if err != nil || len(v.Questions) != 4 || len([]rune(v.What)) != 140 || len([]rune(v.Action)) != 140 {
		t.Fatalf("verdict limits: %+v %v", v, err)
	}
	q := v.Questions[0]
	// Long duplicate labels collapse after trimming to what the person sees.
	if len([]rune(q.Question)) != 200 || len([]rune(q.Header)) != 12 || !q.Multi || len(q.Options) != 2 || q.Options[0].Label != "Publish now" || q.Options[0].Description != "ready today" || len([]rune(q.Options[1].Label)) != 40 || len([]rune(q.Options[1].Description)) != 120 {
		t.Fatalf("form limits: %+v", q)
	}
	four := Clean([]Question{{Question: "Which?", Options: []Option{{Label: "A"}, {Label: "B"}, {Label: "C"}, {Label: "D"}, {Label: "E"}}}})
	if len(four[0].Options) != 4 {
		t.Fatalf("option limit: %+v", four)
	}
}

func TestCandidateIncludesRequestsAndExcludesCodeAndLinkQuestions(t *testing.T) {
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"Done.\n\n```sh\nprintf ready\n```", true},
		{"The report is ready.\n\n\u0416\u0434\u0451\u0442 \u0442\u0432\u043e\u0435\u0433\u043e \u0432\u043b\u0438\u0432\u0430\u043d\u0438\u044f.", true},
		{"Checks passed. \u041d\u0435 \u0437\u0430\u0431\u0443\u0434\u044c \u043f\u0435\u0440\u0435\u0434\u0430\u0442\u044c \u0440\u0435\u0437\u0443\u043b\u044c\u0442\u0430\u0442.", true},
		{"The build passed. The artifact is saved. No changes remain.", false},
		{"Which option?", true},
		{"See `value ? first : second` and [manual](https://example.test/?q=x).", false},
		{"https://example.test/?x=1", false},
		{"Why?https://example.test/", true},
		{"[manual]: https://example.test/?x=1", false},
		{"    sample?\n\tother?", false},
		{"[Which?](https://example.test/)", true},
		{"  ```sh\nvalue?\n  ```" + strings.Repeat("\ncomplete", 160), false},
		{"``literal `?` value``", false},
		{"Please review the screen", true},
		{"\u041c\u044b \u0440\u0435\u0448\u0438\u043b\u0438 \u0437\u0430\u0434\u0430\u0447\u0443.", false},
		{"your " + strings.Repeat("x", 801), false},
		{strings.Repeat("é", 900) + " \u043f\u0435\u0440\u0435\u0434\u0430\u0439 \u0440\u0435\u0437\u0443\u043b\u044c\u0442\u0430\u0442", true},
	} {
		if got := Candidate(tc.text); got != tc.want {
			t.Errorf("candidate %q: %v, want %v", tc.text, got, tc.want)
		}
	}
}
