package check

import (
	"slices"
	"strings"
	"testing"
)

type prefsRow struct {
	Title    string `json:"title"`
	Sub      string `json:"sub"`
	Role     string `json:"role"`
	On       string `json:"on"`
	Disabled bool   `json:"disabled"`
	Lock     string `json:"lock"`
	Dim      string `json:"dim"`
}

type prefsShot struct {
	Alerts struct {
		Door     string   `json:"door"`
		Heads    []string `json:"heads"`
		OldBlock bool     `json:"oldBlock"`
		Buttons  []string `json:"buttons"`
	} `json:"alerts"`
	Head   string   `json:"head"`
	Heads  []string `json:"heads"`
	Device struct {
		Row     *prefsRow `json:"row"`
		Buttons []string  `json:"buttons"`
	} `json:"device"`
	Rows  []prefsRow          `json:"rows"`
	Chips map[string][]string `json:"chips"`
	Idle  []string            `json:"idle"`
	Gets  int                 `json:"gets"`

	Layout []struct {
		Left  int `json:"left"`
		Top   int `json:"top"`
		Width int `json:"width"`
	} `json:"layout"`
	Overflow bool `json:"overflow"`

	Question      []string `json:"question"`
	QuestionShown string   `json:"questionShown"`
	QuestionBack  []string `json:"questionBack"`
	Gone          []string `json:"gone"`

	AcmeOn            []string `json:"acmeOn"`
	InfraOn           []string `json:"infraOn"`
	SessionChipsAfter []string `json:"sessionChipsAfter"`
	ProjectSheet      struct {
		Heads []string `json:"heads"`
		Items []string `json:"items"`
	} `json:"projectSheet"`
	ProjectPicked      []string `json:"projectPicked"`
	SessionChipsPicked []string `json:"sessionChipsPicked"`

	ShopOn           []string `json:"shopOn"`
	StackChipsAfter  []string `json:"stackChipsAfter"`
	StackSheet       []string `json:"stackSheet"`
	StackPicked      []string `json:"stackPicked"`
	StackChipsPicked []string `json:"stackChipsPicked"`

	LimitOff   []string `json:"limitOff"`
	LimitChips []string `json:"limitChips"`
	LimitKind  []string `json:"limitKind"`
	IdleAfter  []string `json:"idleAfter"`

	Rules         []prefsRow `json:"rules"`
	ScrimCovers   bool       `json:"scrimCovers"`
	RuleOff       []int      `json:"ruleOff"`
	RuleShown     string     `json:"ruleShown"`
	RulesRowAfter string     `json:"rulesRowAfter"`

	Refused struct {
		Sent  []string `json:"sent"`
		Shown string   `json:"shown"`
		Toast string   `json:"toast"`
		Bad   bool     `json:"bad"`
	} `json:"refused"`
	LockedPuts  int      `json:"lockedPuts"`
	LockedShown []string `json:"lockedShown"`
	Test        string   `json:"test"`
	Back        bool     `json:"back"`
}

// sameSet says whether two lists hold the same words, in any order: the
// screen appends, the service sorts, and either is the same choice.
func sameSet(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}

// The choice of pushes is reached from the alerts screen, which keeps only a
// door to it: the device's own switch lives on the new screen, over the kinds
// of news. Every switch and chip writes the list it stands for — a kind to
// off, a contour or a project to sessions, a stack to stacks, a contour to
// limits, a rule to rules — and a save the service refuses is taken back.
func TestTheChoiceOfPushesOnAPhone(t *testing.T) {
	var got prefsShot
	runFixture(t, "pushprefs.html", &got)
	checkPushChoice(t, got)

	for i, g := range got.Layout {
		if i > 0 && g.Top <= got.Layout[i-1].Top {
			t.Errorf("on a phone the groups stand one under another, group %d is at %+v after %+v", i, g, got.Layout[i-1])
		}
	}
}

// At a desk the same screen stands as a page over a section, its four groups
// two by two rather than a strip of switches as wide as the page.
func TestTheChoiceOfPushesAtADesk(t *testing.T) {
	var got prefsShot
	runWideFixture(t, "pushprefs.html", &got)
	checkPushChoice(t, got)

	if len(got.Layout) != 4 {
		t.Fatalf("groups laid out: %+v", got.Layout)
	}
	l := got.Layout
	if l[0].Top != l[1].Top || l[1].Left <= l[0].Left || l[2].Top <= l[0].Top || l[2].Left != l[0].Left {
		t.Errorf("at a desk the groups stand two by two, they stand at %+v", l)
	}
}

