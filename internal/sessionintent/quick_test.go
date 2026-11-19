package sessionintent

import (
	"strings"
	"testing"

	"aacpanel/internal/mcp"
)

func TestQuickPhrasesNormalizeAndRespectTheirLimits(t *testing.T) {
	long := strings.Repeat("🙂", QuickPhraseMax)
	for _, c := range []struct {
		name, text, want, problem string
	}{
		{"default", DefaultQuick, "AskUserQuestion\nGo on\nTidy up the checklist\nStatus\nThat's all for today", ""},
		{"blank", " \n\t\r\n ", "", ""},
		{"trim and skip", " AskUserQuestion \r\n\r\n Go on \n\t Status\t\n", "AskUserQuestion\nGo on\nStatus", ""},
		{"five full phrases", strings.Repeat(long+"\n", QuickMax), strings.TrimSuffix(strings.Repeat(long+"\n", QuickMax), "\n"), ""},
		{"too many", strings.Repeat("go\n", QuickMax+1), "", "more than 5"},
		{"too long", long + "🙂", "", "longer than 200"},
		{"control", "go\a", "", "control character"},
		{"bare carriage return", "go\rstatus", "", "control character"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := NormalizeQuick(c.text)
			if c.problem != "" {
				if err == nil || !strings.Contains(err.Error(), c.problem) {
					t.Fatalf("got %q, %v; want %s", got, err, c.problem)
				}
			} else if err != nil || got != c.want {
				t.Fatalf("got %q, %v; want %q", got, err, c.want)
			}
		})
	}
}

func TestQuickPhrasesStayWithTheSessionAndClearIndependently(t *testing.T) {
	dir := t.TempDir()
	p := mcp.Place{ConfigDir: t.TempDir(), Dir: t.TempDir()}
	for _, change := range []Values{{First: "Queue", Restart: "Continue"}, {Quick: " Go on \n\n Status "}} {
		if err := Keep(dir, p, "lead", change); err != nil {
			t.Fatal(err)
		}
	}
	if err := Move(dir, p, "lead", "pilot"); err != nil {
		t.Fatal(err)
	}
	got, err := Read(dir, p, "pilot")
	if err != nil || got[Quick] != "Go on\nStatus" || got[First] != "Queue" || got[Restart] != "Continue" {
		t.Fatalf("renamed values: %v, %v", got, err)
	}
	if err := Keep(dir, p, "pilot", Values{Quick: strings.Repeat("go\n", 6)}); err == nil {
		t.Fatal("an invalid quick list was saved")
	}
	if err := Keep(dir, p, "pilot", Values{Quick: " \n\t"}); err != nil {
		t.Fatal(err)
	}
	got, err = Read(dir, p, "pilot")
	if err != nil || len(got) != 2 || got.Pick(Quick, "Project") != "Project" || got[First] != "Queue" || got[Restart] != "Continue" {
		t.Fatalf("cleared values: %v, %v", got, err)
	}
}
