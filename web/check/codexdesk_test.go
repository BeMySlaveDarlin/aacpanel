package check

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// At a desk the band of a codex thread holds the paperclip, how it thinks and
// what it may do, with what the turn made at its right end; while the thread
// plans, the word keeps the model, since a desk has the room. Each word opens
// a menu over the composer — the plan, the models by number and the scale of
// efforts; the three modes by number and the sandbox in a line under them. A
// number or a press picks one setting and puts the menu down, and a press
// outside puts it down and sends nothing.
func TestTheBandOfACodexThreadOpensMenusAtADesk(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	var got struct {
		PlanWord string   `json:"planWord"`
		Procs    []string `json:"procs"`
		Thread   []string `json:"thread"`
		GoalPane struct {
			MenuDown bool `json:"menuDown"`
			Back     bool `json:"back"`
		} `json:"goalPane"`
		Band  []string `json:"band"`
		Think struct {
			Label  string   `json:"label"`
			Plan   bool     `json:"plan"`
			Models []string `json:"models"`
			Stops  []string `json:"stops"`
			Above  bool     `json:"above"`
		} `json:"think"`
		ByNumber []map[string]any `json:"byNumber"`
		Closed   bool             `json:"closed"`
		Perm     struct {
			Label string   `json:"label"`
			Rows  []string `json:"rows"`
			Note  string   `json:"note"`
		} `json:"perm"`
		ByPress  []map[string]any `json:"byPress"`
		PermWord string           `json:"permWord"`
		Away     struct {
			Closed bool `json:"closed"`
			Sent   int  `json:"sent"`
		} `json:"away"`
	}
	runWideFixture(t, "codexdesk.html", &got)

	if got.PlanWord != "Plan · gpt-6-astra · High" {
		t.Errorf("the word of a planning thread at a desk reads %q, expected the plan, the model and the effort", got.PlanWord)
	}
	if strings.Join(got.Band, ",") != "clip,think,perm,thread,cwork" {
		t.Errorf("the band at a desk holds %v, expected the paperclip, the three words and the work at its end", got.Band)
	}
	if strings.Join(got.Procs, "|") != "background processes: 3 running" {
		t.Errorf("the end of the band counts %v, expected the background processes alone", got.Procs)
	}
	if strings.Join(got.Thread, "|") != "1 Compact|2 Review|3 Rename|4 Goal" || !got.GoalPane.MenuDown || got.GoalPane.Back {
		t.Errorf("the menu of the thread is %v, and its goal opens as %+v — a sheet with nothing to go back to", got.Thread, got.GoalPane)
	}
	if got.Think.Label != "how codex thinks" || !got.Think.Plan || !got.Think.Above ||
		strings.Join(got.Think.Models, "|") != "1 gpt-6-astra|2 gpt-6-astra-mini" ||
		strings.Join(got.Think.Stops, ",") != "Low,Medium,High,Extra" {
		t.Errorf("the menu of how codex thinks is %+v", got.Think)
	}
	if !reflect.DeepEqual(got.ByNumber, []map[string]any{{"model": "gpt-6-astra-mini"}}) || !got.Closed {
		t.Errorf("the number 2 sent %v and put the menu down %v, expected the second model alone", got.ByNumber, got.Closed)
	}
	if got.Perm.Label != "what codex may do" ||
		strings.Join(got.Perm.Rows, "|") != "1 Read only|2 Ask|3 Approve for me" ||
		got.Perm.Note != "writes /srv/shop · beyond it codex asks you · full access only when a thread starts" {
		t.Errorf("the menu of what codex may do is %+v", got.Perm)
	}
	if !reflect.DeepEqual(got.ByPress, []map[string]any{{"mode": "read-only"}}) || got.PermWord != "Read only" {
		t.Errorf("the press on Read only sent %v and the word reads %q", got.ByPress, got.PermWord)
	}
	if !got.Away.Closed || got.Away.Sent != 0 {
		t.Errorf("a press outside the menu left it %v and sent %d requests", !got.Away.Closed, got.Away.Sent)
	}
}
