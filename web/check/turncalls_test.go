package check

import (
	"reflect"
	"strings"
	"testing"
)

type turnCallsShot struct {
	Calls     []string   `json:"calls"`
	Head      []string   `json:"head"`
	EmptyHead string     `json:"emptyHead"`
	EmptyHint string     `json:"emptyHint"`
	EmptyLeft int        `json:"emptyLeft"`
	Places    placesShot `json:"places"`
}

type placeRequest struct {
	Session string `json:"session"`
	ID      string `json:"id"`
	Pos     string `json:"pos"`
	Index   string `json:"i"`
	Path    string `json:"path"`
}

type placeSend struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Params struct {
		Text string `json:"text"`
	} `json:"params"`
}

type placesShot struct {
	PlacedBreaks int          `json:"placedBreaks"`
	PlainBreaks  int          `json:"plainBreaks"`
	OutsideFile  bool         `json:"outsideFile"`
	Elapsed      float64      `json:"elapsed"`
	Edited       bool         `json:"edited"`
	Native       bool         `json:"native"`
	Cards        int          `json:"cards"`
	Malformed    []string     `json:"malformed"`
	Menu         []string     `json:"menu"`
	HoldOnlyMenu bool         `json:"holdOnlyMenu"`
	Outside      bool         `json:"outside"`
	Back         bool         `json:"back"`
	Copied       string       `json:"copied"`
	History      placeRequest `json:"history"`
	CopyRequest  placeRequest `json:"copyRequest"`
	TapRequest   placeRequest `json:"tapRequest"`
	OpenRequest  placeRequest `json:"openRequest"`
	Draft        struct {
		Head    string `json:"head"`
		Excerpt string `json:"excerpt"`
		Text    string `json:"text"`
		Focused bool   `json:"focused"`
		Count   int    `json:"count"`
	} `json:"draft"`
	Send          placeSend    `json:"send"`
	Local         string       `json:"local"`
	LocalQueued   bool         `json:"localQueued"`
	FileMenu      []string     `json:"fileMenu"`
	FileDraft     string       `json:"fileDraft"`
	FileSend      placeSend    `json:"fileSend"`
	FileRequest   placeRequest `json:"fileRequest"`
	SuccessHead   string       `json:"successHead"`
	SuccessSend   placeSend    `json:"successSend"`
	EmptyEnabled  bool         `json:"emptyEnabled"`
	EmptyDraft    string       `json:"emptyDraft"`
	EmptySend     placeSend    `json:"emptySend"`
	EmptyCard     string       `json:"emptyCard"`
	EmptyRequest  placeRequest `json:"emptyRequest"`
	NoUseDisabled bool         `json:"noUseDisabled"`
	Replaced      bool         `json:"replaced"`
	Removed       bool         `json:"removed"`
	Refused       bool         `json:"refused"`
}

