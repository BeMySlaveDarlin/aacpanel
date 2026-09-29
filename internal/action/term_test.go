package action

import (
	"context"
	"strings"
	"testing"
)

type termsExec struct {
	Executor
	list []Term
}

func (e termsExec) Terms(context.Context) ([]Term, error) { return e.list, nil }

func TestServerListsTheTerminalsOfThePanel(t *testing.T) {
	want := []Term{{ID: "t-1a2b3c4d", Place: "/srv/proj/shop", Name: "make", Command: "make", Clients: 1}}
	client := serve(t, termsExec{Executor: okExecutor("done"), list: want})

	got, err := client.Terms(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("the terminals arrived as %+v", got)
	}

	plain := serve(t, okExecutor("done"))
	if _, err := plain.Terms(context.Background()); err == nil {
		t.Error("an executor that keeps no terminals answered with a list")
	}
}

func TestTermIDIsTheFormThePanelGives(t *testing.T) {
	for _, id := range []string{"t-1a2b3c4d", "t-00000000", "t-ffffffff"} {
		if !TermID(id) {
			t.Errorf("%q is refused", id)
		}
	}
	for _, id := range []string{"", "t-", "t-1a2b3c4", "t-1a2b3c4d5", "t-1A2B3C4D", "t-1a2b3c4g", "T-1a2b3c4d",
		"=t-1a2b3c4d", "t-1a2b3c4d:", "shop", "x-1a2b3c4d"} {
		if TermID(id) {
			t.Errorf("%q is taken for the id of a terminal", id)
		}
	}
}

func TestTermRequestsAreValidated(t *testing.T) {
	good := []Request{
		{ID: "a1", Kind: TermStart, Target: "t-1a2b3c4d", Place: "/srv/proj/shop"},
		{ID: "a1", Kind: TermClose, Target: "t-1a2b3c4d"},
		{ID: "a1", Kind: TermConsole, Target: "t-1a2b3c4d"},
		{ID: "a1", Kind: TermRename, Target: "t-1a2b3c4d", Rename: "make check"},
		{ID: "a1", Kind: TermRename, Target: "t-1a2b3c4d", Rename: strings.Repeat("ü", TermNameMax)},
		{Ask: AskTerms},
	}
	for _, r := range good {
		if err := r.Validate(); err != nil {
			t.Errorf("%+v is refused: %v", r, err)
		}
	}
	bad := map[string]Request{
		"a session name for an id":  {ID: "a1", Kind: TermClose, Target: "shop"},
		"an id in capitals":         {ID: "a1", Kind: TermConsole, Target: "t-1A2B3C4D"},
		"a start without a place":   {ID: "a1", Kind: TermStart, Target: "t-1a2b3c4d"},
		"a relative place":          {ID: "a1", Kind: TermStart, Target: "t-1a2b3c4d", Place: "srv/proj"},
		"a place climbing out":      {ID: "a1", Kind: TermStart, Target: "t-1a2b3c4d", Place: "/srv/proj/../../etc"},
		"a place with a semicolon":  {ID: "a1", Kind: TermStart, Target: "t-1a2b3c4d", Place: "/srv/proj;rm"},
		"a place for another kind":  {ID: "a1", Kind: TermClose, Target: "t-1a2b3c4d", Place: "/srv/proj/shop"},
		"a place for a session":     {ID: "a1", Kind: SessionClose, Target: "shop", Place: "/srv/proj/shop"},
		"an empty name":             {ID: "a1", Kind: TermRename, Target: "t-1a2b3c4d"},
		"a blank name":              {ID: "a1", Kind: TermRename, Target: "t-1a2b3c4d", Rename: "  "},
		"a name too long":           {ID: "a1", Kind: TermRename, Target: "t-1a2b3c4d", Rename: strings.Repeat("x", TermNameMax+1)},
		"a name with an escape":     {ID: "a1", Kind: TermRename, Target: "t-1a2b3c4d", Rename: "red \x1b[31m"},
		"a name over two lines":     {ID: "a1", Kind: TermRename, Target: "t-1a2b3c4d", Rename: "one\ntwo"},
		"a name that is not UTF-8":  {ID: "a1", Kind: TermRename, Target: "t-1a2b3c4d", Rename: "a\xffb"},
		"a name for a close":        {ID: "a1", Kind: TermClose, Target: "t-1a2b3c4d", Rename: "build"},
		"the list asked for one":    {Ask: AskTerms, Target: "t-1a2b3c4d"},
		"text typed into a console": {ID: "a1", Kind: TermConsole, Target: "t-1a2b3c4d", Text: "rm -rf ~"},
	}
	for name, r := range bad {
		if err := r.Validate(); err == nil {
			t.Errorf("%s: %+v is accepted", name, r)
		}
	}
}
