package notify

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

const (
	personal = "/home/me/.claude"
	acme     = "/home/me/.claude-profiles/acme"
)

func msg(key string, src Source) Message {
	return Message{Title: "t", Body: "b", Tag: key, Kind: kindOf(key), Source: src}
}

// Every kind a push has is read off the key of its event.
func TestTheKindOfAPushIsReadOffItsKey(t *testing.T) {
	for key, want := range map[string]string{
		"note:4d89:2026": KindCall, "ask:4d89:x": KindAsk, "wait:4d89:1": KindWait, "brief:b1:x": KindBrief,
		"done:4d89:1": KindDone, "gone:4d89": KindGone, "stack:shop": KindStack, "container:shop-php": KindContainer,
		"health:shop-php": KindContainer, "alert:152": KindRule, "probe:3": KindProbe,
		"limit:5h:globex:1790": KindLimit, "blind:agent": KindBlind, "test": "", "push:1": "",
	} {
		if got := kindOf(key); got != want {
			t.Errorf("%q is a %q push, want %q", key, got, want)
		}
	}
}

// A kind turned off is held back; a call and a blind panel are not, whatever
// the choice says: a session asked to fetch the person, and a panel that
// cannot see would fail in silence.
func TestAKindTurnedOffIsHeldBackButACallAndABlindPanelAreNot(t *testing.T) {
	p := Prefs{Off: []string{KindAsk, KindDone, KindCall, KindBlind}}.Clean()
	if p.Allows(msg("ask:x:1", Source{})) || p.Allows(msg("done:x:1", Source{})) {
		t.Error("a kind turned off went out")
	}
	if !p.Allows(msg("wait:x:1", Source{})) {
		t.Error("a kind left on was held back")
	}
	if !p.Allows(msg("note:x:1", Source{})) || !p.Allows(msg("blind:agent", Source{})) {
		t.Error("a call or a blind panel was held back")
	}
	if !reflect.DeepEqual(p.Off, []string{KindAsk, KindDone}) {
		t.Errorf("the choice keeps %v off: a call and a blind panel cannot be turned off", p.Off)
	}
	if !p.Allows(Message{Title: "t", Body: "b", Tag: "test"}) {
		t.Error("the panel's own word was held back")
	}
}

// The turns and closings of a session are held back by its contour or by its
// directory and what lies inside it; what needs the person comes from
// everywhere.
func TestTheNewsOfASessionIsHeldBackByItsContourOrItsPlace(t *testing.T) {
	p := Prefs{Sessions: []string{acme, "/srv/proj/Side/lab/"}}
	for _, c := range []struct {
		key  string
		src  Source
		goes bool
	}{
		{"done:a:1", Source{Contour: acme, Place: "/srv/proj/Acme/infra"}, false},
		{"gone:a", Source{Contour: personal, Place: "/srv/proj/Side/lab"}, false},
		{"done:a:1", Source{Contour: personal, Place: "/srv/proj/Side/lab/sub"}, false},
		{"done:a:1", Source{Contour: personal, Place: "/srv/proj/Side/labs"}, true},
		{"done:a:1", Source{Contour: personal, Place: "/srv/proj/Side/aacpanel"}, true},
		{"ask:a:1", Source{Contour: acme, Place: "/srv/proj/Acme/infra"}, true},
		{"wait:a:1", Source{Contour: acme, Place: "/srv/proj/Acme/infra"}, true},
	} {
		if got := p.Allows(msg(c.key, c.src)); got != c.goes {
			t.Errorf("%s from %+v goes: %v, want %v", c.key, c.src, got, c.goes)
		}
	}
}

// Stacks, rules and the limits of a contour are held back by name; the
// recovery of a container or a stack has a switch of its own.
func TestStacksRulesAndLimitsAreHeldBackByName(t *testing.T) {
	p := Prefs{Stacks: []string{"shop"}, Rules: []int64{7}, Limits: []string{acme}, Off: []string{KindBack}}
	if p.Allows(msg("container:shop-php", Source{Stack: "shop"})) || p.Allows(msg("stack:shop", Source{Stack: "shop"})) {
		t.Error("a quiet stack pushed")
	}
	if !p.Allows(msg("container:aacpanel-db", Source{Stack: "aacpanel"})) {
		t.Error("a stack left on was held back")
	}
	if p.Allows(msg("alert:9", Source{Rule: 7})) || !p.Allows(msg("alert:9", Source{Rule: 8})) {
		t.Error("the rules were not told apart")
	}
	if p.Allows(msg("limit:5h:acme:1", Source{Contour: acme})) || !p.Allows(msg("limit:5h:personal:1", Source{Contour: personal})) {
		t.Error("the limits of the contours were not told apart")
	}
	up := msg("container:aacpanel-db", Source{Stack: "aacpanel"})
	up.Back = true
	if p.Allows(up) {
		t.Error("a container came back with recoveries turned off")
	}
	cleared := msg("alert:9", Source{Rule: 8})
	cleared.Back = true
	if !p.Allows(cleared) {
		t.Error("the recovery switch of the containers held back a rule")
	}
}

// A button of a push turns its source off, and one that names nothing the
// choice knows changes nothing.
func TestAButtonOfAPushTurnsItsSourceOff(t *testing.T) {
	p := Prefs{}
	for _, q := range []Quiet{
		{What: "stacks", Key: "shop"}, {What: "stacks", Key: "shop"}, {What: "sessions", Key: "/opt/p"},
		{What: "rules", Key: "14"}, {What: "limits", Key: acme}, {What: "off", Key: KindProbe},
	} {
		var ok bool
		if p, ok = p.Mute(q); !ok {
			t.Errorf("%+v was refused", q)
		}
	}
	want := Prefs{Off: []string{KindProbe}, Rules: []int64{14}, Sessions: []string{"/opt/p"},
		Stacks: []string{"shop"}, Limits: []string{acme}}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("the choice is %+v, want %+v", p, want)
	}
	for _, q := range []Quiet{{What: "off", Key: KindCall}, {What: "rules", Key: "x"}, {What: "nope", Key: "k"},
		{What: "stacks", Key: " "}} {
		if _, ok := p.Mute(q); ok {
			t.Errorf("%+v was taken", q)
		}
	}
}

