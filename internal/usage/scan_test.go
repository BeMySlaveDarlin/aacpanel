package usage

import (
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"aacpanel/internal/store"
)

func at(hour int) string { return fmt.Sprintf("2026-09-10T%02d:00:00.000Z", hour) }

func head(s string) string { return hex.EncodeToString([]byte(s)) }

func TestUnchangedFileIsNotRead(t *testing.T) {
	files := []File{{Path: "/p/a.jsonl", Contour: "home", Inode: 7, Size: 900, Head: head("head"), First: at(10)}}
	points := map[string]store.UsageScanPoint{
		"/p/a.jsonl": {Path: "/p/a.jsonl", Inode: 7, Size: 900, Offset: 600, HeadSum: []byte("head")},
	}
	p := buildPlan(files, points)
	if len(p.tasks) != 0 {
		t.Fatalf("a task for an unchanged file: %+v", p.tasks)
	}
	if p.skipped != 1 {
		t.Errorf("%d skipped, expected one", p.skipped)
	}
}

func TestGrownFileIsReadFromTheBoundary(t *testing.T) {
	files := []File{{Path: "/p/a.jsonl", Contour: "home", Inode: 7, Size: 1500, Head: head("head"), First: at(10)}}
	points := map[string]store.UsageScanPoint{
		"/p/a.jsonl": {Path: "/p/a.jsonl", Inode: 7, Size: 900, Offset: 600, HeadSum: []byte("head")},
	}
	p := buildPlan(files, points)
	if len(p.tasks) != 1 || p.tasks[0].Offset != 600 {
		t.Fatalf("the task is %+v, expected reading on from 600", p.tasks)
	}
	if p.read["/p/a.jsonl"] != 900 {
		t.Errorf("%d bytes to read, expected 900 — the tail, not the whole file", p.read["/p/a.jsonl"])
	}
}

func TestMovedFileIsRecognisedByItsHead(t *testing.T) {
	files := []File{{Path: "/p/a.jsonl", Contour: "home", Inode: 999, Size: 900, Head: head("head"), First: at(10)}}
	points := map[string]store.UsageScanPoint{
		"/p/a.jsonl": {Path: "/p/a.jsonl", Inode: 7, Size: 900, Offset: 600, HeadSum: []byte("head")},
	}
	p := buildPlan(files, points)
	if len(p.tasks) != 1 {
		t.Fatalf("the task is %+v, expected a single file", p.tasks)
	}
	if p.tasks[0].Offset != 600 {
		t.Fatalf("offset %d, expected 600: same head, same file", p.tasks[0].Offset)
	}
}

func TestRewrittenFileIsReadWhole(t *testing.T) {
	files := []File{{Path: "/p/a.jsonl", Contour: "home", Inode: 7, Size: 2000, Head: head("another"), First: at(10)}}
	points := map[string]store.UsageScanPoint{
		"/p/a.jsonl": {Path: "/p/a.jsonl", Inode: 7, Size: 900, Offset: 600, HeadSum: []byte("head")},
	}
	p := buildPlan(files, points)
	if len(p.tasks) != 1 || p.tasks[0].Offset != 0 {
		t.Fatalf("the task is %+v, expected a reread from zero", p.tasks)
	}
}

func TestShrunkFileIsReadWhole(t *testing.T) {
	files := []File{{Path: "/p/a.jsonl", Contour: "home", Inode: 7, Size: 400, Head: head("head"), First: at(10)}}
	points := map[string]store.UsageScanPoint{
		"/p/a.jsonl": {Path: "/p/a.jsonl", Inode: 7, Size: 900, Offset: 600, HeadSum: []byte("head")},
	}
	p := buildPlan(files, points)
	if len(p.tasks) != 1 || p.tasks[0].Offset != 0 {
		t.Fatalf("the task is %+v, expected a reread from zero", p.tasks)
	}
}

func TestMissingFilesAreMarkedOnce(t *testing.T) {
	files := []File{{Path: "/p/a.jsonl", Contour: "home", Inode: 7, Size: 900, Head: head("head"), First: at(10)}}
	points := map[string]store.UsageScanPoint{
		"/p/a.jsonl": {Path: "/p/a.jsonl", Inode: 7, Size: 900, Offset: 900, HeadSum: []byte("head")},
		"/p/b.jsonl": {Path: "/p/b.jsonl", Inode: 8, Size: 100, HeadSum: []byte("b")},
		"/p/c.jsonl": {Path: "/p/c.jsonl", Inode: 9, Size: 100, HeadSum: []byte("c"),
			MissingAt: time.Now().Add(-time.Hour)},
	}
	p := buildPlan(files, points)
	if len(p.missing) != 1 || p.missing[0] != "/p/b.jsonl" {
		t.Fatalf("missing %v, expected only /p/b.jsonl", p.missing)
	}
}

func TestReturnedFileIsTouchedEvenWhenUnchanged(t *testing.T) {
	files := []File{{Path: "/p/a.jsonl", Contour: "home", Inode: 7, Size: 900, Head: head("head"), First: at(10)}}
	points := map[string]store.UsageScanPoint{
		"/p/a.jsonl": {Path: "/p/a.jsonl", Inode: 7, Size: 900, Offset: 900, HeadSum: []byte("head"),
			MissingAt: time.Now().Add(-time.Hour)},
	}
	p := buildPlan(files, points)
	if len(p.tasks) != 1 {
		t.Fatalf("the task is %+v, expected the returned file in it", p.tasks)
	}
	if p.tasks[0].Offset != 900 {
		t.Errorf("offset %d, expected 900: same file, nothing to reread", p.tasks[0].Offset)
	}
}

