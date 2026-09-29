package install

// Flag is a flag of the command line that answers a question, so that a run
// with nobody at a terminal — a script, CI, an agent — gives the same
// answers a person would. Every question has one; a test holds that.
type Flag struct {
	Name  string // as typed: "--host"
	Takes string // what it takes, for the help
	Help  string
	// Many gathers the values of a flag given more than once, joined by
	// commas, as a question of several picks takes them.
	Many bool
	// PerAccount takes dir=value for the question about one account, or a
	// bare value for every account alike.
	PerAccount bool
}

// Switch is a flag without a value that stands for one answer of another
// flag: --no-windows is --terminal with the empty template.
type Switch struct {
	Name, For, Value, Help string
}

// Flags are the flags of the questions, block by block.
var Flags = []Flag{
	{Name: "--keep", Takes: "yes|review|access", Help: "over an earlier install: keep its answers, review them, or change only how the panel is reached"},
	{Name: "--install-docker", Takes: "yes|no", Help: "install docker with apt when it is missing"},
	{Name: "--install-compose", Takes: "yes|no", Help: "install docker compose v2 with apt when it is missing"},
	{Name: "--install-claude", Takes: "yes|no", Help: "install claude with Anthropic's native installer when it is missing"},
	{Name: "--install-tmux", Takes: "yes|no", Help: "install tmux with apt when it is missing"},
	{Name: "--install-jq", Takes: "yes|no", Help: "install jq with apt when it is missing"},
	{Name: "--host", Takes: "NAME", Help: "what the panel calls this machine"},
	{Name: "--home-session", Takes: "NAME", Help: "the name of the machine's own claude session"},
	{Name: "--projects-root", Takes: "DIR", Help: "where projects live; again for more", Many: true},
	{Name: "--state-dir", Takes: "DIR", Help: "where the panel keeps its state"},
	{Name: "--terminal", Takes: "TEMPLATE", Help: "how a window to a session opens, like 'gnome-terminal --'"},
	{Name: "--window-on-start", Takes: "yes|no", Help: "open a window when a session starts"},
	{Name: "--display", Takes: "DISPLAY", Help: "the display windows open on, like :0"},
	{Name: "--locale", Takes: "LOCALE", Help: "the locale sessions run in, like C.UTF-8"},
	{Name: "--claude", Takes: "PATH", Help: "what starts claude"},
	{Name: "--account", Takes: "DIR", Help: "a claude account the panel serves (CLAUDE_CONFIG_DIR); again for more", Many: true},
	{Name: "--transport", Takes: "stream|tmux", Help: "how sessions are kept"},
	{Name: "--claude-login", Takes: "[DIR=]now|later", Help: "sign in to an account that is not signed in, now or later", PerAccount: true},
	{Name: "--kit", Takes: "PARTS|+PART,-PART", Help: "the kit: a list is the whole kit, +part and -part change the suggested one"},
	{Name: "--ts-authkey-file", Takes: "FILE", Help: "a file holding the one-time Tailscale auth key; the key never goes on the command line"},
	{Name: "--ts-hostname", Takes: "NAME", Help: "the node's name in the tailnet"},
	{Name: "--lan-addr", Takes: "ADDRESS", Help: "the home-network address the panel listens on"},
	{Name: "--lan-cert", Takes: "FILE", Help: "the certificate of the home-network address; its .key lies beside it"},
	{Name: "--domain", Takes: "DOMAIN", Help: "the domain of the panel behind your reverse proxy"},
	{Name: "--bind", Takes: "ADDRESS", Help: "where your reverse proxy reaches the panel"},
	{Name: "--passkey-home", Takes: "DOMAIN", Help: "agree to move the passkeys to this domain, which voids every enrolled passkey"},
	{Name: "--lan-token", Takes: "yes|no", Help: "with the home network as the only way in: a token a phone signs in with"},
	{Name: "--term-public", Takes: "yes|no", Help: "the terminal of live sessions from other devices, behind a passkey"},
	{Name: "--more", Takes: "yes|no", Help: "tune the rare settings"},
	{Name: "--probe", Takes: "NAME=HOST:PORT", Help: "a local service the Machine screen probes; again for more", Many: true},
	{Name: "--push-contact", Takes: "CONTACT", Help: "where push services write when pushes misbehave, like mailto:you@example.org"},
	{Name: "--login-life", Takes: "IDLE/MAX", Help: "how long a sign-in lasts, like 30m/12h"},
	{Name: "--contour", Takes: "DIR=NAME", Help: "the name of an account's contour on the map", PerAccount: true},
	{Name: "--group", Takes: "NAME", Help: "the first group of projects on the map"},
	{Name: "--project", Takes: "DIR", Help: "a project in that group; again for more", Many: true},
	{Name: "--check-session", Takes: "yes|no", Help: "open a test session at the end of the check, and close it"},
}

// Switches are the flags that stand for an answer.
var Switches = []Switch{
	{Name: "--no-windows", For: "--terminal", Value: "", Help: "no windows: sessions live in tmux only"},
	{Name: "--lan-plain", For: "--lan-cert", Value: "plain", Help: "the home network over plain http, the session cookie without Secure"},
	{Name: "--no-group", For: "--group", Value: "", Help: "no group of projects on the map now"},
}

// FlagNamed is the flag of a name, and whether there is one.
func FlagNamed(name string) (Flag, bool) {
	for _, f := range Flags {
		if f.Name == name {
			return f, true
		}
	}
	return Flag{}, false
}
