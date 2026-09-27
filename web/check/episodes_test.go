package check

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

type episodePlaces struct {
	Where map[string][]string `json:"where"`
	Order []string            `json:"order"`
	Eps   []struct {
		How     string    `json:"how"`
		Open    bool      `json:"open"`
		Heads   []string  `json:"heads"`
		Outcome []string  `json:"outcome"`
		Settled int       `json:"settled"`
		Log     int       `json:"log"`
		Files   []string  `json:"files"`
		Now     *nowPlace `json:"now"`
	} `json:"eps"`
}

// nowPlace is what the exchange going on now does, as the episodes say it.
type nowPlace struct {
	Running bool   `json:"running"`
	Call    string `json:"call"`
	Said    string `json:"said"`
	Thought string `json:"thought"`
}

// placesOf cuts a feed into episodes in the engine and says where each of its
// items went. Without items it is the feed of every role.
func placesOf(t *testing.T, calls ...[]any) []episodePlaces {
	t.Helper()
	raw := runModuleJS(t, "check/fixtures/feedplaces.js", "places", calls)
	out := make([]episodePlaces, len(raw))
	for i, one := range raw {
		b, err := json.Marshal(one)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &out[i]); err != nil {
			t.Fatalf("the places did not parse: %v: %s", err, b)
		}
	}
	return out
}

// Every item of a feed has a place once it is cut into episodes, and one
// place only: its head, what came of it, what it handed over, its turn, the
// log of its work, or the messages still waiting — each of them drawn in the
// feed or one tap away. The only item in two places is the last reply of an
// exchange cut off in the middle of its work: it is what the exchange ended
// on, and a line of its log as well.
func TestNoItemOfTheFeedIsLostToAnEpisode(t *testing.T) {
	got := placesOf(t, []any{nil, true})[0]

	want := map[string]string{
		"tools:1": "log@0", "think:2": "log@0", "ai:3": "outcome@0",
		"mind:4": "log@1", "tools:5": "log@1", "ai:6": "outcome@1", "turn:7": "turn@1",
		"me:10": "head@2", "shots:11": "head@2", "note:12": "head@2", "mind:13": "log@2",
		"tools:14": "log@2", "notice:15": "log@2", "ai:16": "log@2", "taskdone:17": "log@2",
		"tools:18": "log@2", "artifact:19": "shown@2", "artifactlink:20": "url of its artifact",
		"sent:21": "shown@2", "brief:22": "shown@2", "mail:23": "log@2", "ai:24": "outcome@2",
		"note:25": "shown@2", "turn:26": "turn@2",
		"shell:30": "head@3", "shellout:31": "head@3", "ai:32": "outcome@3",
		"permitted:40": "head@4", "tools:41": "log@4", "ai:42": "outcome@4",
		"asked:50": "head@5", "asked:51": "head@5", "ai:52": "outcome@5",
		"wake:60": "head@6", "ai:61": "outcome@6",
		"mail:70": "head@7", "ai:71": "outcome@7",
		"me:79": "head@8", "command:80": "head@8", "turn:85": "turn@8",
		"taskdone:90": "head@9", "ai:91": "outcome@9", "hologram:92": "shown@9", "me:93": "shown@9",
		"me:100": "head@10", "mind:101": "log@10", "tools:102": "log@10", "me:103": "waiting",
	}
	for _, key := range got.Order {
		places := got.Where[key]
		switch {
		case len(places) == 0:
			t.Errorf("%s has no place in any episode: it is drawn nowhere and no tap reaches it", key)
		case len(places) > 1:
			t.Errorf("%s stands in %d places (%v): it would be drawn twice", key, len(places), places)
		case want[key] == "":
			t.Errorf("%s went to %s, and the check does not know that role — add it to the feed of every role", key, places[0])
		case places[0] != want[key]:
			t.Errorf("%s went to %s, expected %s", key, places[0], want[key])
		}
	}
	roles := map[string]bool{}
	for _, key := range got.Order {
		roles[strings.SplitN(key, ":", 2)[0]] = true
	}
	for _, role := range []string{"me", "ai", "mind", "tools", "think", "note", "mail", "turn", "taskdone", "notice",
		"shots", "shellout", "shell", "permitted", "asked", "wake", "command", "sent", "artifact", "brief", "artifactlink"} {
		if !roles[role] {
			t.Errorf("the feed of every role has no %s: the check does not cover it", role)
		}
	}

	if len(got.Eps) != 11 {
		t.Fatalf("%d episodes, expected 11: %+v", len(got.Eps), got.Eps)
	}
	if got.Eps[0].How != "earlier" || len(got.Eps[0].Heads) != 0 {
		t.Errorf("the window begins inside an exchange: the first episode is %q with heads %v, expected an earlier one without a head",
			got.Eps[0].How, got.Eps[0].Heads)
	}
	if got.Eps[1].How != "continued" {
		t.Errorf("work after a report went on in an episode %q, expected one that went on without the person", got.Eps[1].How)
	}
	if files := strings.Join(got.Eps[2].Files, ","); files != "a.css" {
		t.Errorf("the exchange lists the files %q its replies named, expected a.css", files)
	}
	for n, ep := range got.Eps {
		if ep.Open != (n == 10) {
			t.Errorf("episode %d is open: %v — only the last one of a busy session is", n, ep.Open)
		}
	}
	now := got.Eps[10].Now
	if now == nil || !now.Running || now.Call != "MARK-call-102" {
		t.Fatalf("the exchange going on now runs %+v, expected the call the host says is still out", now)
	}
	if now.Said != "mind:101" {
		t.Errorf("the card of now says %q, expected the thought said before the call", now.Said)
	}
	if got.Eps[10].Settled != 0 || got.Eps[10].Log != 1 {
		t.Errorf("the open exchange settled %d of %d entries, expected none: its one step, with the thought it began with, is the one going on now",
			got.Eps[10].Settled, got.Eps[10].Log)
	}
}

