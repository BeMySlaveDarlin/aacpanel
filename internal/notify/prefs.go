package notify

import (
	"slices"
	"strconv"
	"strings"
)

// The kinds of what a push says. A kind is read off the key of the event, so
// an event raised before kinds existed still has one.
const (
	KindCall      = "call"
	KindAsk       = "ask"
	KindWait      = "wait"
	KindBrief     = "brief"
	KindDone      = "done"
	KindGone      = "gone"
	KindStack     = "stack"
	KindContainer = "container"
	KindRule      = "rule"
	KindProbe     = "probe"
	KindLimit     = "limit"
	KindBlind     = "blind"
	// KindBack is not a kind of event but the recovery of a container or a
	// stack: the person turns it off apart from the fall.
	KindBack = "back"
)

var keyKinds = []struct{ prefix, kind string }{
	{"note:", KindCall},
	{"ask:", KindAsk},
	{"wait:", KindWait},
	{"brief:", KindBrief},
	{"done:", KindDone},
	{"gone:", KindGone},
	{"stack:", KindStack},
	{"container:", KindContainer},
	{"health:", KindContainer},
	{"alert:", KindRule},
	{"probe:", KindProbe},
	{"limit:", KindLimit},
	{"blind:", KindBlind},
}

// kindOf is the kind of the event a key belongs to, or nothing for the
// panel's own words — a test, a lost subscription.
func kindOf(key string) string {
	for _, k := range keyKinds {
		if strings.HasPrefix(key, k.prefix) {
			return k.kind
		}
	}
	return ""
}

// kindsOff are the kinds a person may turn off. A call is a session asked to
// fetch the person and a blind panel is a failure that would pass in silence:
// neither is on the list, so neither is ever held back.
var kindsOff = []string{KindAsk, KindWait, KindBrief, KindDone, KindGone, KindStack, KindContainer,
	KindBack, KindProbe, KindLimit}

// Source is where an event comes from, as far as a push can be quieted by it.
type Source struct {
	// Contour is the configuration directory of the account a session or a
	// limit belongs to: the one key the snapshot, the limits and the map share.
	Contour string `json:"contour,omitempty"`
	// Place is the directory of a session.
	Place string `json:"place,omitempty"`
	Stack string `json:"stack,omitempty"`
	Rule  int64  `json:"rule,omitempty"`
	// Name is what the source is called on a button that quiets it.
	Name string `json:"name,omitempty"`
}

// Prefs is what the person chose to hear, the same on every device. Each list
// names what is off, so a kind or a source the panel learns later is heard.
type Prefs struct {
	Off []string `json:"off"`
	// Rules are the rules whose alerts do not push.
	Rules []int64 `json:"rules"`
	// Sessions are contours and directories whose turns and closings do not
	// push: a session is held back when its contour is here, or its directory
	// is one of these or inside one. What needs the person — a question, a
	// permission, a call, a brief — comes from everywhere.
	Sessions []string `json:"sessions"`
	// Stacks are the stacks whose falls and recoveries do not push.
	Stacks []string `json:"stacks"`
	// Limits are the contours whose limits do not push.
	Limits []string `json:"limits"`
}

// Clean returns the choice with what cannot be chosen left out and every list
// sorted and without repeats, so that two equal choices are stored the same.
func (p Prefs) Clean() Prefs {
	words := func(in []string, keep func(string) bool) []string {
		out := []string{}
		for _, s := range in {
			s = strings.TrimSpace(s)
			if s != "" && keep(s) && !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
		slices.Sort(out)
		return out
	}
	all := func(string) bool { return true }
	rules := []int64{}
	for _, id := range p.Rules {
		if id > 0 && !slices.Contains(rules, id) {
			rules = append(rules, id)
		}
	}
	slices.Sort(rules)
	return Prefs{
		Off:      words(p.Off, func(k string) bool { return slices.Contains(kindsOff, k) }),
		Rules:    rules,
		Sessions: words(p.Sessions, all),
		Stacks:   words(p.Stacks, all),
		Limits:   words(p.Limits, all),
	}
}

// Allows reports whether a message goes out under the choice.
func (p Prefs) Allows(m Message) bool {
	switch m.Kind {
	case "", KindCall, KindBlind:
		return true
	}
	if slices.Contains(p.Off, m.Kind) {
		return false
	}
	if m.Back && (m.Kind == KindStack || m.Kind == KindContainer) && slices.Contains(p.Off, KindBack) {
		return false
	}
	switch m.Kind {
	case KindDone, KindGone:
		return !p.sessionOff(m.Source)
	case KindStack, KindContainer:
		return m.Source.Stack == "" || !slices.Contains(p.Stacks, m.Source.Stack)
	case KindRule:
		return m.Source.Rule == 0 || !slices.Contains(p.Rules, m.Source.Rule)
	case KindLimit:
		return m.Source.Contour == "" || !slices.Contains(p.Limits, m.Source.Contour)
	}
	return true
}

func (p Prefs) sessionOff(s Source) bool {
	for _, off := range p.Sessions {
		if s.Contour != "" && off == s.Contour {
			return true
		}
		root := strings.TrimSuffix(off, "/")
		if s.Place != "" && root != "" && (s.Place == root || strings.HasPrefix(s.Place, root+"/")) {
			return true
		}
	}
	return false
}

// Quiet is the button on a push that turns its source off.
type Quiet struct {
	// What is the list the key goes to: sessions, stacks, rules, limits, or
	// off for a whole kind.
	What  string `json:"what"`
	Key   string `json:"key"`
	Label string `json:"label"`
}

// quietOf is the button a message carries, or nothing: what needs the person
// has none, and a source the push cannot name has none.
func quietOf(kind string, s Source) *Quiet {
	name := s.Name
	switch kind {
	case KindDone, KindGone:
		if s.Place == "" {
			return nil
		}
		return &Quiet{What: "sessions", Key: s.Place, Label: "Quiet: " + orKey(name, baseName(s.Place))}
	case KindStack, KindContainer:
		if s.Stack == "" {
			return nil
		}
		return &Quiet{What: "stacks", Key: s.Stack, Label: "Quiet: " + s.Stack}
	case KindRule:
		if s.Rule == 0 {
			return nil
		}
		return &Quiet{What: "rules", Key: strconv.FormatInt(s.Rule, 10), Label: "Quiet this rule"}
	case KindProbe:
		return &Quiet{What: "off", Key: KindProbe, Label: "Quiet: probes"}
	case KindLimit:
		if s.Contour == "" {
			return nil
		}
		return &Quiet{What: "limits", Key: s.Contour, Label: "Quiet: " + orKey(name, baseName(s.Contour))}
	}
	return nil
}

func orKey(name, key string) string {
	if name != "" {
		return name
	}
	return key
}

// Mute returns the choice with the source of a button turned off. A button
// that names nothing the choice knows leaves it as it was, and says so.
func (p Prefs) Mute(q Quiet) (Prefs, bool) {
	key := strings.TrimSpace(q.Key)
	if key == "" {
		return p, false
	}
	switch q.What {
	case "sessions":
		p.Sessions = append(p.Sessions, key)
	case "stacks":
		p.Stacks = append(p.Stacks, key)
	case "limits":
		p.Limits = append(p.Limits, key)
	case "rules":
		id, err := strconv.ParseInt(key, 10, 64)
		if err != nil || id <= 0 {
			return p, false
		}
		p.Rules = append(p.Rules, id)
	case "off":
		if !slices.Contains(kindsOff, key) {
			return p, false
		}
		p.Off = append(p.Off, key)
	default:
		return p, false
	}
	return p.Clean(), true
}
