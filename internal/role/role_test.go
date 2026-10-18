package role

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/mcp"
)

// repoCases is the layout both readers of a repository are checked against:
// the executor here, the collector in agent/test_roles.py.
type repoCases struct {
	Dirs  []string          `json:"dirs"`
	Files map[string]string `json:"files"`
	Repos [][2]string       `json:"repos"`
}

// repoLayout makes the shared layout under a fresh directory and returns it
// with the cases.
func repoLayout(t *testing.T) (string, repoCases) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "agent", "testdata", "role-repos.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c repoCases
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Repos) == 0 || len(c.Dirs) == 0 {
		t.Fatal("the shared cases are empty: the test checks nothing")
	}
	root := t.TempDir()
	for _, dir := range c.Dirs {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for file, body := range c.Files {
		body = strings.ReplaceAll(body, "{root}", root)
		if err := os.WriteFile(filepath.Join(root, file), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, c
}

// A worktree, a directory inside a checkout and the checkout itself work in
// one repository; a directory no checkout holds, or one whose git files do not
// read, is a repository of its own.
func TestTheRepoOfADirectoryIsTheCommonGitDirectory(t *testing.T) {
	root, c := repoLayout(t)
	for _, r := range c.Repos {
		if got, want := Repo(filepath.Join(root, r[0])), filepath.Join(root, r[1]); got != want {
			t.Errorf("%s works in %s, meant %s", r[0], got, want)
		}
	}
	if got := Repo(filepath.Join(root, "lab") + "//./src/../"); got != filepath.Join(root, "lab", ".git") {
		t.Errorf("a directory written another way works in %s", got)
	}
}

// The role of a session is kept by its repository: the session that moved
// from the main checkout into a worktree of it, or came back in another, finds
// the role it had, and the session of the same name in another repository
// does not.
func TestTheRoleKeyIsTheSameForACheckoutAndItsWorktree(t *testing.T) {
	root, _ := repoLayout(t)
	config := t.TempDir()
	at := func(dir string) mcp.Place { return mcp.Place{ConfigDir: config, Dir: filepath.Join(root, dir)} }
	lab := File(at("lab"), "lead")
	for _, dir := range []string{"worktrees/lab-fix", "worktrees/lab-abs", "lab/src/deep"} {
		if got := File(at(dir), "lead"); got != lab {
			t.Errorf("the role of lead in %s is %s, in the checkout %s", dir, got, lab)
		}
	}
	if File(at("shop"), "lead") == lab {
		t.Error("two repositories keep the role of lead in one file")
	}
	if File(at("lab"), "lead-db") == lab {
		t.Error("two sessions of one repository keep their roles in one file")
	}
	if File(mcp.Place{ConfigDir: t.TempDir(), Dir: filepath.Join(root, "lab")}, "lead") == lab {
		t.Error("two accounts keep the role of lead in one file")
	}
	if File(mcp.Place{ConfigDir: "relative", Dir: root}, "lead") != "" {
		t.Error("a place that is not one has a file")
	}

	dir := t.TempDir()
	if _, err := Keep(dir, at("lab"), "lead", "the lead of the split", BySession, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := Read(dir, at("worktrees/lab-fix"), "lead"); got == nil || got.Role != "the lead of the split" {
		t.Errorf("the session in the worktree reads %+v", got)
	}
	if got := Read(dir, at("shop"), "lead"); got != nil {
		t.Errorf("the session of another repository reads %+v", got)
	}
}

// A role is kept whole and trimmed, in a file only the owner reads, and an
// empty one is a role cleared. One longer than a role is refused with why,
// and the role kept before stays.
func TestARoleIsSetReadAndCleared(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "roles")
	place := mcp.Place{ConfigDir: "/srv/claude/", Dir: t.TempDir()}
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.FixedZone("x", 3600))

	if got := Read(dir, place, "lab"); got != nil {
		t.Fatalf("a session with no role reads %+v", got)
	}
	whole := strings.Repeat("é", MaxText)
	kept, err := Keep(dir, place, "lab", "  \n"+whole+"\t ", ByOpener("lead"), now)
	if err != nil {
		t.Fatal(err)
	}
	if kept.Role != whole || kept.By != "opener lead" || kept.At != "2026-03-04T04:06:07Z" || kept.ConfigDir != "/srv/claude" ||
		kept.Name != "lab" || kept.Dir != filepath.Clean(place.Dir) || kept.Repo != filepath.Clean(place.Dir) {
		t.Errorf("kept %+v", kept)
	}
	if got := Read(dir, place, "lab"); got == nil || *got != *kept {
		t.Errorf("read back %+v", got)
	}
	info, err := os.Stat(filepath.Join(dir, File(place, "lab")))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("the file is %v (%v)", info, err)
	}
	if info, err := os.Stat(dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("the directory is %v (%v)", info, err)
	}

	if _, err := Keep(dir, place, "lab", whole+"é", BySession, now); err == nil || !strings.Contains(err.Error(), "at most 500") {
		t.Errorf("a role of %d characters: %v", MaxText+1, err)
	}
	if got := Read(dir, place, "lab"); got == nil || got.Role != whole {
		t.Errorf("a refused role took the place of the kept one: %+v", got)
	}

	if got, err := Keep(dir, place, "lab", " \n ", ByPerson, now); err != nil || got != nil {
		t.Fatalf("clearing gave %+v, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, File(place, "lab"))); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the cleared role is still on disk: %v", err)
	}
	if got, err := Keep(dir, place, "lab", "", ByPerson, now); err != nil || got != nil {
		t.Errorf("clearing what is not there: %+v, %v", got, err)
	}
	if _, err := Keep(dir, mcp.Place{Dir: "/srv"}, "lab", "x", ByPerson, now); err == nil {
		t.Error("a role was kept for a place that is not one")
	}
}

