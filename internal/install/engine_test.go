package install

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestEveryKindHasAnUndo reads the package for every constant of type Kind:
// a kind declared and not given an undo would be a change that uninstall
// cannot take back.
func TestEveryKindHasAnUndo(t *testing.T) {
	pkgs, err := parser.ParseDir(token.NewFileSet(), ".", func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.CONST {
					continue
				}
				for _, spec := range gen.Specs {
					vs := spec.(*ast.ValueSpec)
					if id, ok := vs.Type.(*ast.Ident); !ok || id.Name != "Kind" {
						continue
					}
					for i, name := range vs.Names {
						lit := vs.Values[i].(*ast.BasicLit)
						kinds = append(kinds, name.Name+"="+lit.Value)
						if undos[Kind(strings.Trim(lit.Value, `"`))] == nil {
							t.Errorf("the kind %s has no undo", name.Name)
						}
					}
				}
			}
		}
	}
	if len(kinds) == 0 {
		t.Fatal("no constant of type Kind was found: the walk reads nothing")
	}
	if len(kinds) != len(undos) {
		t.Errorf("kinds %q against %d undos: an undo without its kind", kinds, len(undos))
	}
}

// stepWith is a step that makes a directory, the way a real step records
// its change.
func stepWith(path string) *Step {
	return &Step{ID: "test", Title: "Make a directory",
		Apply: func(r *Run) error { return r.MakeDir(path, 0o755) },
		Undo:  UndoKind,
	}
}

func manifestIn(t *testing.T) (*Manifest, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "aacpanel-install")
	m, err := OpenManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	return m, dir
}

func lines(t *testing.T, m *Manifest) []Entry {
	t.Helper()
	es, err := ReadManifest(m.Path)
	if err != nil {
		t.Fatal(err)
	}
	return es
}

func TestTheManifestBeginsWithItsOwnDirectory(t *testing.T) {
	m, dir := manifestIn(t)
	want := []Entry{{Step: "manifest", Kind: "dir", Target: dir, Meta: "created"}}
	if got := lines(t, m); !slices.Equal(got, want) {
		t.Errorf("a new manifest holds %v, want %v", got, want)
	}
	raw, _ := os.ReadFile(m.Path)
	if string(raw) != "manifest\tdir\t"+dir+"\tcreated\n" {
		t.Errorf("the line on disk is %q", raw)
	}
	again, err := OpenManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := lines(t, again); len(got) != 1 {
		t.Errorf("opening the manifest again added a line: %v", got)
	}
}

func TestALineIsWrittenBeforeItsChange(t *testing.T) {
	m, dir := manifestIn(t)
	// The parent of the directory is not there: the change fails, and the
	// line written for it must be there all the same.
	target := filepath.Join(dir, "no-parent", "sub")
	r := &Run{Manifest: m}
	if err := r.Do(stepWith(target)); err == nil {
		t.Fatal("making a directory under a file went through")
	}
	got := lines(t, m)
	if want := (Entry{Step: "test", Kind: "dir", Target: target, Meta: "created"}); len(got) != 2 || got[1] != want {
		t.Fatalf("after a change that failed the manifest holds %v, want its line %v", got, want)
	}
	if err := r.Undo(stepWith(target), got[1]); err != nil {
		t.Errorf("the undo of a change that never happened failed: %v", err)
	}
}