func checkPushChoice(t *testing.T, got prefsShot) {
	t.Helper()

	if got.Alerts.OldBlock || slices.Contains(got.Alerts.Buttons, "Test") || slices.Contains(got.Alerts.Buttons, "Turn off") {
		t.Errorf("the alerts screen still carries the old block of notifications: buttons %v", got.Alerts.Buttons)
	}
	if !strings.Contains(got.Alerts.Door, "on for this device") || !strings.Contains(got.Alerts.Door, "choose what reaches the phone") {
		t.Errorf("the door on the alerts screen reads %q, meant the state of this device and the way in", got.Alerts.Door)
	}
	if !got.Back {
		t.Error("the way back from the notifications does not lead to the alerts")
	}

	if got.Head != "Notifications" || strings.Join(got.Heads, "|") != "needs you|sessions|containers|the host" {
		t.Errorf("the screen is %q with groups %v", got.Head, got.Heads)
	}
	d := got.Device.Row
	if d == nil || d.Title != "This device gets them" || d.Role != "switch" || d.On != "true" || d.Disabled ||
		d.Sub != "phone · the choice below is the same on every device" {
		t.Errorf("the card of this device reads %+v", d)
	}
	if strings.Join(got.Device.Buttons, "|") != "Send a test" || got.Test != "Send a test notification?" {
		t.Errorf("the card of this device offers %v, and the test asks %q", got.Device.Buttons, got.Test)
	}
	if got.Gets != 1 {
		t.Errorf("the choice was read %d times, meant once", got.Gets)
	}
	if got.Overflow {
		t.Error("a row of the screen is wider than the screen")
	}

	want := []prefsRow{
		{Title: "A session calls you", Sub: "it was asked to — always comes", Role: "switch", On: "true", Disabled: true, Lock: "always"},
		{Title: "A question", Role: "switch", On: "true"},
		{Title: "Waits for a permission", Role: "switch", On: "true"},
		{Title: "A brief is ready", Role: "switch", On: "true"},
		{Title: "A long turn finished", Sub: "10 min and longer", Role: "switch", On: "true"},
		{Title: "A session closed by itself", Sub: "from the same places", Role: "switch", On: "false"},
		{Title: "A stack went down", Sub: "one push for the stack, not one per container", Role: "switch", On: "true"},
		{Title: "A container fell or is unhealthy", Sub: "while the rest of its stack runs", Role: "switch", On: "true"},
		{Title: "…and when it is back", Role: "switch", On: "false"},
		{Title: "Rules", Sub: "4 of 4 · Container eats CPU, Host CPU is loaded, Disk is filling up, The claude limit is running out"},
		{Title: "Probes stop answering", Sub: "3 · API Anthropic, API Google, API Telegram", Role: "switch", On: "true"},
		{Title: "A claude limit past 80%", Role: "switch", On: "true"},
		{Title: "The panel went blind", Sub: "otherwise a failure passes in silence", Role: "switch", On: "true", Disabled: true, Lock: "always"},
	}
	if len(got.Rows) != len(want) {
		t.Fatalf("rows drawn: %+v", got.Rows)
	}
	for i, w := range want {
		if got.Rows[i] != w {
			t.Errorf("row %d reads %+v, meant %+v", i, got.Rows[i], w)
		}
	}
	if got.LockedPuts != 0 || strings.Join(got.LockedShown, " ") != "true true" {
		t.Errorf("a call and a blind panel moved when pressed: %d saves, shown %v", got.LockedPuts, got.LockedShown)
	}

	chips := map[string]string{
		"sessions": "personal|work|acme (off)|infra (off)",
		"stacks":   "acme-dev (off)|acme-top (off)|shop (off)",
		"limits":   "personal|work|acme",
	}
	for what, w := range chips {
		if strings.Join(got.Chips[what], "|") != w {
			t.Errorf("the chips of %s read %v, meant %s", what, got.Chips[what], w)
		}
	}
	if strings.Join(got.Idle, "") != "000" || strings.Join(got.IdleAfter, "") != "001" {
		t.Errorf("chips stand quiet %v, then %v — they go quiet only when the kinds they choose for are off", got.Idle, got.IdleAfter)
	}

	if !sameSet(got.Question, []string{"ask", "back", "gone"}) || got.QuestionShown != "false" {
		t.Errorf("turning off a question sent off %v and shows %q", got.Question, got.QuestionShown)
	}
	if !sameSet(got.QuestionBack, []string{"back", "gone"}) || !sameSet(got.Gone, []string{"back"}) {
		t.Errorf("turning kinds back on sent off %v, then %v", got.QuestionBack, got.Gone)
	}

	const infra = "/srv/proj/acme/infra"
	const panel = "/srv/proj/panel"
	if !sameSet(got.AcmeOn, []string{infra}) || len(got.InfraOn) != 0 {
		t.Errorf("hearing acme and infra again sent sessions %v, then %v", got.AcmeOn, got.InfraOn)
	}
	if strings.Join(got.SessionChipsAfter, "|") != "personal|work|acme" {
		t.Errorf("after infra is heard again the chips read %v — a project is a chip while it is quiet", got.SessionChipsAfter)
	}
	if strings.Join(got.ProjectSheet.Heads, "|") != "personal|work|acme" || len(got.ProjectSheet.Items) != 5 ||
		got.ProjectSheet.Items[0] != "panel | "+panel {
		t.Errorf("the projects offered: %+v", got.ProjectSheet)
	}
	if !sameSet(got.ProjectPicked, []string{panel}) || strings.Join(got.SessionChipsPicked, "|") != "personal|work|acme|panel (off)" {
		t.Errorf("quieting panel sent sessions %v, the chips read %v", got.ProjectPicked, got.SessionChipsPicked)
	}

	if !sameSet(got.ShopOn, []string{"acme-dev", "acme-top"}) ||
		strings.Join(got.StackChipsAfter, "|") != "acme-dev (off)|acme-top (off)" {
		t.Errorf("hearing shop again sent stacks %v, the chips read %v", got.ShopOn, got.StackChipsAfter)
	}
	if strings.Join(got.StackSheet, "|") != "home-assistant|panel|shop" {
		t.Errorf("the stacks offered: %v — only those not quiet yet", got.StackSheet)
	}
	if !sameSet(got.StackPicked, []string{"acme-dev", "acme-top", "home-assistant"}) ||
		!slices.Contains(got.StackChipsPicked, "home-assistant (off)") {
		t.Errorf("quieting home-assistant sent stacks %v, the chips read %v", got.StackPicked, got.StackChipsPicked)
	}

	if !sameSet(got.LimitOff, []string{"/home/u/.claude"}) || strings.Join(got.LimitChips, "|") != "personal (off)|work|acme" {
		t.Errorf("quieting the limits of personal sent limits %v, the chips read %v", got.LimitOff, got.LimitChips)
	}
	if !slices.Contains(got.LimitKind, "limit") {
		t.Errorf("turning the limits off sent off %v", got.LimitKind)
	}

	if !got.ScrimCovers {
		t.Error("the scrim of an open sheet does not cover the window — the sides of the screen stay pressable under it")
	}
	if len(got.Rules) != 5 {
		t.Fatalf("the rules listed: %+v", got.Rules)
	}
	down := got.Rules[4]
	if down.Title != "The whole stack is down" || down.On != "false" || !down.Disabled || down.Lock != "never" ||
		down.Sub != "never pushes: the stack pushes that itself" {
		t.Errorf("the rule that a whole stack is down reads %+v — it never pushes and cannot be turned on", down)
	}
	if off := got.Rules[3]; off.Dim != "1" || off.Sub != "the rule is off" || off.Disabled || off.On != "true" {
		t.Errorf("a rule that is itself off reads %+v", off)
	}
	if !slices.Equal(got.RuleOff, []int{3}) || got.RuleShown != "false" {
		t.Errorf("turning off a rule sent rules %v and shows %q", got.RuleOff, got.RuleShown)
	}
	if !strings.HasPrefix(got.RulesRowAfter, "3 of 4 · ") || strings.Contains(got.RulesRowAfter, "Host CPU") {
		t.Errorf("the row of the rules still reads %q after one was turned off", got.RulesRowAfter)
	}

	if !slices.Contains(got.Refused.Sent, "brief") || got.Refused.Shown != "true" ||
		!strings.Contains(got.Refused.Toast, "The choice was not saved") || !got.Refused.Bad {
		t.Errorf("a refused save: sent %v, the switch shows %q, the note %q (bad %v) — meant taken back with a note",
			got.Refused.Sent, got.Refused.Shown, got.Refused.Toast, got.Refused.Bad)
	}
}
