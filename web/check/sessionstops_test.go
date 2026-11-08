package check

import (
	"slices"
	"strings"
	"testing"
)

// pieItem is a sector of a pie as the fixtures read it.
type pieItem struct {
	Key, Label, Role, Why, Word string
	Slot                        int
	Off, On                     bool
}

func pieKeys(ring []pieItem) []string {
	keys := make([]string, 0, len(ring))
	for _, it := range ring {
		keys = append(keys, it.Key)
	}
	return keys
}

type stopsGate struct {
	Name, Kind, Title, Effect, OK string
	Harsher                       []string
	Before, Opened                int
	Pie                           bool
	Sent                          []struct{ Kind, Target string }
}

type stopsRun struct {
	Pies map[string]struct {
		Buttons    int
		Kept, Back bool
		Focus      string
		Menu       struct{ Role, Label string }
		Ring       []pieItem
	} `json:"pies"`
	Gates       []stopsGate                 `json:"gates"`
	Opened      []struct{ Name, ID string } `json:"opened"`
	More        int                         `json:"more"`
	Notes       map[string]string           `json:"notes"`
	Unavailable []pieItem                   `json:"unavailable"`
	Busy        bool                        `json:"busy"`
	BusyMarks   []struct {
		Kind        string `json:"kind"`
		Glyphs      int    `json:"glyphs"`
		Spins       int    `json:"spins"`
		SpinIsGlyph bool   `json:"spinIsGlyph"`
	} `json:"busyMarks"`
	Gestures struct {
		Card  struct{ Select string }
		Short struct {
			Pie    bool
			Opened int
		}
		Slid, Scrolled bool
		Held           struct {
			Pie, Scrolls, KeepsFinger, Layer bool
			Buzzed, Opened, Sent             int
			Plate                            string
		}
		OffTap struct {
			pieItem
			Pie   bool
			Sent  int
			Plate string
		}
		More struct {
			Ring []pieItem
			Hub  string
		}
		Back struct {
			Ring []string
			Hub  string
		}
		Hub          struct{ Pie, Focus bool }
		SlideRestart struct {
			Plate, Gate string
			Pie         bool
		}
		LiftClose struct {
			Pie, Gate bool
			Sent      int
			Plate     string
		}
		TapClose struct {
			Gate, OK string
			Pie      bool
		}
		Detach struct {
			Gate string
			Pie  bool
			Sent []struct{ Kind, Target string }
		}
		Stop struct {
			Gate bool
			Sent []struct{ Kind, Target string }
		}
		Closes map[string]struct{ Pie, Focus bool }
		Desk   struct {
			Pie     bool
			Buttons int
		}
	} `json:"gestures"`
}