func TestACallOrFileCanBeAskedAboutFromItsPlace(t *testing.T) {
	for _, wide := range []bool{false, true} {
		name := "phone"
		if wide {
			name = "desktop"
		}
		t.Run(name, func(t *testing.T) {
			var page turnCallsShot
			if wide {
				runWideFixture(t, "turncalls.html", &page)
			} else {
				runFixture(t, "turncalls.html", &page)
			}
			got := page.Places
			t.Logf("place scenario: %.3f s", got.Elapsed/1000)
			if got.PlacedBreaks != 2 || got.PlainBreaks != 1 || !got.OutsideFile {
				t.Errorf("own message breaks: with a place %d, plain %d; outside file refuses opening %v", got.PlacedBreaks, got.PlainBreaks, got.OutsideFile)
			}
			if (wide && !got.Edited) || (!wide && !got.Native) {
				t.Errorf("the edited message lost its place or the phone claimed text selection: edit %v, native %v", got.Edited, got.Native)
			}
			if got.Cards != 1 || len(got.Malformed) != 2 || !strings.Contains(got.Malformed[0], "] trailing") {
				t.Errorf("only an exact heading at the start becomes a card: %d cards, ordinary messages %q", got.Cards, got.Malformed)
			}
			if !reflect.DeepEqual(got.Menu, []string{"Ask about this", "Copy the output", "Open"}) || !got.HoldOnlyMenu || !got.Outside || !got.Back {
				t.Errorf("the object's menu or its dismissal opened the call as well: %+v", got)
			}
			wantRequest := placeRequest{Session: "lab", ID: "s1", Pos: "40", Index: "2"}
			for _, request := range []placeRequest{got.History, got.CopyRequest, got.TapRequest, got.OpenRequest} {
				if request != wantRequest {
					t.Errorf("the call was read at %+v, expected %+v", request, wantRequest)
				}
			}
			if got.Copied != "discard\n\nfirst\n"+strings.Repeat("x", 170)+"\nlast\n\n" {
				t.Errorf("copy lost output: %q", got.Copied)
			}
			if got.Draft.Head != "Bash · make check · failed" || got.Draft.Excerpt != "first\n"+strings.Repeat("x", 159)+"…\nlast" || got.Draft.Text != "Why?" || !got.Draft.Focused || got.Draft.Count != 1 {
				t.Errorf("the draft lost its place, excerpt or words: %+v", got.Draft)
			}
			want := "> [Bash · make check · failed · use u40 · pos 40.2]\n> first\n> " + strings.Repeat("x", 159) + "…\n> last\n\nWhy?"
			if got.Send.Kind != "session.send" || got.Send.Target != "lab" || got.Send.Params.Text != want {
				t.Errorf("the sent message lost the address or its exact form: %+v", got.Send)
			}
			if !got.LocalQueued || !strings.Contains(got.Local, "Bash · make check · failed") || !strings.HasSuffix(got.Local, "Why?") {
				t.Errorf("the local queued row lost its card or words: %q (queued %v)", got.Local, got.LocalQueued)
			}
			if len(got.FileMenu) == 0 || got.FileMenu[0] != "Ask about this" || got.FileDraft != "notes.txt" || got.FileSend.Kind != "session.send" || got.FileSend.Params.Text != "> [file /srv/proj/notes.txt]" || got.FileRequest != (placeRequest{Session: "lab", ID: "s1", Path: "/srv/proj/notes.txt"}) {
				t.Errorf("the file lost its menu, draft, send or opening: %+v", got)
			}
			wantSuccess := "> [Read · read " + strings.Repeat("y", 74) + "… · use u41 · pos 41.0]"
			if strings.Contains(got.SuccessHead, "failed") || got.SuccessSend.Kind != "session.send" || got.SuccessSend.Params.Text != wantSuccess {
				t.Errorf("a successful call gained a failure or lost its capped argument: %q, %+v", got.SuccessHead, got.SuccessSend)
			}
			if !got.Replaced || !got.Removed || !got.Refused {
				t.Errorf("replace %v, remove %v, refused fetch %v", got.Replaced, got.Removed, got.Refused)
			}
			if !got.EmptyEnabled || got.EmptyDraft != "Read" || got.EmptyCard != "Read" || got.EmptySend.Kind != "session.send" || got.EmptySend.Target != "lab" || got.EmptySend.Params.Text != "> [Read · — · use u42 · pos 42.0]" || got.EmptyRequest != (placeRequest{Session: "lab", ID: "s1", Pos: "42", Index: "0"}) || !got.NoUseDisabled {
				t.Errorf("the call without an argument lost its placeholder, card or address, or an unaddressed call became quotable: %+v", got)
			}
		})
	}
}

// The badge of a turn opens the calls of the whole turn — every run between
// the end of the turn before and this one — with how long the turn took and
// how many agents it left at work.
func TestTheBadgeOfATurnOpensItsCalls(t *testing.T) {
	var got turnCallsShot
	runFixture(t, "turncalls.html", &got)

	all := strings.Join(got.Calls, " | ")
	if len(got.Calls) != 3 || !strings.Contains(all, "thinking") ||
		!strings.Contains(all, "make check") || !strings.Contains(all, "main.go") {
		t.Errorf("the turn lists %v: the thinking and the calls of both its runs", got.Calls)
	}
	if strings.Contains(all, "the turn before") {
		t.Errorf("the turn lists a call of the turn before it: %v", got.Calls)
	}
	if len(got.Head) != 2 || !strings.HasPrefix(got.Head[0], "2 calls · worked 4m 59s · ") {
		t.Fatalf("the head of the sheet reads %q: the calls, how long the turn took and when it ended", got.Head)
	}
	if got.Head[1] != "2 background agents were still at work" {
		t.Errorf("the head does not say what the turn left at work: %q", got.Head[1])
	}
	if !strings.HasPrefix(got.EmptyHead, "0 calls · worked 2s") || got.EmptyLeft != 1 {
		t.Errorf("a turn without calls reads %q with %d lines", got.EmptyHead, got.EmptyLeft)
	}
	if !strings.Contains(got.EmptyHint, "no calls") {
		t.Errorf("a turn without calls opens an empty sheet with no word: %q", got.EmptyHint)
	}
}
