package check

import (
	"os"
	"reflect"
	"sort"
	"testing"
)

// A codex thread reads by the name the panel gave it. One without such a name
// reads by the session of its project, as the claude session beside it does —
// not by its address, and not by a title codex made up, which the executor
// leaves out — and a thread no project holds reads by its address. The
// address stays the session's: the phone opens the conversation by it, and
// the desktop row tells it on hover wherever the name read is another.
func TestACodexThreadReadsByThePanelsNameOrItsProjects(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	type row struct {
		Name    string `json:"name"`
		Tip     string `json:"tip"`
		Address string `json:"address"`
	}
	var got struct {
		Shown map[string]string `json:"shown"`
		Desk  []row             `json:"desk"`
		Phone []row             `json:"phone"`
		Head  string            `json:"head"`
	}
	runWideFixture(t, "codexname.html", &got)

	// The lists keep the order of the project map; what is checked is what
	// each row reads.
	inOrder := func(rows []row) []row {
		out := append([]row(nil), rows...)
		sort.Slice(out, func(i, j int) bool {
			a, b := out[i], out[j]
			if a.Name != b.Name {
				return a.Name < b.Name
			}
			return a.Tip+a.Address < b.Tip+b.Address
		})
		return out
	}

	shown := map[string]string{"admin": "admin", "codex-0000aaaa": "admin", "codex-0000bbbb": "login-bug",
		"codex-0000cccc": "codex-0000cccc"}
	if !reflect.DeepEqual(got.Shown, shown) {
		t.Errorf("the sessions read as %v, expected %v", got.Shown, shown)
	}
	desk := []row{{Name: "admin"}, {Name: "admin", Tip: "codex-0000aaaa"}, {Name: "codex-0000cccc"},
		{Name: "login-bug", Tip: "codex-0000bbbb"}}
	if got := inOrder(got.Desk); !reflect.DeepEqual(got, desk) {
		t.Errorf("the desktop rows read %+v, expected %+v", got, desk)
	}
	phone := []row{{Name: "admin", Address: "admin"}, {Name: "admin", Address: "codex-0000aaaa"},
		{Name: "codex-0000cccc", Address: "codex-0000cccc"}, {Name: "login-bug", Address: "codex-0000bbbb"}}
	if got := inOrder(got.Phone); !reflect.DeepEqual(got, phone) {
		t.Errorf("the phone rows read %+v, expected %+v", got, phone)
	}
	if got.Head != "admin" {
		t.Errorf("the conversation of a thread the panel did not name is headed %q, expected the session of its project",
			got.Head)
	}
}
