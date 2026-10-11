package guards

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"aacpanel/internal/action"
)

// The fixture holds the file format both the writer and the hooks rely on.
func TestTheSharedCasesOfThePlaces(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "deploy", "claude", "testdata", "guards.json"))
	if err != nil {
		t.Fatal(err)
	}
	var shared struct {
		Cases []struct {
			What string             `json:"what"`
			File *string            `json:"file"`
			Dirs map[string]*[2]any `json:"dirs"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &shared); err != nil {
		t.Fatal(err)
	}
	if len(shared.Cases) == 0 {
		t.Fatal("the shared cases are empty: the test checks nothing")
	}
	for _, c := range shared.Cases {
		t.Run(c.What, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			if c.File != nil {
				if err := os.MkdirAll(Home(), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(Path(), []byte(*c.File), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			for dir, want := range c.Dirs {
				g, ok := Of(dir)
				switch {
				case want == nil && ok:
					t.Errorf("%q is held by %+v, meant by nothing", dir, g)
				case want != nil && (!ok || float64(g.Cap) != want[0].(float64) || g.Restart != want[1].(bool)):
					t.Errorf("%q is held by %+v (%v), meant %v", dir, g, ok, *want)
				}
			}
		})
	}
}

// What the executor writes is what the hooks read: a line a place, sorted,
// and the closest place of each directory comes back as it went.
func TestTheFileReadsBackAsItWasWritten(t *testing.T) {
	list := []action.Guard{
		{Path: "/srv/proj/Side/service/aacpanel", Cap: 80, Restart: true},
		{Path: "/srv/proj", Cap: 70},
	}
	body := Lines(list)
	if want := "/srv/proj\t70\t0\n/srv/proj/Side/service/aacpanel\t80\t1\n"; body != want {
		t.Errorf("the file reads %q, meant %q", body, want)
	}
	for dir, want := range map[string]action.Guard{
		"/srv/proj/Side/service/aacpanel/web": list[0],
		"/srv/proj/Side":                      list[1],
	} {
		if g, ok := Closest(body, dir); !ok || g != want {
			t.Errorf("%s reads back as %+v (%v), meant %+v", dir, g, ok, want)
		}
	}
	if Lines(nil) != "" {
		t.Error("no place is not an empty line")
	}
}