// On the phone a live card offers its actions in its pie, the actions the
// host can take: the home session never closes and keeps the place of its
// close empty, a session the panel did not start has no pie, and the gate
// owns every confirmation. A press on the card still opens its chat.
func TestThePhoneCardsOfferRestartAndCloseThroughThePie(t *testing.T) {
	var got stopsRun
	runFixture(t, "sessionstops.html", &got)

	first := []string{"close", "restart", "pause", "pin", "more", "stop"}
	for name, want := range map[string][]string{
		"helios": first[1:], "lab": first, "quiet": first, "codex-5afc361b": first, "lab-kid": first, "outside": nil,
	} {
		p, ok := got.Pies[name]
		if !ok {
			t.Errorf("%s: the card is not on the list", name)
			continue
		}
		if p.Buttons != 0 {
			t.Errorf("%s: the phone card draws %d buttons of its own; its actions are in its pie", name, p.Buttons)
		}
		if want == nil {
			if p.Ring != nil || p.Kept {
				t.Errorf("%s: a session the panel did not start opened a pie %v or kept the browser's menu %v", name, p.Ring, p.Kept)
			}
			continue
		}
		if !p.Kept || !slices.Equal(pieKeys(p.Ring), want) {
			t.Errorf("%s: the contextmenu opened %v (the browser's menu kept off: %v), want %v", name, pieKeys(p.Ring), p.Kept, want)
		}
		shown := name
		if name == "codex-5afc361b" {
			shown = "Cart flakes"
		}
		if p.Menu.Role != "menu" || p.Menu.Label != "session "+shown {
			t.Errorf("%s: the pie is not a menu named for the session to a reader: %+v", name, p.Menu)
		}
		for _, it := range p.Ring {
			// Every item keeps its slot: the home session's close leaves its
			// own empty rather than moving the others round.
			if it.Slot != slices.Index(first, it.Key) {
				t.Errorf("%s: %s stands in slot %d, want %d", name, it.Key, it.Slot, slices.Index(first, it.Key))
			}
			wantRole := "menuitem"
			if it.Key == "pin" {
				wantRole = "menuitemcheckbox"
			}
			if it.Role != wantRole || it.Label == "" || it.Word == "" {
				t.Errorf("%s: the sector %s is not an item a reader can name: %+v", name, it.Key, it)
			}
			// The host knows no pin, these sessions stand in no project a
			// pause could be addressed by, and only the codex thread is in a
			// turn to stop.
			off := it.Key == "pause" || it.Key == "pin" || it.Key == "stop" && name != "codex-5afc361b"
			if it.Off != off || off && it.Why == "" {
				t.Errorf("%s: %s is off %v (%q), want off %v with its reason", name, it.Key, it.Off, it.Why, off)
			}
			if it.Key == "stop" && name == "lab" && (it.Why != "No turn is running" || !strings.Contains(it.Word, "no turn")) {
				t.Errorf("%s: the stop of a session in no turn says %q under %q, want no turn", name, it.Why, it.Word)
			}
		}
		if p.Focus != want[0] || !p.Back {
			t.Errorf("%s: the keyboard landed on %q, want %q; the focus came back to the card %v", name, p.Focus, want[0], p.Back)
		}
	}
	if got.More != 0 {
		t.Errorf("the list has %d menus", got.More)
	}
	if len(got.Opened) != len(got.Pies) {
		t.Errorf("the cards opened %+v", got.Opened)
	}
	for _, opened := range got.Opened {
		if _, ok := got.Pies[opened.Name]; !ok || opened.ID == "" {
			t.Errorf("a card opened %+v instead of its named conversation", opened)
		}
	}
	if len(got.Gates) != 4 {
		t.Fatalf("the presses reached %d confirmations, expected four", len(got.Gates))
	}
	for _, g := range got.Gates {
		if g.Before != 0 || g.Opened != 0 || g.Pie || len(g.Sent) != 1 ||
			g.Sent[0].Kind != "session."+g.Kind || g.Sent[0].Target != g.Name {
			t.Errorf("the confirmation did not own the action, or the pie stayed over it: %+v", g)
		}
		if !strings.Contains(g.Title, g.Name) {
			t.Errorf("the confirmation does not name %s: %q", g.Name, g.Title)
		}
		switch {
		case g.Kind == "restart":
			if g.OK != "Restart" || !strings.Contains(g.Effect, "empty context") || len(g.Harsher) != 0 {
				t.Errorf("restart reached the wrong confirmation: %+v", g)
			}
		case g.Name == "codex-5afc361b":
			if g.OK != "Close" || !strings.Contains(g.Effect, "daemon unloads it") || len(g.Harsher) != 0 {
				t.Errorf("codex close reached the wrong confirmation: %+v", g)
			}
		default:
			if g.OK != "Close gently" || !strings.Contains(g.Effect, "transcript") ||
				len(g.Harsher) != 1 || !strings.Contains(g.Harsher[0], "kill -9") {
				t.Errorf("claude close lost its gentle close or escalation: %+v", g)
			}
		}
	}
	if len(got.Unavailable) != 2 || !got.Busy {
		t.Errorf("unavailable actions %+v, busy disabled %v", got.Unavailable, got.Busy)
	}
	if len(got.BusyMarks) != 2 {
		t.Errorf("the lines of a restart and a close on their way are missing: %+v", got.BusyMarks)
	}
	for _, m := range got.BusyMarks {
		if m.Glyphs != 1 || m.Spins != 1 || !m.SpinIsGlyph {
			t.Errorf("the line of a %s on its way holds %d glyphs and %d spinners, the spinner the glyph %v: one mark is wanted",
				m.Kind, m.Glyphs, m.Spins, m.SpinIsGlyph)
		}
	}
	for _, b := range got.Unavailable {
		if !b.Off || !strings.Contains(b.Why, "the host does not know") {
			t.Errorf("an action unknown to the host remains available or unexplained: %+v", b)
		}
	}
	for name, want := range map[string]string{
		"helios": "stops 1 background task, 2 agents · a new session",
		"lab":    "stops 3 background tasks, 1 workflow · the conversation stays",
	} {
		if !strings.Contains(got.Notes[name], want) {
			t.Errorf("the end of %s reads %q, expected it to name %q", name, got.Notes[name], want)
		}
	}
	if strings.Contains(got.Notes["quiet"], "stops") || !strings.Contains(got.Notes["quiet"], "the conversation stays") {
		t.Errorf("the end of a session with nothing at work reads %q", got.Notes["quiet"])
	}
	saysCodexClose(t, "the pie of a phone card", got.Notes["codex-5afc361b"])
}