// A file that names another session is not its role, whatever its name.
func TestAFileIsTheRoleOnlyOfTheSessionItNames(t *testing.T) {
	dir := t.TempDir()
	place := mcp.Place{ConfigDir: "/srv/claude", Dir: t.TempDir()}
	for _, other := range []Role{
		{ConfigDir: "/srv/other", Repo: place.Dir, Name: "lab", Role: "x"},
		{ConfigDir: "/srv/claude", Repo: "/srv/elsewhere", Name: "lab", Role: "x"},
		{ConfigDir: "/srv/claude", Repo: place.Dir, Name: "lab-review", Role: "x"},
		{ConfigDir: "/srv/claude", Repo: place.Dir, Name: "lab", Role: ""},
	} {
		body, _ := json.Marshal(other)
		if err := os.WriteFile(filepath.Join(dir, File(place, "lab")), body, 0o600); err != nil {
			t.Fatal(err)
		}
		if got := Read(dir, place, "lab"); got != nil {
			t.Errorf("%+v was read as the role of lab", other)
		}
	}
}

// A session renamed by the panel takes its role to the new name, and the old
// name has none; a session without a role moves nothing.
func TestARenamedSessionTakesItsRole(t *testing.T) {
	dir := t.TempDir()
	place := mcp.Place{ConfigDir: "/srv/claude", Dir: t.TempDir()}
	if err := Move(dir, place, "lab", "lab-db"); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("a session with no role left %v", entries)
	}
	kept, err := Keep(dir, place, "lab", "the db split", ByPerson, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := Move(dir, place, "lab", "lab-db"); err != nil {
		t.Fatal(err)
	}
	got := Read(dir, place, "lab-db")
	if got == nil || got.Role != kept.Role || got.By != ByPerson || got.At != kept.At || got.Name != "lab-db" {
		t.Errorf("the renamed session reads %+v", got)
	}
	if was := Read(dir, place, "lab"); was != nil {
		t.Errorf("the old name kept %+v", was)
	}
}

// The session is told its role at the handshake, with who set it and what the
// role is for; a session without one is told nothing of a role.
func TestTheSessionIsToldItsRoleAtTheHandshake(t *testing.T) {
	dir := t.TempDir()
	place := mcp.Place{ConfigDir: "/srv/claude", Dir: t.TempDir()}
	tool := Tool(dir, time.Now)
	b := mcp.Binding{Place: place, Name: "lab", PID: 42}
	if said := tool.Standing(b); said != "" {
		t.Errorf("a session with no role is told %q", said)
	}
	for by, who := range map[string]string{
		ByPerson: "set by the person", BySession: "set by itself", ByOpener("lead"): "set by its opener lead",
		ByOpener(""): "set by its opener:",
	} {
		if _, err := Keep(dir, place, "lab", "the db split", by, time.Now()); err != nil {
			t.Fatal(err)
		}
		said := tool.Standing(b)
		if !strings.Contains(said, who) || !strings.Contains(said, "“the db split”") ||
			!strings.Contains(said, "may be theirs") {
			t.Errorf("set %s, the session is told %q", by, said)
		}
	}
	if said := tool.Standing(mcp.Binding{Place: place, Name: "lab-review"}); said != "" {
		t.Errorf("another session of the directory is told %q", said)
	}
}

// The tool reads the role without one, sets it with a text — the session's
// own — and clears it with an empty one; a role too long comes back as an
// error the model reads.
func TestTheToolReadsSetsAndClearsTheRole(t *testing.T) {
	dir := t.TempDir()
	b := mcp.Binding{Place: mcp.Place{ConfigDir: "/srv/claude", Dir: t.TempDir()}, Name: "lab"}
	tool := Tool(dir, func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) })
	bind := func() (mcp.Binding, error) { return b, nil }
	call := func(args string) (string, bool) { return tool.Call(context.Background(), bind, json.RawMessage(args)) }

	if said, isErr := call(`{}`); isErr || said != "This session has no role." {
		t.Errorf("read with none: %q %v", said, isErr)
	}
	if said, isErr := call(`{"role":"the db split"}`); isErr || !strings.Contains(said, "kept") {
		t.Errorf("set: %q %v", said, isErr)
	}
	if got := Read(dir, b.Place, "lab"); got == nil || got.By != BySession {
		t.Errorf("the role set by the tool is %+v", got)
	}
	if said, isErr := call(`{}`); isErr || said != "This session's role, set by itself at 2026-01-02T03:04:05Z: “the db split”" {
		t.Errorf("read: %q %v", said, isErr)
	}
	if said, isErr := call(`{"role":"` + strings.Repeat("a", MaxText+1) + `"}`); !isErr || !strings.Contains(said, "at most 500") {
		t.Errorf("too long: %q %v", said, isErr)
	}
	if said, isErr := call(`{"role":""}`); isErr || said != "The role is cleared." {
		t.Errorf("clear: %q %v", said, isErr)
	}
	if Read(dir, b.Place, "lab") != nil {
		t.Error("the cleared role is still read")
	}
	lost := func() (mcp.Binding, error) { return mcp.Binding{}, errors.New("no file of the session") }
	if said, isErr := tool.Call(context.Background(), lost, json.RawMessage(`{"role":"x"}`)); !isErr ||
		!strings.Contains(said, "no file of the session") {
		t.Errorf("an unknown place: %q %v", said, isErr)
	}
	if !tool.Allowed || tool.Name != "role" {
		t.Errorf("the tool is %q, allowed %v", tool.Name, tool.Allowed)
	}
}
