package check

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// The answer of /usage is a card like the one of /context: the limits that
// matter now in the feed, and the report of the client's own card in the sheet
// it opens — each limit with its reset and a bar coloured by how loud the plan
// says it is, what the session cost, its tokens by model, and which habits
// spent the limits over a day and a week.
func TestTheAnswerOfUsageIsACardAndASheet(t *testing.T) {
	var got struct {
		Before struct {
			Tag    string   `json:"tag"`
			Title  string   `json:"title"`
			Figure string   `json:"figure"`
			Bar    []string `json:"bar"`
		} `json:"before"`
		Inside struct {
			Head     string   `json:"head"`
			Limits   []string `json:"limits"`
			Resets   []string `json:"resets"`
			Percents []string `json:"percents"`
			Tones    []string `json:"tones"`
			Facts    []string `json:"facts"`
			Model    string   `json:"model"`
			Rows     []string `json:"rows"`
			Tabs     []string `json:"tabs"`
			Day      []string `json:"day"`
			Week     []string `json:"week"`
			WeekSum  string   `json:"weekSum"`
		} `json:"inside"`
	}
	runFixture(t, "usagecard.html", &got)

	b := got.Before
	if b.Tag != "BUTTON" || b.Title != "Usage" || b.Figure != "5-hour 4% · week 76%" {
		t.Errorf("the card is a %s %q reading %q", b.Tag, b.Title, b.Figure)
	}
	if !reflect.DeepEqual(b.Bar, []string{"t-warning"}) {
		t.Errorf("the bar of the card is %v, expected the fullest limit, loud as the plan says", b.Bar)
	}

	in := got.Inside
	if in.Head != "Usage" {
		t.Errorf("the sheet is headed %q", in.Head)
	}
	if !reflect.DeepEqual(in.Limits, []string{"5-hour limit", "Weekly · all models", "Weekly · Fable"}) ||
		!reflect.DeepEqual(in.Percents, []string{"4%", "76%", "12%"}) {
		t.Errorf("the limits read %v at %v", in.Limits, in.Percents)
	}
	if len(in.Resets) != 3 || in.Resets[0] != "Resets in 2 h 30 min" ||
		!regexp.MustCompile(`^Resets \d\d\.\d\d, \d\d:\d\d$`).MatchString(in.Resets[1]) {
		t.Errorf("the resets read %q: soon in hours, later as a date and a time", in.Resets)
	}
	if !reflect.DeepEqual(in.Tones, []string{"t-normal", "t-warning", "t-normal"}) {
		t.Errorf("the bars of the limits are toned %v", in.Tones)
	}
	for _, say := range []string{"Cost $0.07", "Cache hit 91%", "API 15 s", "Lines +3 −1"} {
		if !contains(say, in.Facts) {
			t.Errorf("the session part does not say %q: %v", say, in.Facts)
		}
	}
	if in.Model != "Haiku 4.5" {
		t.Errorf("the breakdown is of %q, expected the model by its name", in.Model)
	}
	for _, say := range []string{"Input62", "Output1.1k", "Thinking508", "Cache read209k", "Cache write20k", "Cost$0.07"} {
		if !contains(say, in.Rows) {
			t.Errorf("the breakdown does not carry %q: %v", say, in.Rows)
		}
	}
	if !reflect.DeepEqual(in.Tabs, []string{"24h", "7d"}) || len(in.Day) != 2 ||
		!reflect.DeepEqual(in.Week, []string{"87% of your usage was at >150k context"}) ||
		!strings.HasPrefix(in.WeekSum, "Last 7d · 4424 requests") {
		t.Errorf("the habits: tabs %v, a day %v, a week %v (%q)", in.Tabs, in.Day, in.Week, in.WeekSum)
	}
}
