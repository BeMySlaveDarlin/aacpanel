package ui

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

var update = flag.Bool("update", false, "rewrite the golden files of the views")

// golden compares a view with its file in testdata. The view is compared
// without its styling: the golden files are for a person to read, and the
// colours have a test of their own.
func golden(t *testing.T, name, got string) {
	t.Helper()
	got = Strip(got) + "\n"
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v — run go test ./internal/install/ui -update to write it", err)
	}
	if got != string(want) {
		t.Errorf("%s differs from the golden file; the view:\n%s", name, got)
	}
}

func press(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "shift+tab":
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

func typeIn(b *Block, s string) {
	for _, r := range s {
		b.Update(press(string(r)))
	}
}

var plain = NewTheme(false)

func machineBlock() *Block {
	return NewBlock("This machine",
		Question{
			ID: "host", Tab: "Host", Prompt: "What does the panel call this machine?",
			Options: []Option{{Label: "lab", Detail: "From hostname. Metrics history is keyed by this name: changing it later starts a new history."}},
			Own:     "Type something.",
		},
		Question{
			ID: "home", Tab: "Session", Prompt: "Name of the home session?",
			Options: []Option{{Label: "lab", Detail: "The host name in lower case."}},
			Own:     "Type something.",
		},
		Question{
			ID: "roots", Tab: "Projects", Prompt: "Where do your projects live?", Kind: Multi,
			Options: []Option{
				{Label: "/srv/proj", Detail: "3 git repositories", On: true},
				{Label: "/home/u", Detail: "only the home directory itself"},
			},
			Own: "Type a path.",
		},
		Question{
			ID: "state", Tab: "State", Prompt: "Where does the panel keep its state?",
			Options: []Option{{Label: "/var/lib/aacpanel", Detail: "Recommended."}, {Label: "/srv/state"}},
			Own:     "Type something.",
		},
	)
}

func kitBlock() *Block {
	part := func(group, label, detail string, locked, on bool) Option {
		return Option{Group: group, Label: label, Detail: detail, Locked: locked, On: on || locked}
	}
	return NewBlock("Kit", Question{
		ID: "kit", Tab: "Kit", Prompt: "What goes into this install?", Kind: Multi,
		Note: "Space checks a part, Enter takes the list as it stands.",
		Options: []Option{
			part("Always", "Question relay", "a session's questions reach your phone", true, false),
			part("Always", "Panel tools", "checklist, briefs, a call to your phone, restart", true, false),
			part("Quiet — checked for you", "Page copies", "a published page opens on the phone under any account", false, true),
			part("Quiet — checked for you", "Context cap", "stops a session at the cap, restarts it with your line", false, true),
			part("On request", "Docker cleanup", "weekly prune of unused images — of the whole machine", false, false),
			part("Ways in besides this machine", "Tailscale", "your phone from anywhere, through your tailnet", false, false),
			part("Ways in besides this machine", "Home network, TLS", "straight at home, with a certificate you issue", false, false),
		},
	})
}

func secretBlock() *Block {
	return NewBlock("Access",
		Question{
			ID: "key", Tab: "Key", Kind: Secret, Prompt: "Tailscale auth key (one-time, admin console → Settings → Keys)",
			Note: "The key is used once and wiped after the node signs in.", Placeholder: "tskey-auth-…",
			Check: func(s string) error {
				if !strings.HasPrefix(s, "tskey-") {
					return errors.New("a Tailscale auth key starts with tskey-")
				}
				return nil
			},
		},
		Question{ID: "node", Tab: "Node", Prompt: "Node name in the tailnet?", Options: []Option{{Label: "aacpanel"}}, Own: "Type something."},
	)
}

func rootView(width int) string {
	frame := plain.Frame(Frame{Title: "Root command", Edge: plain.RootEdge, Rows: []Row{
		{Text: "sudo bash deploy/install/root.sh apply --user u \\"},
		{Text: "  --staged /srv/state/host.env.staged"},
		{},
		{Text: "Installs tmux jq with apt, creates /var/lib/aacpanel, installs the collector unit and enables linger. Nothing else runs as root."},
	}}, width)
	b := NewBlock("Root command", Question{ID: "confirm", Prompt: "Do you want to run it?", Options: []Option{
		{Label: "Yes"}, {Label: "Show the script first"}, {Label: "No — print the command for an administrator and stop"},
	}})
	b.Bare, b.EscHint = true, "say no"
	return frame + "\n\n" + b.View(plain, width, 0)
}

func collapsedView(width int) string {
	out := []string{
		"go: downloading charm.land/bubbletea/v2 v2.0.10",
		"go: downloading charm.land/lipgloss/v2 v2.0.6",
		"go: downloading github.com/jackc/pgx/v5 v5.10.0",
	}
	lines := plain.Tail(out, 1, "lines")
	return strings.Join([]string{
		plain.Step(Plain, "Build the executor", width),
		plain.Output(lines, width),
		"",
		plain.Spinner(4, "Building the executor", 41*time.Second, "ctrl+c stops after a safe point", width),
		plain.Tasks([]Task{
			{Title: "Check the machine", State: Finished},
			{Title: "Root part (one sudo)", State: Finished},
			{Title: "Executor", State: Running},
			{Title: "Panel stack"},
		}, width),
	}, "\n")
}

func TestViewsMatchTheirGoldenFiles(t *testing.T) {
	for _, width := range []int{80, 60} {
		w := "_" + map[int]string{80: "80", 60: "60"}[width]

		b := machineBlock()
		golden(t, "block"+w, b.View(plain, width, 0))

		b.Update(press("enter"))
		b.Update(press("tab"))
		golden(t, "block_multi"+w, b.View(plain, width, 0))

		b.Update(press("tab"))
		b.Update(press("tab"))
		golden(t, "block_submit"+w, b.View(plain, width, 0))

		golden(t, "kit"+w, kitBlock().View(plain, width, 0))
		golden(t, "root"+w, rootView(width))
		golden(t, "collapsed"+w, collapsedView(width))
	}

	s := secretBlock()
	typeIn(s, "tskey-auth-kQ3x")
	golden(t, "secret_80", s.View(plain, 80, 0))
}

func TestEscOnTheFirstTabStepsBackToTheBlockBefore(t *testing.T) {
	b := machineBlock()
	if got := b.Update(press("esc")); got != Back {
		t.Fatalf("Esc on the first tab: %v, want Back", got)
	}
	b.Update(press("tab"))
	if got := b.Update(press("esc")); got != Open || b.Current() != 0 {
		t.Fatalf("Esc on the second tab: %v on question %d, want Open on the first", got, b.Current())
	}
}

func TestADigitPicksAtOnce(t *testing.T) {
	b := machineBlock()
	b.Update(press("tab"))
	b.Update(press("tab"))
	b.Update(press("tab")) // State: two options and a line of one's own
	if got := b.Update(press("2")); got != Open {
		t.Fatalf("a digit left the block: %v", got)
	}
	if _, a, _ := b.Get("state"); !a.Given || a.Choice != 1 {
		t.Fatalf("the digit did not pick the second option: %+v", a)
	}
	if !b.OnSubmit() {
		t.Fatalf("after a pick the focus stays on question %d, not on Submit", b.Current())
	}
}

func TestADigitOfTheOwnLineMovesThereAndThenTypes(t *testing.T) {
	b := machineBlock()
	b.Update(press("2")) // Host: the own line is the second row
	if _, a, _ := b.Get("host"); a.Given {
		t.Fatal("the digit of the own line answered with nothing typed")
	}
	typeIn(b, "lab2")
	b.Update(press("enter"))
	q, a, _ := b.Get("host")
	if got := q.Value(a); got != "lab2" {
		t.Fatalf("the own line answered %q, want lab2: a digit typed there is text, not a pick", got)
	}
}

func TestASecretNeverReachesTheScreen(t *testing.T) {
	const key1 = "tskey-auth-kQ3xSECRET"
	b := secretBlock()
	typeIn(b, key1)
	if v := b.View(plain, 80, 0); strings.Contains(v, key1) || strings.Contains(v, "kQ3x") {
		t.Fatalf("the key is on screen while typed:\n%s", v)
	}
	if v := Strip(b.View(plain, 80, 0)); !strings.Contains(v, strings.Repeat("*", len(key1))) {
		t.Fatalf("the field does not show the key as asterisks:\n%s", v)
	}
	b.Update(press("enter"))
	b.Update(press("enter")) // the node name
	for name, v := range map[string]string{"submit": b.View(plain, 80, 0), "summary": b.Summary(plain, 80)} {
		if strings.Contains(v, "kQ3x") {
			t.Errorf("the %s shows the key:\n%s", name, v)
		}
		if !strings.Contains(strings.Join(strings.Fields(Strip(v)), " "), SecretShown) {
			t.Errorf("the %s does not say the key is set:\n%s", name, v)
		}
	}
	if q, a, _ := b.Get("key"); q.Value(a) != key1 {
		t.Fatal("the answer lost the key it was given")
	}
}

func TestACheckRefusesAndKeepsTheQuestion(t *testing.T) {
	b := secretBlock()
	typeIn(b, "not-a-key")
	b.Update(press("enter"))
	if b.Current() != 0 {
		t.Fatal("a refused key moved the focus on")
	}
	if v := Strip(b.View(plain, 80, 0)); !strings.Contains(v, "starts with tskey-") {
		t.Fatalf("the refusal is not on screen:\n%s", v)
	}
}

func TestALockedPartStaysChecked(t *testing.T) {
	b := kitBlock()
	b.Update(press("up"))
	b.Update(press("up")) // from the first part that comes off to the first locked one
	b.Update(press("space"))
	q, a, _ := b.Get("kit")
	if !a.Picks[0] {
		t.Fatal("space took a locked part out")
	}
	if v := Strip(b.View(plain, 80, 0)); !strings.Contains(v, "is always part of the install") {
		t.Fatalf("the refusal is not on screen:\n%s", v)
	}
	b.Update(press("down"))
	b.Update(press("down"))
	b.Update(press("space"))
	if _, a, _ = b.Get("kit"); a.Picks[2] {
		t.Fatalf("space did not take %s out", q.Options[2].Label)
	}
}

func TestABlockOfOneQuestionIsSubmittedByItsAnswer(t *testing.T) {
	if got := kitBlock().Update(press("enter")); got != Submitted {
		t.Fatalf("Enter on a lone question: %v, want Submitted", got)
	}
}

func TestSkipAnswersWithTheLockedOnly(t *testing.T) {
	b := NewBlock("Map", Question{
		ID: "projects", Kind: Multi, Prompt: "Which projects?",
		Options: []Option{{Label: "/srv/proj/a", On: true}, {Label: "/srv/proj/b", On: true}},
		Own:     "Type a path.", Skip: "Skip — add projects later",
	})
	b.Update(press("down"))
	b.Update(press("down"))
	b.Update(press("down"))
	if got := b.Update(press("enter")); got != Submitted {
		t.Fatalf("Enter on Skip: %v", got)
	}
	if got := b.Value("projects"); got != "" {
		t.Fatalf("Skip answered %q, want nothing", got)
	}
}

func TestAPathOfOneOwnJoinsTheList(t *testing.T) {
	b := machineBlock()
	b.Update(press("enter"))
	b.Update(press("enter"))
	b.Update(press("down"))
	b.Update(press("down")) // the own line of Projects
	typeIn(b, "/srv/more")
	b.Update(press("enter"))
	if got := b.Value("roots"); got != "/srv/proj,/srv/more" {
		t.Fatalf("the list is %q after a path of one's own", got)
	}
}

func TestHiddenQuestionsLeaveTheirTabs(t *testing.T) {
	b := machineBlock()
	b.Refresh = func(b *Block) { b.Find("roots").Hidden = b.Value("host") == "lab" }
	b.Update(press("enter"))
	b.Update(press("enter"))
	if got := b.Questions[b.Current()].ID; got != "state" {
		t.Fatalf("after the hiding answer the focus is on %s, want state", got)
	}
	if v := Strip(b.View(plain, 80, 0)); strings.Contains(v, "Projects") {
		t.Fatalf("a hidden question keeps its tab:\n%s", v)
	}
}

var colour = regexp.MustCompile(`\x1b\[[0-9;]*(3[0-9]|9[0-7]|4[0-9]|10[0-7])(;[0-9;]*)?m`)

func TestNoColourMeansNoColour(t *testing.T) {
	views := map[string]string{
		"block": machineBlock().View(plain, 80, 0),
		"kit":   kitBlock().View(plain, 80, 0),
		"root":  rootView(80),
		"feed":  collapsedView(80),
	}
	for name, v := range views {
		if colour.MatchString(v) {
			t.Errorf("%s carries a colour with colour off: %q", name, v)
		}
	}
	if !colour.MatchString(NewTheme(true).Result(Pass, "ok")) {
		t.Error("the check does not see colour where it is: it proves nothing")
	}
}

func TestNarrowTerminalsGetNoFrame(t *testing.T) {
	v := plain.Frame(Frame{Title: "Plan", Rows: []Row{{Text: "a line"}}}, MinWidth-1)
	if strings.ContainsAny(v, "╭╮╰╯│") {
		t.Fatalf("a frame below %d columns:\n%s", MinWidth, v)
	}
	for _, line := range strings.Split(Strip(rootView(MinWidth)), "\n") {
		if Width(line) > MinWidth {
			t.Errorf("a line wider than %d columns: %q", MinWidth, line)
		}
	}
}

func TestTheLivePartOnlyGrowsBetweenPrints(t *testing.T) {
	var l Live
	five := "1\n2\n3\n4\n5"
	if got := l.View(five); got != five {
		t.Fatalf("the first view changed: %q", got)
	}
	shorter := l.View("1\n2")
	if got := strings.Count(shorter, "\n") + 1; got != 5 {
		t.Fatalf("a shorter view took %d lines between prints, want 5", got)
	}
	if l.Out("entry") == nil {
		t.Fatal("a print sent nothing")
	}
	if got := l.View("1"); got != shorter {
		t.Fatalf("the view moved while the opening line was on its way: %q", got)
	}
	l.Update(opened{})
	if got := l.View("1"); got != "" {
		t.Fatalf("the view is %q while the entry is on its way, want it empty", got)
	}
	l.Update(printed{})
	if got := l.View("1"); got != "1" {
		t.Fatalf("after the print the view is %q, want its own height", got)
	}
}

func TestPrintsWaitForOneAnother(t *testing.T) {
	var l Live
	if l.Out("first") == nil {
		t.Fatal("the first print sent nothing")
	}
	if l.Out("second", tea.Quit) != nil {
		t.Fatal("a second print went out before the first was done")
	}
	l.Update(opened{})
	if _, next := l.Update(printed{}); next == nil {
		t.Fatal("the first print done, the second did not follow")
	}
}
