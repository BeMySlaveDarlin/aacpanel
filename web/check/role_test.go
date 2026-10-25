package check

import (
	"strings"
	"testing"
)

type roleLine struct {
	Checklist string  `json:"checklist"`
	Name      string  `json:"name"`
	Role      *string `json:"role"`
	Cut       bool    `json:"cut"`
	OneLine   bool    `json:"oneLine"`
	Placed    bool    `json:"placed"`
}

// The cards show the checklist, cut to one line under the state; the role
// stays in the tools. Its sheet still counts, saves and clears the words.
func TestTheChecklistTakesTheRowAndTheRoleStaysInTheTools(t *testing.T) {
	var got struct {
		Desk         []roleLine `json:"desk"`
		Phone        []roleLine `json:"phone"`
		Labels       []string   `json:"labels"`
		Note         string     `json:"note"`
		Clamped      string     `json:"clamped"`
		Title        string     `json:"title"`
		Value        string     `json:"value"`
		Count        string     `json:"count"`
		Buttons      []string   `json:"buttons"`
		SameOff      bool       `json:"sameOff"`
		LongOff      bool       `json:"longOff"`
		LongSays     string     `json:"longSays"`
		LongWarn     bool       `json:"longWarn"`
		AstralOff    bool       `json:"astralOff"`
		GoodOff      bool       `json:"goodOff"`
		NoneNote     string     `json:"noneNote"`
		NoneValue    string     `json:"noneValue"`
		NoneButtons  []string   `json:"noneButtons"`
		Sent         []string   `json:"sent"`
		Confirmation bool       `json:"confirmation"`
	}
	runWideFixture(t, "role.html", &got)

	if len(got.Desk) != 3 || len(got.Phone) != 3 {
		t.Fatalf("the desktop list has %d rows and the phone's %d: %+v %+v", len(got.Desk), len(got.Phone), got.Desk, got.Phone)
	}
	for _, r := range got.Desk {
		if r.Role != nil {
			t.Errorf("desktop: %s shows the role %v", r.Name, r.Role)
		}
		want := map[string]string{
			"shop":             "1/1 · Review the branch and the changes to all the migrations before handing the work to the next session",
			"shop-db-split":    "2/2 · Run migrations",
			"shop-queue-watch": "",
		}[r.Name]
		if r.Checklist != want {
			t.Errorf("desktop: %s shows checklist %q, want %q", r.Name, r.Checklist, want)
		}
		if !r.OneLine || !r.Placed {
			t.Errorf("desktop: the checklist of %s is on one line %v, under the state %v", r.Name, r.OneLine, r.Placed)
		}
		if r.Cut != (r.Name == "shop") {
			t.Errorf("desktop: the checklist of %s is cut %v — only the long one is", r.Name, r.Cut)
		}
	}
	for _, r := range got.Phone {
		if r.Role != nil {
			t.Errorf("phone: the card of %s carries the role %q", r.Name, *r.Role)
		}
	}

	at := -1
	for i, label := range got.Labels {
		if label == "Role…" {
			at = i
		}
	}
	if at < 1 || got.Labels[at-1] != "Rename…" {
		t.Errorf("the tools list %v: the role stands right after Rename", got.Labels)
	}
	if got.Note != "The database split and its migrations" || got.Clamped != "2" {
		t.Errorf("the line of the role says %q, clamped to %q lines", got.Note, got.Clamped)
	}
	if got.Title != "Role of the session" || got.Value != "The database split and its migrations" ||
		got.Count != "37 / 500" || !got.SameOff {
		t.Errorf("the sheet opened as %q with %q, counting %q (save off on the same role: %v)", got.Title, got.Value,
			got.Count, got.SameOff)
	}
	if strings.Join(got.Buttons, ",") != "Clear,Cancel,Save" {
		t.Errorf("the sheet of a role offers %v", got.Buttons)
	}
	if !got.LongOff || !got.LongWarn || !strings.Contains(got.LongSays, "501 of 500") {
		t.Errorf("a role of 501 characters can be saved (%v): %q (warned %v)", !got.LongOff, got.LongSays, got.LongWarn)
	}
	if got.AstralOff || got.GoodOff {
		t.Errorf("a role of 500 letters outside the basic plane is held back (%v), a sound one (%v)", got.AstralOff, got.GoodOff)
	}
	want := []string{
		`session.role:shop-db-split:{"role":"The migrations of the history tables"}`,
		`session.role:shop-db-split:{"role":""}`,
	}
	if strings.Join(got.Sent, "\n") != strings.Join(want, "\n") || got.Confirmation {
		t.Errorf("the sheet sent %v (asked again: %v), meant %v", got.Sent, got.Confirmation, want)
	}
	if !strings.HasPrefix(got.NoneNote, "none:") || got.NoneValue != "" || strings.Join(got.NoneButtons, ",") != "Cancel,Save" {
		t.Errorf("a session without a role: the line says %q, the sheet holds %q and offers %v", got.NoneNote,
			got.NoneValue, got.NoneButtons)
	}
}