func TestTasksGoInTimeOrder(t *testing.T) {
	files := []File{
		{Path: "/p/x.jsonl", Contour: "home", Size: 10, First: at(4)},
		{Path: "/p/y.jsonl", Contour: "home", Size: 10, First: at(12)},
		{Path: "/p/z.jsonl", Contour: "home", Size: 10, First: at(8)},
		{Path: "/p/empty.jsonl", Contour: "home", Size: 0},
	}
	p := buildPlan(files, nil)
	want := []string{"/p/x.jsonl", "/p/z.jsonl", "/p/y.jsonl", "/p/empty.jsonl"}
	for i, task := range p.tasks {
		if task.Path != want[i] {
			t.Fatalf("the task order is %v, expected %v", pathsOf(p.tasks), want)
		}
	}
}

func pathsOf(tasks []Task) []string {
	out := make([]string, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, t.Path)
	}
	return out
}

func TestRewriteOnlyWhenReadFromTheStart(t *testing.T) {
	res := Result{Path: "/p/a.jsonl", Offset: 1200,
		Session: Session{SessionID: "s1", Contour: "home", Agents: []string{"", "sidechain"}}}
	f := File{Path: "/p/a.jsonl", Contour: "home", Inode: 7, Size: 1200, Head: head("head")}

	whole, err := toStore(res, f, 0)
	if err != nil {
		t.Fatalf("the conversion: %v", err)
	}
	if !whole.Rewrite {
		t.Error("a file reread whole must wipe its previous rows")
	}
	if len(whole.Agents) != 2 {
		t.Errorf("agents %v, expected the pair from the session: the wipe goes by them", whole.Agents)
	}

	tail, err := toStore(res, f, 600)
	if err != nil {
		t.Fatalf("the conversion: %v", err)
	}
	if tail.Rewrite {
		t.Error("picking up the tail must not wipe what was collected before the boundary")
	}
}

func TestBoundaryOnlyMovesForward(t *testing.T) {
	f := File{Path: "/p/a.jsonl", Contour: "home", Inode: 7, Size: 1200, Head: head("head")}
	got, err := toStore(Result{Path: "/p/a.jsonl", Offset: 0, Session: Session{SessionID: "s1"}}, f, 600)
	if err != nil {
		t.Fatalf("the conversion: %v", err)
	}
	if got.Scan.Offset != 600 {
		t.Errorf("the boundary is %d, expected 600: it never moves back", got.Scan.Offset)
	}
}

// The repository of a worktree session is known only on the host: what the
// agent found reaches the store, and the session is placed by it.
func TestTheRepositoryOfAWorktreeReachesTheStore(t *testing.T) {
	f := File{Path: "/p/a.jsonl", Contour: "home", Inode: 7, Size: 10, Head: head("head")}
	got, err := toStore(Result{Path: f.Path, Session: Session{
		SessionID: "s1", CWD: "/srv/proj/shop-fix", Checkout: "/srv/proj/shop"}}, f, 0)
	if err != nil {
		t.Fatalf("the conversion: %v", err)
	}
	if got.Session.Checkout != "/srv/proj/shop" || got.Session.CWD != "/srv/proj/shop-fix" {
		t.Errorf("the session went to the store as %+v", got.Session)
	}
}

func TestUnparsableHourFailsTheFile(t *testing.T) {
	f := File{Path: "/p/a.jsonl", Contour: "home", Inode: 7, Size: 10, Head: head("head")}
	_, err := toStore(Result{
		Path:    "/p/a.jsonl",
		Session: Session{SessionID: "s1", Contour: "home"},
		Rows:    []Row{{Bucket: "yesterday", Model: "opus", Answers: 1}},
	}, f, 0)
	if err == nil {
		t.Fatal("a row with an unparsable hour must fail the file instead of landing as hour zero")
	}
}

func TestContourFallsBackToTheListing(t *testing.T) {
	f := File{Path: "/p/a.jsonl", Contour: "work", Inode: 7, Size: 10, Head: head("head")}
	got, err := toStore(Result{Path: "/p/a.jsonl", Session: Session{SessionID: "s1"}}, f, 0)
	if err != nil {
		t.Fatalf("the conversion: %v", err)
	}
	if got.Session.Contour != "work" || got.Scan.Contour != "work" {
		t.Errorf("contour %q/%q, expected work", got.Session.Contour, got.Scan.Contour)
	}
}

func TestBrokenFileStillGetsAScanPoint(t *testing.T) {
	f := File{Path: "/p/empty.jsonl", Contour: "home", Inode: 3, Size: 0, Head: head("")}
	got, err := toStore(Result{Path: "/p/empty.jsonl"}, f, 0)
	if err != nil {
		t.Fatalf("the conversion: %v", err)
	}
	if got.Scan.Path != "/p/empty.jsonl" || got.Session.SessionID != "" {
		t.Errorf("the scan point is %+v, expected it without a session", got.Scan)
	}
}
