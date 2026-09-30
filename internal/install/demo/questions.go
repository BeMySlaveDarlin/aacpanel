package demo

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"aacpanel/internal/contours"
	"aacpanel/internal/install/ui"
)

// The questions of the installer, block by block, as the plan orders them.
// They are data for ui.Block; what an answer changes is read back from the
// block by id when the plan and the steps are drawn up.

const typeOwn = "Type something."

var (
	hostName    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,62}$`)
	sessionName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	cleanPath   = regexp.MustCompile(`^(~|/)[A-Za-z0-9._/-]*$`)
)

func checkHost(s string) error {
	if !hostName.MatchString(s) {
		return errors.New("a host name takes letters, digits, dots and dashes")
	}
	return nil
}

func checkSession(s string) error {
	if !sessionName.MatchString(s) {
		return errors.New("a session name takes lower-case letters, digits, dashes and underscores")
	}
	return nil
}

func checkPath(s string) error {
	if !cleanPath.MatchString(s) {
		return errors.New("a path starts with / or ~ and carries no spaces or quotes: unit files cannot hold them")
	}
	return nil
}

func options(fs []found) []ui.Option {
	out := make([]ui.Option, len(fs))
	for i, f := range fs {
		out[i] = ui.Option{Label: f.value, Detail: f.where}
	}
	return out
}

func prerequisites(mc machine) *ui.Block {
	why := map[string][2]string{
		"tmux": {
			"Sessions without the stream live in tmux, and a session on the stream moves into tmux when the panel is down (tmux attach). Windows to a session and the panel's live terminal go through it as well.",
			"Without tmux the executor turns off every action on sessions: their buttons go grey. The run stops here.",
		},
		"jq": {
			"The status line script reads the limits with it: without jq the 5-hour and weekly percentages are silently not written.",
			"The run stops here; install it yourself and run again.",
		},
	}
	var qs []ui.Question
	for _, pkg := range mc.missing {
		qs = append(qs, ui.Question{
			ID:     "pkg:" + pkg,
			Tab:    pkg,
			Prompt: pkg + " is missing. Install it?",
			Options: []ui.Option{
				{Label: "Yes — with apt, in the root step", Value: "yes", Detail: why[pkg][0]},
				{Label: "No — I will install it myself", Value: "no", Detail: why[pkg][1]},
			},
		})
	}
	return ui.NewBlock("Prerequisites", qs...)
}

func thisMachine(mc machine) *ui.Block {
	b := ui.NewBlock("This machine",
		ui.Question{
			ID: "host", Tab: "Host", Prompt: "What does the panel call this machine?",
			Options: []ui.Option{{Label: mc.host, Detail: "From hostname. Metrics history is keyed by this name: changing it later starts a new history."}},
			Own:     typeOwn, Check: checkHost,
		},
		ui.Question{
			ID: "home", Tab: "Session", Prompt: "Name of the home session (the machine's own claude session)?",
			Options: []ui.Option{{Label: strings.ToLower(mc.host), Detail: "The host name in lower case."}},
			Own:     typeOwn, Check: checkSession,
		},
		ui.Question{
			ID: "roots", Tab: "Projects", Prompt: "Where do your projects live?", Kind: ui.Multi,
			Note:    "The panel looks for projects three levels deep under these; the home directory is never walked whole.",
			Options: rootOptions(mc),
			Own:     "Type a path.", Check: checkPath,
		},
		ui.Question{
			ID: "state", Tab: "State", Prompt: "Where does the panel keep its state?",
			Options: []ui.Option{{Label: "/var/lib/aacpanel", Detail: "Recommended. The root step creates it for " + mc.user + ", and the collector's unit points there as written."}},
			Own:     typeOwn, Check: checkPath,
		},
	)
	// The home session is named after the host: a host named by hand moves
	// the suggestion with it.
	b.Refresh = func(b *ui.Block) {
		if host := b.Value("host"); host != "" {
			b.Find("home").Options[0].Label = strings.ToLower(host)
		}
	}
	return b
}

func rootOptions(mc machine) []ui.Option {
	out := options(mc.roots)
	out[0].On = true
	return out
}

func windows(mc machine) *ui.Block {
	terms := options(mc.terminals)
	terms = append(terms, ui.Option{
		Label: "No windows — sessions live in tmux only", Value: "none",
		Detail: "The panel still shows every session and its terminal; a window opens by hand, with tmux attach.",
	})
	b := ui.NewBlock("Session windows",
		ui.Question{
			ID: "terminal", Tab: "Terminal", Prompt: "How should the panel open a window to a session?",
			Note:    "A command of your own takes %s where tmux attach goes.",
			Options: terms, Own: typeOwn,
		},
		ui.Question{
			ID: "auto", Tab: "On start", Prompt: "Open a window when a session starts?",
			Options: []ui.Option{
				{Label: "Yes", Value: "yes", Detail: "A window opens on the display the moment a session starts."},
				{Label: "No, only on request", Value: "no", Detail: "A window opens when you ask for one on the panel."},
			},
		},
		ui.Question{
			ID: "display", Tab: "Display", Prompt: "Which display do the windows open on?",
			Options: options(mc.displays), Own: typeOwn,
		},
		ui.Question{
			ID: "locale", Tab: "Locale", Prompt: "Which locale do sessions run in?",
			Options: options(mc.locales),
		},
	)
	// Without windows there is nothing to open on start and no display to
	// open it on; the locale is asked all the same.
	b.Refresh = func(b *ui.Block) {
		none := b.Value("terminal") == "none"
		b.Find("auto").Hidden = none
		b.Find("display").Hidden = none
	}
	return b
}

func claude(mc machine) *ui.Block {
	accounts := options(mc.accounts)
	accounts[0].On = true
	return ui.NewBlock("Claude",
		ui.Question{
			ID: "claude", Tab: "claude", Prompt: "What starts claude?",
			Options: []ui.Option{{Label: mc.claudePath, Detail: "Found with command -v claude. The link is kept as it is, so an update of claude reaches the panel."}},
			Own:     typeOwn, Check: checkPath,
		},
		ui.Question{
			ID: "accounts", Tab: "Accounts", Prompt: "Which claude accounts does the panel serve?", Kind: ui.Multi,
			Note:    "Each account gets the panel's hooks and tools and becomes a contour on the map.",
			Options: accounts, Own: "Type a path.", Check: checkPath,
		},
		ui.Question{
			ID: "transport", Tab: "Sessions", Prompt: "How are sessions kept?",
			Options: []ui.Option{
				{Label: "On the stream (Recommended)", Value: "stream", Detail: "The panel speaks claude's own protocol: the feed, questions and permissions as in the native client. tmux stays as the way back."},
				{Label: "In tmux", Value: "tmux", Detail: "claude runs in a terminal and the panel types the keys; tmux attach reaches a session whatever happens to the panel."},
			},
		},
	)
}

// The parts of the kit, by group. The first group is the panel itself and
// does not come off; the second is checked for the person and stays quiet;
// the rest waits to be asked for.
const (
	gAlways = "Always"
	gQuiet  = "Quiet — checked for you"
	gAsk    = "On request"
	gLegs   = "Ways in besides this machine"
	gDev    = "For development"
)

func kit() *ui.Block {
	part := func(group, value, label, detail string) ui.Option {
		return ui.Option{
			Group: group, Value: value, Label: label, Detail: detail,
			Locked: group == gAlways, On: group == gAlways || group == gQuiet,
		}
	}
	return ui.NewBlock("Kit", ui.Question{
		ID: "kit", Tab: "Kit", Prompt: "What goes into this install?", Kind: ui.Multi,
		Note: "Space checks a part, Enter takes the list as it stands.",
		Options: []ui.Option{
			part(gAlways, "relay", "Question relay", "a session's questions reach your phone"),
			part(gAlways, "limits", "Limits snapshot", "5-hour and weekly limits, before your status line"),
			part(gAlways, "tools", "Panel tools", "checklist, briefs, a call to your phone, restart"),
			part(gQuiet, "copies", "Page copies", "a published page opens on the phone under any account"),
			part(gQuiet, "brief", "Brief reminder", "a session learns that an answered brief waits"),
			part(gQuiet, "cap", "Context cap", "stops a session at the cap, restarts it with your line"),
			part(gQuiet, "nudge", "Checklist nudge", "reminds a session that forgot its checklist"),
			part(gQuiet, "restart", "Self-restart", "a session may restart itself without asking you"),
			part(gAsk, "stamp", "Prompt stamp", "time, context, limits and load before every prompt"),
			part(gAsk, "cost", "Cost snapshot", "spend per session, written after every turn"),
			part(gAsk, "background", "Background work", "reminds a session of work left running over an hour"),
			part(gAsk, "gc", "Docker cleanup", "weekly prune of unused images — of the whole machine"),
			part(gLegs, "tailscale", "Tailscale", "your phone from anywhere, through your tailnet"),
			part(gLegs, "lan", "Home network, TLS", "straight at home, with a certificate you issue"),
			part(gLegs, "domain", "Own domain", "behind your reverse proxy"),
			part(gDev, "testdb", "Test database", "for make check: forty tests skip without it"),
		},
	})
}

var legs = []string{"tailscale", "lan", "domain"}

func access(mc machine, picked []string) *ui.Block {
	on := map[string]bool{}
	for _, p := range picked {
		on[p] = true
	}
	var qs, homes []ui.Question
	var addrs []ui.Option
	if on["tailscale"] {
		qs = append(qs,
			ui.Question{
				ID: "tskey", Tab: "Tailscale key", Kind: ui.Secret,
				Prompt:      "Tailscale auth key (one-time, admin console → Settings → Keys)",
				Note:        "Turn on MagicDNS and HTTPS Certificates in the tailnet's DNS page first. The key is used once and wiped after the node signs in.",
				Placeholder: "tskey-auth-…",
				Check: func(s string) error {
					if !strings.HasPrefix(s, "tskey-") {
						return errors.New("a Tailscale auth key starts with tskey-")
					}
					return nil
				},
			},
			ui.Question{
				ID: "tsname", Tab: "Node", Prompt: "Node name in the tailnet?",
				Options: []ui.Option{{Label: "aacpanel", Detail: "The phone opens https://aacpanel." + mc.tailnet + "."}},
				Own:     typeOwn, Check: checkHost,
			},
		)
		addrs = append(addrs, ui.Option{Label: "aacpanel." + mc.tailnet, Value: "tailscale", Detail: "The tailnet's name of the panel."})
	}
	if on["lan"] {
		certs := options(mc.certs)
		certs = append(certs, ui.Option{
			Label: "No TLS — plain http", Value: "plain",
			Detail: "The session cookie goes without Secure: whoever sees the traffic on the network sees the cookie.",
		})
		qs = append(qs,
			ui.Question{
				ID: "lanaddr", Tab: "LAN address", Prompt: "Which address should the panel listen on in the home network?",
				Options: options(mc.lan), Own: typeOwn,
			},
			ui.Question{
				ID: "lancert", Tab: "Certificate", Prompt: "Certificate and key of the home-network address?",
				Options: certs, Own: typeOwn, Check: checkPath,
			},
		)
		addrs = append(addrs, ui.Option{Label: "helios.lan", Value: "lan", Detail: "The name on the home-network certificate."})
	}
	if on["domain"] {
		qs = append(qs,
			ui.Question{
				ID: "domain", Tab: "Domain", Kind: ui.Text, Prompt: "Domain of the panel?",
				Note:        "The reverse proxy, the DNS record and the certificate stay yours; the panel only needs its address.",
				Placeholder: "panel.example.org", Check: checkHost,
			},
			ui.Question{
				ID: "bind", Tab: "Proxy", Prompt: "Where does your reverse proxy reach the panel?",
				Options: options(mc.proxies), Own: typeOwn,
			},
		)
		addrs = append(addrs, ui.Option{Label: "your domain", Value: "domain", Detail: "The address your reverse proxy serves."})
	}
	if len(addrs) > 1 {
		for i := range addrs {
			addrs[i].Detail += " A passkey is bound to its address for good: changing it wipes every passkey. The other addresses sign in through a handoff from this one."
		}
		homes = append(homes, ui.Question{
			ID: "passkey", Tab: "Passkeys", Prompt: "Which address do passkeys belong to?", Options: addrs,
		})
	}
	qs = append(qs, homes...)
	qs = append(qs, ui.Question{
		ID: "term", Tab: "Terminal", Prompt: "Terminal of live sessions from other devices?",
		Options: []ui.Option{
			{Label: "No — local panel only (Recommended)", Value: "no", Detail: "The live terminal opens only on the panel of this machine."},
			{Label: "Yes — behind a passkey", Value: "yes", Detail: "Whoever holds a passkey of the panel gets a shell as " + mc.user + " here, from wherever the panel is reachable."},
		},
	})
	return ui.NewBlock("Access", qs...)
}

func tune() *ui.Block {
	return ui.NewBlock("More settings", ui.Question{
		ID: "tune", Prompt: "Tune more settings?",
		Options: []ui.Option{
			{Label: "No — defaults", Value: "no", Detail: "Probes of local services, the contact for pushes and the length of a sign-in keep their defaults."},
			{Label: "Yes", Value: "yes", Detail: "Three more questions."},
		},
	})
}

func more(mc machine) *ui.Block {
	return ui.NewBlock("More settings",
		ui.Question{
			ID: "probes", Tab: "Probes", Prompt: "Which local services should the Machine screen probe?", Kind: ui.Multi,
			Note:    "Listening ports found with ss; the panel's own are left out.",
			Options: options(mc.ports),
		},
		ui.Question{
			ID: "push", Tab: "Push contact", Prompt: "Contact for push services?",
			Options: []ui.Option{{Label: "mailto:" + mc.email, Detail: "From git config user.email. A push service writes here when pushes misbehave."}},
			Own:     typeOwn,
		},
		ui.Question{
			ID: "signin", Tab: "Sign-in", Prompt: "How long does a sign-in last?",
			Options: []ui.Option{
				{Label: "30m idle · 12h at most", Value: "30m/12h", Detail: "The defaults."},
				{Label: "2h idle · 7 days at most", Value: "2h/168h", Detail: "For a phone that is opened now and then."},
			},
			Own: "Type idle/max, like 1h/24h.",
		},
	)
}

func contourName(dir string) string {
	if dir == "~/.claude" {
		return contours.Personal
	}
	name := strings.TrimPrefix(dir[strings.LastIndex(dir, "/")+1:], ".claude-")
	return strings.TrimPrefix(name, ".")
}

func mapBlock(mc machine, accounts, roots []string) *ui.Block {
	var qs []ui.Question
	for n, dir := range accounts {
		tab := "Contour"
		if len(accounts) > 1 {
			tab = fmt.Sprintf("Contour %d", n+1)
		}
		qs = append(qs, ui.Question{
			ID: "contour:" + dir, Tab: tab, Prompt: "Name of the contour for " + dir + "?",
			Options: []ui.Option{{Label: contourName(dir), Detail: "A contour gathers the sessions of one account on the Profiles screen."}},
			Own:     typeOwn, Check: checkSession,
		})
	}
	in := map[string]bool{}
	for _, r := range roots {
		in[r] = true
	}
	var projects []ui.Option
	for _, p := range mc.projects {
		if in[p.root] {
			projects = append(projects, ui.Option{Label: p.path, Detail: p.detail, On: p.on})
		}
	}
	qs = append(qs,
		ui.Question{
			ID: "group", Tab: "Group", Prompt: "Create a group for your projects?",
			Options: []ui.Option{
				{Label: "Projects", Detail: "The first group of the contour; more come on the Profiles screen."},
				{Label: "Skip", Value: "-", Detail: "No group now, and no projects in it."},
			},
			Own: typeOwn,
		},
		ui.Question{
			ID: "projects", Tab: "Projects", Kind: ui.Multi,
			Options: projects, Own: "Type a path.", Check: checkPath,
			Skip: "Skip — add projects on the Profiles screen later",
		},
	)
	b := ui.NewBlock("Map", qs...)
	b.Refresh = func(b *ui.Block) {
		group := b.Value("group")
		q := b.Find("projects")
		q.Hidden = group == "-"
		q.Prompt = fmt.Sprintf("Which projects go into the group %q?", group)
	}
	b.Refresh(b)
	return b
}

func testSession() *ui.Block {
	b := ui.NewBlock("Test session", ui.Question{
		ID: "session", Prompt: "Open a test session to check the whole chain?",
		Options: []ui.Option{
			{Label: "Yes", Value: "yes", Detail: "Opens aacpanel-check through the executor, finds it in the panel's snapshot and closes exactly it."},
			{Label: "No", Value: "no", Detail: "./install.sh check --session runs it later."},
		},
	})
	b.EscHint = "skip it"
	return b
}

// confirm is the question under a frame: a numbered choice, Esc for the
// last option, which is always the one that changes nothing.
func confirm(title, prompt string, labels ...string) *ui.Block {
	opts := make([]ui.Option, len(labels))
	for i, l := range labels {
		opts[i] = ui.Option{Label: l}
	}
	b := ui.NewBlock(title, ui.Question{ID: "confirm", Prompt: prompt, Options: opts})
	b.Bare = true
	b.EscHint = "say no"
	return b
}
