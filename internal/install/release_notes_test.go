package install

import (
	"slices"
	"strings"
	"testing"
)

// The notes of a release are made by deploy/release-notes.sh in a
// repository of releases made by make release: the kind of the release
// against the one before it, the subjects of the commits between them with
// the commits of the releases left out, and how an install gets there.
func TestTheNotesOfAReleaseSayItsKindItsChangesAndTheUpdate(t *testing.T) {
	r := newReleaseRepo(t)
	release := func(version string) {
		t.Helper()
		if out := r.release(t, nil, "VERSION="+version); out.status != 0 {
			t.Fatalf("make release %s: %s%s", version, out.stdout, out.stderr)
		}
	}
	notes := func(args ...string) string {
		t.Helper()
		out := r.notes(t, args...)
		if out.status != 0 {
			t.Fatalf("the notes of %v ended with %d:\n%s%s", args, out.status, out.stdout, out.stderr)
		}
		return out.stdout
	}

	release("1.0.0")
	r.commit(t, "Fix the dark theme")
	release("1.0.1")
	r.commit(t, "Add the rail")
	r.commit(t, "Move the tables")

	// A release not made yet is read off HEAD, through make as well.
	preview := run(t, r.work, r.env(), "", "make", "--no-print-directory", "release-notes", "VERSION=1.1.0")
	if preview.status != 0 {
		t.Fatalf("make release-notes ended with %d:\n%s%s", preview.status, preview.stdout, preview.stderr)
	}
	release("1.1.0")
	r.commit(t, "A new approach")

	if got, want := notes("v1.0.1"), "A minor release, after v1.0.0: fixes.\n\n"+
		"## Changes\n\n- Fix the dark theme\n\n"+
		"## Update\n\nIn the clone of the panel:\n\n    ./install.sh update\n\n"+
		"A clone on a release moves to the newest release of its paradigm, and a clone on a branch pulls it.\n"; got != want {
		t.Errorf("the notes of a minor release read:\n%s\nwant:\n%s", got, want)
	}

	major := notes("v1.1.0")
	if preview.stdout != major {
		t.Errorf("the notes read before the release:\n%s\ndiffer from those of its tag:\n%s", preview.stdout, major)
	}
	if !strings.HasPrefix(major, "A major release, after v1.0.1: features and migrations.\n") ||
		!strings.Contains(major, "\n- Add the rail\n- Move the tables\n\n") || !strings.Contains(major, "    ./install.sh update\n") {
		t.Errorf("the notes of a major release read:\n%s", major)
	}

	first := notes("v1.0.0")
	if !strings.HasPrefix(first, "The first release of the panel.\n") || !strings.Contains(first, "\n- start\n") {
		t.Errorf("the notes of the first release read:\n%s", first)
	}

	paradigm := notes("v2.0.0")
	if !strings.HasPrefix(paradigm, "A paradigm release, after v1.1.0: a change of approach.\n") ||
		!strings.Contains(paradigm, "\n- A new approach\n") ||
		!strings.Contains(paradigm, "does not take v2.0.0") || !strings.Contains(paradigm, "    ./install.sh update --to 2\n") {
		t.Errorf("the notes of a paradigm release read:\n%s", paradigm)
	}

	named := notes("v1.1.0", "v1.0.0")
	if !strings.HasPrefix(named, "A major release, after v1.0.0") ||
		!strings.Contains(named, "\n- Fix the dark theme\n- Add the rail\n- Move the tables\n\n") {
		t.Errorf("the notes against a release named read:\n%s", named)
	}

	for _, n := range []string{first, major, paradigm, named, notes("v1.0.1")} {
		if strings.Contains(n, "Release v") {
			t.Errorf("the notes carry the commit of a release:\n%s", n)
		}
	}
}

// The release before is found by number, not by the order of the tags, and a
// tag that is not a release is passed by.
func TestTheNotesFindTheReleaseBeforeByNumber(t *testing.T) {
	r := newReleaseRepo(t)
	r.commit(t, "Two")
	r.git(t, "tag", "v1.2.0")
	r.commit(t, "Nine")
	r.git(t, "tag", "v1.9.0")
	r.commit(t, "Candidate")
	r.git(t, "tag", "v1.9.9-rc1")
	r.git(t, "tag", "nightly")
	r.commit(t, "Ten")

	// As text v1.10.0 sorts before both releases; v1.9.9-rc1 is a candidate
	// that never came out, between them by number.
	out := r.notes(t, "v1.10.0")
	if out.status != 0 || !strings.HasPrefix(out.stdout, "A major release, after v1.9.0: features and migrations.\n") ||
		!strings.Contains(out.stdout, "\n- Candidate\n- Ten\n\n") || strings.Contains(out.stdout, "- Nine") {
		t.Errorf("the notes of v1.10.0 ended with %d:\n%s%s", out.status, out.stdout, out.stderr)
	}
}

func TestTheNotesRefuseWhatIsNoRelease(t *testing.T) {
	r := newReleaseRepo(t)
	for _, c := range []struct {
		args []string
		stop string
	}{
		{nil, "stop: name the release: deploy/release-notes.sh vP.M.m, like v1.4.0"},
		{[]string{"1.2.0"}, "stop: 1.2.0 is not a release: the notes are of a tag vP.M.m, like deploy/release-notes.sh v1.4.0"},
		{[]string{"v1.2.0", "v1.1.0"}, "stop: v1.1.0 is not a release tag here: name the release before v1.2.0, like v1.3.0, or leave it out"},
	} {
		out := r.notes(t, c.args...)
		if out.status == 0 || out.stdout != "" || !slices.Contains(strings.Split(out.stderr, "\n"), c.stop) {
			t.Errorf("the notes of %v ended with %d:\n%s%s\nwant the line:\n%s", c.args, out.status, out.stdout, out.stderr, c.stop)
		}
	}
}
