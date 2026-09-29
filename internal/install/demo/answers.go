package demo

import (
	"slices"
	"strings"

	"aacpanel/internal/install/ui"
)

// answers reads the submitted blocks back as the installer would use them.
// A block the run skipped — Access without a way in, More settings declined —
// leaves its values empty. The Tailscale key itself is never kept here: only
// that it was given.
type answers struct {
	host, home, state   string
	roots               []string
	terminal, auto      string
	display, locale     string
	claude, transport   string
	accounts            []string
	kit                 []string
	tsnode, domain, lan string
	tskeySet            bool
	term                string
	tuned               bool
	contours            map[string]string
	group               string
	projects            []string

	// source is where a value came from: what the machine gave, or "you".
	source map[string]string
}

func (a answers) has(part string) bool { return slices.Contains(a.kit, part) }

func (a answers) legs() []string {
	var out []string
	for _, l := range legs {
		if a.has(l) {
			out = append(out, l)
		}
	}
	return out
}

// read gathers the answers from the blocks by their slot names. It also
// notes whether each single answer is the machine's suggestion or the
// person's own.
func read(blocks map[string]*ui.Block) answers {
	a := answers{source: map[string]string{}, contours: map[string]string{}}
	get := func(slot, id string) string {
		if b := blocks[slot]; b != nil {
			return b.Value(id)
		}
		return ""
	}
	from := func(slot, id, detected string) string {
		b := blocks[slot]
		if b == nil {
			return detected
		}
		q, ans, ok := b.Get(id)
		if !ok {
			return detected
		}
		if q.Kind == ui.Single && ans.Choice == 0 {
			a.source[id] = detected
		} else {
			a.source[id] = "you"
		}
		return q.Value(ans)
	}

	a.host = from("A", "host", "hostname")
	a.home = from("A", "home", "host name")
	a.state = from("A", "state", "recommended")
	a.roots = split(get("A", "roots"))
	a.terminal = from("B", "terminal", "found")
	a.auto = get("B", "auto")
	a.display = from("B", "display", "user manager")
	a.locale = from("B", "locale", "$LANG")
	a.claude = from("C", "claude", "command -v")
	a.transport = from("C", "transport", "recommended")
	a.accounts = split(get("C", "accounts"))
	a.kit = split(get("K", "kit"))
	a.tsnode = get("D", "tsname")
	a.domain = get("D", "domain")
	a.lan = get("D", "lanaddr")
	a.tskeySet = get("D", "tskey") != ""
	a.term = get("D", "term")
	a.tuned = get("tune", "tune") == "yes"
	for _, dir := range a.accounts {
		a.contours[dir] = get("M", "contour:"+dir)
	}
	a.group = get("M", "group")
	if a.group != "-" {
		a.projects = split(get("M", "projects"))
	}
	return a
}

func split(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}
