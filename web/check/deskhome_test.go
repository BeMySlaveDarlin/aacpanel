package check

import (
	"regexp"
	"strings"
	"testing"
)

type deskStop struct {
	Here     string `json:"here"`
	Page     string `json:"page"`
	Chat     bool   `json:"chat"`
	Head     string `json:"head"`
	Asked    string `json:"asked"`
	Composer bool   `json:"composer"`
}

type deskPlace struct {
	First        deskStop       `json:"first"`
	BriefsButton bool           `json:"briefsButton"`
	Section      deskStop       `json:"reloadSection"`
	AlertsBack   deskStop       `json:"alertsBack"`
	DevicesBack  deskStop       `json:"devicesBack"`
	PageOverPage deskStop       `json:"pageOverPageBack"`
	Page         deskStop       `json:"reloadPage"`
	PageBack     deskStop       `json:"reloadPageBack"`
	Chat         deskStop       `json:"reloadChat"`
	ChatAlerts   deskStop       `json:"chatAlertsBack"`
	KeptArchived map[string]any `json:"keptArchived"`
	Archived     deskStop       `json:"reloadArchived"`
	Unknown      deskStop       `json:"unknownSection"`
	Garbage      deskStop       `json:"garbage"`
	Error        string         `json:"error"`
}

func runDeskPlace(t *testing.T) deskPlace {
	t.Helper()
	var got deskPlace
	runWideFixture(t, "deskplace.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke: %s", got.Error)
	}
	return got
}

// onlyAsked says every request of the feed went out for one conversation.
func onlyAsked(asked, id string) bool {
	if asked == "" {
		return false
	}
	for _, one := range strings.Split(asked, ",") {
		if one != id {
			return false
		}
	}
	return true
}

// The first visit opens home: with nothing seen yet, the overview is where a
// person starts. From then on a reload comes back to where the person was — the
// section and the conversation open in it — and not to home: a reload is not a
// request to start over, and landing on home every time is a walk back to the
// work on each one. What cannot be read back as a place opens home.
func TestDesktopStartsOnHomeAndReloadsWhereItWas(t *testing.T) {
	got := runDeskPlace(t)

	if got.First.Here != "home" || got.First.Head != "home" {
		t.Errorf("the first visit opened %q with %q in the centre, not home", got.First.Here, got.First.Head)
	}
	if got.Section.Here != "containers" {
		t.Errorf("a reload in Containers came back to %q — the section is forgotten with the page", got.Section.Here)
	}
	if got.Chat.Here != "sessions" || !got.Chat.Chat {
		t.Errorf("a reload with a conversation open came back to %q, conversation drawn: %v — the conversation is forgotten with the page",
			got.Chat.Here, got.Chat.Chat)
	}
	if !onlyAsked(got.Chat.Asked, "live-1") {
		t.Errorf("after the reload the feed was asked as %q, not for the conversation that was open", got.Chat.Asked)
	}
	if !got.Chat.Composer {
		t.Error("a live conversation came back from the reload without its composer — it is not found as its session any more")
	}

	if got.KeptArchived == nil {
		t.Fatal("a conversation opened from the archive was not kept at all")
	}
	if _, ok := got.KeptArchived["row"]; ok {
		t.Error("the archive row is kept along with the conversation — a stale copy of the list outlives the page")
	}
	if got.KeptArchived["archived"] != true || got.KeptArchived["id"] != "past-1" || got.KeptArchived["name"] != "gateway" {
		t.Errorf("a closed conversation is kept as %v, not by its name, id and being closed", got.KeptArchived)
	}
	if got.Archived.Here != "sessions" || !got.Archived.Chat || !onlyAsked(got.Archived.Asked, "past-1") {
		t.Errorf("a closed conversation came back from the reload as %+v — it should open by its id", got.Archived)
	}

	for name, stop := range map[string]deskStop{"a section the shell does not have": got.Unknown, "a kept value that does not parse": got.Garbage} {
		if stop.Here != "home" {
			t.Errorf("%s opened %q instead of home — the shell stands on nothing it can draw", name, stop.Here)
		}
	}
}

// A page of the header — the alerts, the devices — stands over the section it
// was opened from, and its way back leads there, not to home: the bell pressed
// in the middle of a conversation returns to the conversation. Two pages opened
// one from the other replace each other, and back leads past both. A reload on
// a page keeps its way back.
func TestDesktopPagesCloseOntoWhereTheyWereOpened(t *testing.T) {
	got := runDeskPlace(t)

	for _, c := range []struct {
		what string
		stop deskStop
		want string
	}{
		{"the alerts opened from Containers", got.AlertsBack, "containers"},
		{"the devices opened from Containers", got.DevicesBack, "containers"},
		{"the alerts opened from the devices over Containers", got.PageOverPage, "containers"},
		{"the alerts reloaded over Containers", got.PageBack, "containers"},
		{"the alerts opened from a conversation", got.ChatAlerts, "sessions"},
	} {
		if c.stop.Here != c.want {
			t.Errorf("back from %s went to %q, not %q", c.what, c.stop.Here, c.want)
		}
		if c.stop.Page != "" {
			t.Errorf("back from %s left the page %q standing", c.what, c.stop.Page)
		}
	}
	if !got.ChatAlerts.Chat {
		t.Error("back from the alerts opened in a conversation lands on the sessions without the conversation")
	}
	if got.Page.Here != "alerts" || got.Page.Page != "Alerts" {
		t.Errorf("a reload on the alerts came back to %q with page %q", got.Page.Here, got.Page.Page)
	}
}

// Briefs on the desktop are the conversation's own: each opens from the
// conversation that wrote it. The header holds no shelf of them.
func TestDesktopHeaderHasNoShelfOfBriefs(t *testing.T) {
	if runDeskPlace(t).BriefsButton {
		t.Error("the desktop header has a Briefs button — the shelf of every brief is back beside the sections")
	}
}

// The phone keeps a start of its own: its tabs have no home.
func TestThePhoneDoesNotStartOnTheDesktopHome(t *testing.T) {
	mobile := stripComments(srcFiles(t)["src/mobile/shell.js"])
	if mobile == "" {
		t.Fatal("src/mobile/shell.js not found — the boundary with the phone is left unchecked")
	}
	tab := regexp.MustCompile(`\[\s*tab\s*,\s*setTab\s*\]\s*=\s*useState\(([^)]*)\)`).FindStringSubmatch(mobile)
	if tab == nil {
		t.Fatal("the mobile shell has no tab state — the test looks in the wrong place")
	}
	if strings.Contains(tab[1], `"home"`) {
		t.Error("the phone now starts on the desktop home — the mobile layout has a starting screen of its own")
	}
}