// A call is running only while the host says it is still out. A call whose
// result came back, or one the host says nothing about, leaves the session
// thinking; and a session that is not busy has nothing going on at all.
func TestTheCallRunningNowIsTheOneTheHostSaysIsOut(t *testing.T) {
	var roles []map[string]any
	raw := runModuleJS(t, "check/fixtures/feedroles.js", "roleItems", [][]any{{}})
	b, _ := json.Marshal(raw[0])
	if err := json.Unmarshal(b, &roles); err != nil {
		t.Fatal(err)
	}
	strip := func(keys ...string) []map[string]any {
		out := make([]map[string]any, 0, len(roles))
		for _, item := range roles {
			copied := map[string]any{}
			for k, v := range item {
				copied[k] = v
			}
			if calls, ok := item["calls"].([]any); ok {
				var fresh []any
				for _, c := range calls {
					call := map[string]any{}
					for k, v := range c.(map[string]any) {
						call[k] = v
					}
					for _, k := range keys {
						delete(call, k)
					}
					fresh = append(fresh, call)
				}
				copied["calls"] = fresh
			}
			out = append(out, copied)
		}
		return out
	}
	got := placesOf(t,
		[]any{strip("open"), true},
		[]any{strip("open", "failed"), true},
		[]any{roles, false},
		[]any{roles, true},
	)
	last := func(p episodePlaces) *nowPlace { return p.Eps[len(p.Eps)-1].Now }
	if now := last(got[0]); now == nil || now.Running {
		t.Errorf("a call the host no longer says is out is still running: %+v", now)
	}
	if now := last(got[1]); now == nil || now.Running {
		t.Errorf("a call the host says nothing about is taken for running: %+v", now)
	}
	if now := last(got[3]); now == nil || !now.Running {
		t.Errorf("the call the host says is still out is not running: %+v", now)
	}
	if now := last(got[2]); now != nil || got[2].Eps[len(got[2].Eps)-1].Open {
		t.Errorf("a session that is not busy has an exchange going on: %+v", now)
	}
}

// An exchange the person cut off with a message of their own ends on the
// last thing the session said; that reply stays in the log of its work too.
func TestAnExchangeCutOffEndsOnItsLastReply(t *testing.T) {
	items := []any{
		map[string]any{"role": "me", "pos": 1, "text": "go"},
		map[string]any{"role": "ai", "pos": 2, "text": "running the checks"},
		map[string]any{"role": "tools", "kind": "bash", "run": 3, "pos": 3,
			"calls": []any{map[string]any{"name": "Bash", "pos": 3, "index": 0}}},
		map[string]any{"role": "me", "pos": 4, "text": "stop"},
	}
	got := placesOf(t, []any{items, false})[0]
	places := got.Where["ai:2"]
	sort.Strings(places)
	if strings.Join(places, " ") != "log@0 outcome@0" {
		t.Errorf("the last reply of a cut-off exchange went to %v, expected what it ended on and a line of its log", places)
	}
}