// What a push can be quieted by rides in its body; what needs the person has
// no such button.
func TestAPushCarriesTheButtonThatQuietsItsSource(t *testing.T) {
	for _, c := range []struct {
		key  string
		src  Source
		want *Quiet
	}{
		{"done:a:1", Source{Place: "/opt/p/harness", Name: "harness"}, &Quiet{What: "sessions", Key: "/opt/p/harness", Label: "Quiet: harness"}},
		{"stack:shop", Source{Stack: "shop"}, &Quiet{What: "stacks", Key: "shop", Label: "Quiet: shop"}},
		{"alert:9", Source{Rule: 14}, &Quiet{What: "rules", Key: "14", Label: "Quiet this rule"}},
		{"limit:5h:x:1", Source{Contour: acme, Name: "acme"}, &Quiet{What: "limits", Key: acme, Label: "Quiet: acme"}},
		{"ask:a:1", Source{Place: "/opt/p"}, nil},
		{"container:solo", Source{}, nil},
	} {
		got := quietOf(kindOf(c.key), c.src)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s from %+v carries %+v, want %+v", c.key, c.src, got, c.want)
		}
	}
	raw, err := payloadOf(msg("stack:shop", Source{Stack: "shop"}))
	if err != nil || !strings.Contains(string(raw), `"quiet":{"what":"stacks","key":"shop","label":"Quiet: shop"}`) {
		t.Errorf("the body of the push is %s (%v)", raw, err)
	}
}

// A stack that goes down whole is one push, its own: its containers going
// down with it say nothing, while a container that falls in a stack still
// running is news of its own.
func TestAStackDownWholeIsOnePush(t *testing.T) {
	up := func(name, stack string) Container { return Container{Name: name, Stack: stack, State: "running"} }
	down := func(name, stack string) Container { return Container{Name: name, Stack: stack, State: "exited"} }
	prev, cur := snapshotWorld(), snapshotWorld()
	prev.Containers = []Container{up("shop-php", "shop"), up("shop-db", "shop"), up("web-a", "web"), up("web-b", "web")}
	prev.Stacks = []Stack{{Name: "shop", Running: 2, Total: 2}, {Name: "web", Running: 2, Total: 2}}
	cur.Containers = []Container{down("shop-php", "shop"), down("shop-db", "shop"), down("web-a", "web"), up("web-b", "web")}
	cur.Stacks = []Stack{{Name: "shop", Running: 0, Total: 2}, {Name: "web", Running: 1, Total: 2}}

	var keys []string
	for _, e := range Look(prev, cur).Raise {
		keys = append(keys, e.Key)
	}
	if strings.Join(keys, " ") != "container:web-a stack:shop" {
		t.Errorf("the pushes are %v: the whole stack says it once, the lone fall says its own", keys)
	}
}

// The rule that a whole stack is down says what the stack said minutes ago:
// it pushes nothing, and one it already pushed is forgotten without a
// cleared push.
func TestTheRuleOfAStackDownPushesNothing(t *testing.T) {
	cur := snapshotWorld()
	cur.Alerts = []Alert{{ID: 152, RuleID: 14, RuleKey: ruleStackDown, Rule: "The whole stack is down", Subject: "shop"},
		{ID: 153, RuleID: 7, RuleKey: "disk.filling", Rule: "Disk is filling up", Subject: "/"}}
	r := Look(snapshotWorld(), cur)
	if len(r.Raise) == 0 || r.Raise[len(r.Raise)-1].Key != "alert:153" {
		t.Fatalf("raised %+v", r.Raise)
	}
	for _, e := range r.Raise {
		if e.Key == "alert:152" {
			t.Error("the rule of a whole stack down pushed")
		}
	}
	dropped := false
	for _, k := range r.Drop {
		dropped = dropped || k == "alert:152"
	}
	if !dropped {
		t.Error("an alert of a whole stack down told before stays remembered, and its end would push")
	}
	if r.Raise[len(r.Raise)-1].Source.Rule != 7 {
		t.Errorf("the alert does not carry its rule: %+v", r.Raise[len(r.Raise)-1].Source)
	}
}

// The news of a session names its contour and its directory, so the choice
// can hold it back.
func TestTheNewsOfASessionNamesWhereItComesFrom(t *testing.T) {
	busy, idle := sess(), sess()
	busy.ConfigDir, idle.ConfigDir = acme, acme
	busy.Status, busy.StatusAt = "busy", ms(when.Add(-42*time.Minute))
	idle.Status, idle.StatusAt = "idle", ms(when)
	prev, cur := snapshotWorld(), snapshotWorld()
	prev.Sessions, cur.Sessions = []Session{busy}, []Session{idle}
	r := Look(prev, cur)
	if len(r.Raise) != 1 {
		t.Fatalf("raised %+v", r.Raise)
	}
	want := Source{Contour: acme, Place: "/srv/proj/aacpanel", Name: "aacpanel"}
	if r.Raise[0].Source != want {
		t.Errorf("a finished turn comes from %+v, want %+v", r.Raise[0].Source, want)
	}
	if b, _ := json.Marshal(r.Raise[0]); !strings.Contains(string(b), `"source":{"contour"`) {
		t.Errorf("the source is not kept with the event: %s", b)
	}
}