func TestAChangeThatWentThroughIsInTheManifestAndInTheRun(t *testing.T) {
	m, dir := manifestIn(t)
	target := filepath.Join(dir, "made")
	var changed []string
	r := &Run{Manifest: m, Sink: func(e Event) {
		if e.Type == Changed {
			changed = append(changed, e.Text)
		}
	}}
	if err := r.Do(stepWith(target)); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(target); err != nil || !st.IsDir() {
		t.Fatalf("the directory was not made: %v", err)
	}
	if !slices.Equal(changed, []string{target}) || len(r.ChangedInRun()) != 1 {
		t.Errorf("the sink heard of %q, the run holds %v", changed, r.ChangedInRun())
	}
	// A directory that is there already is not the installer's.
	r2 := &Run{Manifest: m}
	if err := r2.Do(stepWith(target)); err != nil {
		t.Fatal(err)
	}
	if got := lines(t, m); len(got) != 2 {
		t.Errorf("a directory already there was recorded again: %v", got)
	}
	if err := r2.Undo(stepWith(target), lines(t, m)[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("undo left the directory: %v", err)
	}
}

func TestNothingIsRecordedThatNothingTakesBack(t *testing.T) {
	m, dir := manifestIn(t)
	target := filepath.Join(dir, "never")
	noUndo := stepWith(target)
	noUndo.Undo = nil
	if err := (&Run{Manifest: m}).Do(noUndo); err == nil || !strings.Contains(err.Error(), "no undo") {
		t.Errorf("a step without an undo recorded a change: %v", err)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Error("the change was made without its line")
	}
	unknown := &Step{ID: "test", Undo: UndoKind, Apply: func(r *Run) error { return r.Record("rocket", "/x", "") }}
	if err := (&Run{Manifest: m}).Do(unknown); err == nil {
		t.Error("a kind nobody knows was recorded")
	}
	if err := (&Run{}).Do(stepWith(target)); !errors.Is(err, ErrReadOnly) {
		t.Errorf("a run without a manifest made a change: %v", err)
	}
	if got := lines(t, m); len(got) != 1 {
		t.Errorf("the manifest took %v", got)
	}
}

func TestTheUndoOfADirectoryLeavesWhatIsNotItsOwn(t *testing.T) {
	dir := t.TempDir()
	full := filepath.Join(dir, "full")
	if err := os.MkdirAll(filepath.Join(full, "theirs"), 0o755); err != nil {
		t.Fatal(err)
	}
	var said []string
	r := &Run{Sink: func(e Event) { said = append(said, e.Text) }}
	s := &Step{ID: "test", Undo: UndoKind}
	for _, target := range []string{full, filepath.Join(dir, "gone")} {
		if err := r.Undo(s, Entry{Kind: "dir", Target: target, Meta: "created"}); err != nil {
			t.Errorf("%s: %v", target, err)
		}
	}
	if _, err := os.Stat(full); err != nil {
		t.Error("a directory with something in it was removed")
	}
	if len(said) != 1 || !strings.Contains(said[0], "is left") {
		t.Errorf("said %q", said)
	}
}

func TestDoFollowsTheContract(t *testing.T) {
	var calls []string
	s := &Step{ID: "s", Title: "A step",
		Done:   func(*Run) (bool, error) { calls = append(calls, "done"); return true, nil },
		Apply:  func(*Run) error { calls = append(calls, "apply"); return nil },
		Verify: func(*Run) error { calls = append(calls, "verify"); return nil },
	}
	var events []EventType
	r := &Run{Sink: func(e Event) { events = append(events, e.Type) }}
	if err := r.Do(s); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(calls, []string{"done"}) {
		t.Errorf("a step already done ran %q", calls)
	}
	s.Done = func(*Run) (bool, error) { return false, nil }
	calls = nil
	if err := r.Do(s); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(calls, []string{"apply", "verify"}) {
		t.Errorf("a step to do ran %q", calls)
	}
	if !slices.Equal(events, []EventType{Opened, Closed, Opened, Closed}) {
		t.Errorf("events %v", events)
	}
}

func TestTheCheckIsAStepThatSaysItsLines(t *testing.T) {
	in := Inspection{Findings: []Finding{{Mark: Pass, Text: "fine"}, {Mark: Stop, Text: "stop: no"}}}
	var said []string
	var closed error
	r := &Run{Sink: func(e Event) {
		switch e.Type {
		case Said:
			said = append(said, e.Text)
		case Closed:
			closed = e.Err
		}
	}}
	err := r.Do(Check(in))
	var stopped *Stopped
	if !errors.As(err, &stopped) || stopped.Lines != 1 || closed != err {
		t.Errorf("the check ended with %v, the sink heard %v", err, closed)
	}
	if !slices.Equal(said, []string{"fine", "stop: no"}) {
		t.Errorf("said %q", said)
	}
}

func TestAQuestionWithNobodyToAskNamesItsFlag(t *testing.T) {
	q := Question{Prompt: "Where will you open the panel?", Flag: "--access",
		Values: []string{"local", "tailscale", "lan", "domain"}, Default: "local"}
	_, err := (&Run{}).Answer(q)
	want := `stop: no terminal to ask "Where will you open the panel?"; pass --access local|tailscale|lan|domain or --yes`
	if err == nil || err.Error() != want {
		t.Errorf("a plain run answered with %v, want %q", err, want)
	}
	_, err = (&Run{}).Answer(Question{Prompt: "What does the panel call this machine?", Flag: "--host"})
	if err == nil || !strings.HasSuffix(err.Error(), "pass --host <value> or --yes") {
		t.Errorf("a free value: %v", err)
	}
	for _, c := range []struct {
		run  Run
		want string
	}{
		{Run{Answers: map[string]string{"--access": "lan"}}, "lan"},
		{Run{Yes: true}, "local"},
		{Run{Ask: func(Question) (string, error) { return "tailscale", nil }}, "tailscale"},
	} {
		if got, err := c.run.Answer(q); err != nil || got != c.want {
			t.Errorf("answered %q, %v; want %q", got, err, c.want)
		}
	}
}

func TestTheJournalHoldsNoSecret(t *testing.T) {
	dir := t.TempDir()
	j, err := OpenJournal(dir, "install", time.Date(2026, 9, 29, 14, 2, 7, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "log", "20260929-140207-install.log"); j.Path != want {
		t.Errorf("the journal is %s, want %s", j.Path, want)
	}
	const secret = "tskey-auth-kDEADBEEF"
	j.Hide(secret)
	if err := j.Command([]string{"docker", "compose", "up", "-d"}, "TS_AUTHKEY="+secret+"\nstarted", errors.New("exit status 1")); err != nil {
		t.Fatal(err)
	}
	if err := j.Line("the key %s was used", secret); err != nil {
		t.Fatal(err)
	}
	j.Close()
	raw, _ := os.ReadFile(j.Path)
	if strings.Contains(string(raw), secret) {
		t.Errorf("the journal holds the secret:\n%s", raw)
	}
	want := "$ docker compose up -d\nTS_AUTHKEY=[hidden]\nstarted\n! exit status 1\nthe key [hidden] was used\n"
	if string(raw) != want {
		t.Errorf("the journal reads:\n%s\nwant:\n%s", raw, want)
	}
}

func TestStateDirFollowsXDG(t *testing.T) {
	env := map[string]string{"HOME": "/home/u"}
	if got := StateDir(func(k string) string { return env[k] }); got != "/home/u/.local/state/aacpanel-install" {
		t.Errorf("without XDG_STATE_HOME: %s", got)
	}
	env["XDG_STATE_HOME"] = "/srv/state"
	if got := StateDir(func(k string) string { return env[k] }); got != "/srv/state/aacpanel-install" {
		t.Errorf("with XDG_STATE_HOME: %s", got)
	}
}

// TestARunThatStoppedGoesOnFromTheStepItStoppedAt: there is no file of
// progress. The steps before the stop find the machine done and pass by;
// the step that failed runs again; a stop asked for ends the run after the
// step at work.
func TestARunThatStoppedGoesOnFromTheStepItStoppedAt(t *testing.T) {
	m, dir := manifestIn(t)
	made := filepath.Join(dir, "made")
	broken := true
	var ran []string
	steps := []*Step{
		{ID: "one", Title: "One", Undo: UndoKind,
			Done:  func(*Run) (bool, error) { _, err := os.Stat(made); return err == nil, nil },
			Apply: func(r *Run) error { ran = append(ran, "one"); return r.MakeDir(made, 0o755) }},
		{ID: "two", Title: "Two", Apply: func(*Run) error {
			ran = append(ran, "two")
			if broken {
				return &Failed{Diagnosis: "the stack did not come up"}
			}
			return nil
		}},
		{ID: "three", Title: "Three", Apply: func(*Run) error { ran = append(ran, "three"); return nil }},
	}
	var already []string
	sink := func(e Event) {
		if e.Type == Closed && e.Already {
			already = append(already, e.Title)
		}
	}
	err := Perform(&Run{Manifest: m, Sink: sink}, steps)
	var f *Failed
	if !errors.As(err, &f) || !slices.Equal(ran, []string{"one", "two"}) {
		t.Fatalf("the first run ended with %v after %q", err, ran)
	}
	broken, ran = false, nil
	if err := Perform(&Run{Manifest: m, Sink: sink}, steps); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ran, []string{"two", "three"}) || !slices.Equal(already, []string{"One"}) {
		t.Errorf("the second run ran %q and passed by %q", ran, already)
	}
	ran = nil
	r := &Run{Manifest: m}
	steps[1].Apply = func(*Run) error { ran = append(ran, "two"); r.Stop(); return nil }
	var stopped *Interrupted
	if err := Perform(r, steps); !errors.As(err, &stopped) || stopped.After != "Two" || !slices.Equal(ran, []string{"two"}) {
		t.Errorf("a stop during Two ended with %v after %q", err, ran)
	}
}
