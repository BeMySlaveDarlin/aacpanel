package check

import (
	"net/http"
	"strings"
	"testing"
)

// The settings page of a contour: the same rows a project has; a value most
// of its projects store offered as the default, going into the draft and
// saying how many projects follow it; a group dragged to another place; Save
// sending the launch and the order without a sheet; the copies left behind
// offered for removal through a sheet naming them; the journal laid over the
// page with its draft kept underneath, a deletion taken back from it.
func TestTheContourSettingsPage(t *testing.T) {
	var got struct {
		Rows           []string       `json:"rows"`
		Groups         []string       `json:"groups"`
		Raise          []string       `json:"raise"`
		Files          string         `json:"files"`
		PathInputs     []string       `json:"pathInputs"`
		DeleteDisabled bool           `json:"deleteDisabled"`
		AfterRaise     string         `json:"afterRaise"`
		RcNote         string         `json:"rcNote"`
		RaiseLeft      []string       `json:"raiseLeft"`
		AfterDrag      string         `json:"afterDrag"`
		OrderNow       []string       `json:"orderNow"`
		OrderSays      string         `json:"orderSays"`
		Saved          []string       `json:"saved"`
		SheetOnSave    bool           `json:"sheetOnSave"`
		Copies         []string       `json:"copies"`
		UnpinSheet     string         `json:"unpinSheet"`
		Unpin          map[string]any `json:"unpin"`
		PageHidden     bool           `json:"pageHidden"`
		Entries        []string       `json:"entries"`
		Undo           bool           `json:"undo"`
		BackToPage     bool           `json:"backToPage"`
		Closed         int            `json:"closed"`
	}
	runFixtureServing(t, "contoursettings.html", phoneScreen, phonePointer, schemaAnswer(map[string]any{}), &got)

	if len(got.Rows) != 11 || got.Rows[0] != "Model" {
		t.Errorf("the contour's rows are %v, meant the eleven a project has", got.Rows)
	}
	if strings.Join(got.Groups, " ") != "home side" {
		t.Errorf("the groups read %v", got.Groups)
	}
	if len(got.Raise) != 1 || got.Raise[0] != "Remote Control is On in 3 of 4 projects — make it this contour's default?" {
		t.Errorf("the raise hint reads %v", got.Raise)
	}
	if !strings.Contains(got.Files, "every directory no other contour takes") || len(got.PathInputs) != 0 {
		t.Errorf("a routed account shows %q and fields %v — its paths are the host's, not typed here", got.Files, got.PathInputs)
	}
	if !got.DeleteDisabled {
		t.Error("a contour with groups — the personal one at that — offers its deletion")
	}
	if got.AfterRaise != "1 change" || got.RcNote != "1 of 4 projects follow it · 3 set their own and stay" || len(got.RaiseLeft) != 0 {
		t.Errorf("after the raise: %q, note %q, the hint still %v", got.AfterRaise, got.RcNote, got.RaiseLeft)
	}
	if got.AfterDrag != "2 changes" || strings.Join(got.OrderNow, " ") != "side home" || got.OrderSays != "order not saved" {
		t.Errorf("after the drag: %q, order %v, said %q", got.AfterDrag, got.OrderNow, got.OrderSays)
	}
	want := []string{
		`PATCH /api/profiles/3 {"launchSet":{"remoteControl":true}}`,
		`PUT /api/profiles/3/groups/order {"ids":[5,3]}`,
	}
	if strings.Join(got.Saved, "|") != strings.Join(want, "|") || got.SheetOnSave {
		t.Errorf("Save sent %v (a sheet: %v), meant %v without one", got.Saved, got.SheetOnSave, want)
	}
	if len(got.Copies) != 1 || got.Copies[0] != "3 projects still store Remote Control On themselves — remove the copies?" {
		t.Errorf("the copies hint reads %v", got.Copies)
	}
	if !strings.Contains(got.UnpinSheet, "3 projects store Remote Control On themselves — gamma, alpha, beta") || got.Unpin["key"] != "remoteControl" {
		t.Errorf("the unpin sheet reads %q and sent %v", got.UnpinSheet, got.Unpin)
	}
	if !got.PageHidden || len(got.Entries) != 2 || !strings.Contains(got.Entries[1], "launch.remoteControl — → true") || !got.Undo {
		t.Errorf("the journal: page hidden %v, entries %v, undo %v", got.PageHidden, got.Entries, got.Undo)
	}
	if !got.BackToPage || got.Closed != 0 {
		t.Errorf("back from the journal: on the page %v, closed %d", got.BackToPage, got.Closed)
	}
}

// On a machine without the router a contour's paths are typed on its page:
// the record's path to claude is shown, a Save of another field does not send
// it, and clearing it goes through the sheet as a change of the account.
func TestTheContourPathsGoOnlyWhenChanged(t *testing.T) {
	var got struct {
		Bin           string         `json:"bin"`
		Renamed       map[string]any `json:"renamed"`
		SheetOnRename bool           `json:"sheetOnRename"`
		ClearSheet    bool           `json:"clearSheet"`
		Cleared       map[string]any `json:"cleared"`
	}
	serve := schemaAnswer(map[string]any{})
	serve["/mode"] = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("paths")) })
	runFixtureServing(t, "contoursettings.html", phoneScreen, phonePointer, serve, &got)
	if got.Bin != "/srv/bin/claude" {
		t.Errorf("the page shows the path to claude as %q, meant the record's", got.Bin)
	}
	if len(got.Renamed) != 1 || got.Renamed["name"] != "home" || got.SheetOnRename {
		t.Errorf("a rename sent %v (sheet %v) — the path goes only when it is changed", got.Renamed, got.SheetOnRename)
	}
	if !got.ClearSheet || len(got.Cleared) != 1 || got.Cleared["claudeBin"] != "" {
		t.Errorf("clearing the path: sheet %v, sent %v — meant a sheet and claudeBin \"\"", got.ClearSheet, got.Cleared)
	}
}

// A new contour is taken from an account of the host the map has none for;
// by hand only on a machine without the router.
func TestANewContourComesFromTheHostsAccounts(t *testing.T) {
	type answer struct {
		Accounts []string       `json:"accounts"`
		ByHand   bool           `json:"byHand"`
		Sheet    string         `json:"sheet"`
		Sent     map[string]any `json:"sent"`
	}
	for _, c := range []struct {
		mode   string
		prefix string
		byHand bool
	}{{"new", "/srv/proj/Work/", false}, {"bare", "", true}} {
		t.Run(c.mode, func(t *testing.T) {
			var got answer
			serve := schemaAnswer(map[string]any{})
			serve["/mode"] = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(c.mode)) })
			runFixtureServing(t, "contoursettings.html", phoneScreen, phonePointer, serve, &got)
			if len(got.Accounts) != 1 || !strings.Contains(got.Accounts[0], "/home/u/.claude-profiles/work") {
				t.Errorf("the accounts offered read %v", got.Accounts)
			}
			if got.ByHand != c.byHand {
				t.Errorf("by hand offered %v, meant %v", got.ByHand, c.byHand)
			}
			body, _ := got.Sent["body"].(map[string]any)
			if got.Sent["url"] != "/api/profiles" || body["name"] != "work" || body["configDir"] != "/home/u/.claude-profiles/work" || body["prefix"] != c.prefix {
				t.Errorf("taking the account sent %v", got.Sent)
			}
		})
	}
}