// A long press opens the pie and nothing else: no text is selected, the
// browser keeps its menu to itself, the phone buzzes once, and the click that
// ends the press neither opens the conversation nor takes the pie down. A
// short press opens the conversation; a press that slides or scrolls first
// opens nothing. Once held, the finger chooses rather than scrolls.
func TestALongPressOnAPhoneCardOpensItsPie(t *testing.T) {
	var got stopsRun
	runFixture(t, "sessionstops.html", &got)
	g := got.Gestures
	if g.Card.Select != "none" {
		t.Errorf("a long press on the card selects its text: user-select is %q", g.Card.Select)
	}
	if g.Short.Pie || g.Short.Opened != 1 {
		t.Errorf("a short press opened a pie %v and %d conversations, want the conversation alone", g.Short.Pie, g.Short.Opened)
	}
	if g.Slid || g.Scrolled {
		t.Errorf("a press that slid (%v) or scrolled (%v) before its time opened the pie", g.Slid, g.Scrolled)
	}
	h := g.Held
	if !h.Pie || h.Buzzed != 1 || h.Opened != 0 || h.Sent != 0 || !h.Layer {
		t.Errorf("a long press did not open the pie alone, as a layer, with one buzz: %+v", h)
	}
	if !h.Scrolls || !h.KeepsFinger {
		t.Errorf("a finger scrolls the list before the press holds (%v) and is kept by the pie after (%v): want both", h.Scrolls, h.KeepsFinger)
	}
	if h.Plate != "lab · tap an action, or slide to it and lift" {
		t.Errorf("a finger that lifted where it went down chose something: the plate reads %q", h.Plate)
	}
	for how, c := range g.Closes {
		if c.Pie || !c.Focus {
			t.Errorf("%s left the pie open (%v) or the focus away from the card (%v)", how, c.Pie, c.Focus)
		}
	}
	if len(g.Closes) != 3 {
		t.Errorf("the ways out of the pie tried: %v, want the scrim, the back of the phone and Escape", g.Closes)
	}
	if g.Desk.Pie || g.Desk.Buttons != 0 {
		t.Errorf("at a desk a long press opened a pie (%v) or the card carries %d buttons, where its actions are its pie on a right click", g.Desk.Pie, g.Desk.Buttons)
	}
}

// A sector does what the button of the same name does: Restart and Close go
// through their confirmations, an item of the second ring does what the line
// of the tools does. Lifting over Close does nothing — it asks for a tap — and
// a sector that cannot be done says why and does nothing. More opens the
// second ring, Back returns, and the hub of the first takes the pie down.
func TestThePieOfAPhoneCardDoesWhatItsItemsSay(t *testing.T) {
	var got stopsRun
	runFixture(t, "sessionstops.html", &got)
	g := got.Gestures
	if o := g.OffTap; !o.Pie || o.Sent != 0 || !o.Off || !strings.Contains(o.Plate, "No project of the map holds this session") {
		t.Errorf("a tap on a sector that cannot be done did something or did not say why: %+v", o)
	}
	second := []string{"rename", "role", "intent", "status", "move", "remote", "parent", "id", "resume"}
	if !slices.Equal(pieKeys(g.More.Ring), second) || g.More.Hub != "Back to the first ring" {
		t.Errorf("More opened %v with the hub %q, want %v and the way back", pieKeys(g.More.Ring), g.More.Hub, second)
	}
	for _, it := range g.More.Ring {
		if it.Slot != slices.Index(second, it.Key) || it.Label == "" {
			t.Errorf("the second ring holds %+v out of its place or without a name", it)
		}
	}
	if !slices.Equal(g.Back.Ring, []string{"close", "restart", "pause", "pin", "more", "stop"}) || g.Back.Hub != "Close the menu" {
		t.Errorf("Back returned to %v with the hub %q, want the first ring", g.Back.Ring, g.Back.Hub)
	}
	if g.Hub.Pie || !g.Hub.Focus {
		t.Errorf("the hub of the first ring left the pie %v, the focus on the card %v", g.Hub.Pie, g.Hub.Focus)
	}
	if s := g.SlideRestart; !strings.HasPrefix(s.Plate, "Restart the session") || s.Gate != "Restart lab?" || s.Pie {
		t.Errorf("sliding on to Restart and lifting did not reach its confirmation alone: %+v", s)
	}
	if l := g.LiftClose; !l.Pie || l.Gate || l.Sent != 0 || !strings.Contains(l.Plate, "lifting here does nothing") {
		t.Errorf("lifting over Close did something or did not say it needs a tap: %+v", l)
	}
	if c := g.TapClose; c.Gate != "Close session lab?" || c.OK != "Close gently" || c.Pie {
		t.Errorf("a tap on Close did not reach the confirmation of the card's close: %+v", c)
	}
	if d := g.Detach; d.Gate != "Detach lab-kid from lab?" || d.Pie || len(d.Sent) != 1 ||
		d.Sent[0].Kind != "session.detach" || d.Sent[0].Target != "lab-kid" {
		t.Errorf("Detach of the second ring did not do what the line of the tools does: %+v", d)
	}
	if s := g.Stop; s.Gate || len(s.Sent) != 1 || s.Sent[0].Kind != "session.stop" || s.Sent[0].Target != "codex-5afc361b" {
		t.Errorf("a tap on Stop of a thread in its turn did not stop it at once: %+v", s)
	}
}
