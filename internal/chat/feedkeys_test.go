package chat

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// feedPlace is a place in the collector that writes keys into a dict, as
// testdata/feedkeys.py reads it off the source.
type feedPlace struct {
	File     string   `json:"file"`
	Function string   `json:"function"`
	Var      string   `json:"var"`
	Row      bool     `json:"row"`
	Keys     []string `json:"keys"`
	Unknown  bool     `json:"unknown"`
	Line     int      `json:"line"`
}

// The dicts of the collector that are no row of the feed and none of the
// parts of a row, by file, by file and function, or by file, function and
// variable, and what they are instead.
var notTheFeed = map[string]string{
	"repo.py":   "the replies of the repository screen, under RepoOut",
	"server.py": "the replies of the socket around the feed, under Reply",
	"spots.py":  "the reply of one call opened from the feed",
	"locate.py": "where the transcripts lie",

	"commands.py:usage_from_markdown": "the numbers of an answer, carried whole in Data",
	"commands.py:usage_from_record":   "the numbers of an answer, carried whole in Data",
	"commands.py:usage_report":        "the numbers of an answer, carried whole in Data",
	"commands.py:servers":             "the numbers of an answer, carried whole in Data",
	"commands.py:capped":              "the numbers of an answer, carried whole in Data",
	"commands.py:behaviors":           "the numbers of an answer, carried whole in Data",
	"commands.py:entry":               "the numbers of an answer, carried whole in Data",
	"commands.py:tables":              "the rows of a markdown table read into the numbers of an answer",
	"disk.py:read_file":               "the reply of a file opened from the feed, under Reply",
	"disk.py:read_raw":                "the reply of a file opened from the feed, under Reply",
	"disk.py:task_output":             "the reply of the output of a task, under Reply",

	"records.py:result_mark": "a mark of a result, for the fold: it sets what came on the call, in ToolCall",
	"codex.py:_call:mark":    "a mark of a result, for the fold: it sets what came on the call, in ToolCall",
	"codex.py:details":       "the reply of one call opened from the feed, under Call",
	"codex.py:context":       "how full the context of a codex thread is, for its row in the snapshot",
	"codex.py:_take":         "how full the context of a codex thread is, for its row in the snapshot",

	"mail.py::_PEER_NAMES":                 "the names of the sessions next door, by pid",
	"queue.py:remember:texts":              "the texts the queue has drawn, by text",
	"tail.py:view:_locks":                  "the locks of the parsed ends of transcripts, by file",
	"tail.py:view:_pieces":                 "the parsed ends of transcripts, by file",
	"records.py:rows_of:calls":             "the calls waiting for a result, by id",
	"records.py:rows_of:asks":              "the rounds of questions waiting for an answer, by id",
	"records.py:rows_of:briefs":            "the calls that may publish a brief or ask for a secret, by id",
	"records.py:parse:seen":                "how many rows of each role a record gave so far, by role",
	"records.py:cutoff:calls":              "the calls waiting for a result, by id",
	"records.py:command_card:unanswered":   "the commands waiting for an answer, by the record it will name",
	"records.py:command_answer:unanswered": "the commands waiting for an answer, by the record it will name",
	"window.py:fold:window":                "the list of the rows of a window, by place",
	"window.py:fold:<return>":              "the window itself, under Reply",
	"search.py:take:counted":               "the hits of the rows a later record may draw again, by place, role and number",
}

// Dicts of the collector that answer a request of their own rather than make
// a row of the feed, by file, and the structures the service carries them in:
// a key with no field there is dropped on the way just the same.
var answersOfTheirOwn = map[string][]any{
	"search.py": {Match{}, Found{}},
}

// Keys a row carries that the collector reads and the screen never does: they
// are dropped on the way, and that is their place.
var keptByTheCollector = map[string]string{
	"index":  "the fold puts a call into a group of calls, and its index lands in ToolCall",
	"open":   "the fold keeps a running call open in ToolCall, and a letter open until its answer",
	"edited": "the fold hangs the files a call wrote on the answer after it",
}

// A row of the feed is built by the collector and carried to the screen by the
// service through Item and the parts it holds, and a key with no field there is
// dropped on the way without a word: the card of a slash command reached the
// screen with no numbers in it, a card of permissions with no rows, the output
// of a shell command without the command and its exit code. A hand-written
// sample holds the keys someone remembered, and a scan of the literals the
// keys someone wrote into a literal; this reads every place of the collector
// that writes a key into a dict — a literal, a subscript, a keyword, a key
// bound by a loop — and demands a field for each. A place that writes into a
// row is held to Item itself; any other to Item or a part of it; and a dict
// that is no part of the feed has to be named so above.
func TestEveryKeyTheFeedCarriesHasAField(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 was not found: the collector is read by the parser of its own language")
	}
	files, err := filepath.Glob(filepath.Join("..", "..", "agent", "chat", "*.py"))
	if err != nil || len(files) == 0 {
		t.Fatalf("the collector source is out of reach: %v", err)
	}
	raw, err := exec.Command(python, append([]string{filepath.Join("testdata", "feedkeys.py")}, files...)...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Fatalf("the reading of the collector failed: %v\n%s", err, ee.Stderr)
		}
		t.Fatal(err)
	}
	var places []feedPlace
	if err := json.Unmarshal(raw, &places); err != nil {
		t.Fatalf("the places did not parse: %v", err)
	}

	row := jsonNames(Item{})
	part := jsonNames(Item{}, ToolCall{}, Shot{}, ThinkSpot{}, FileRef{}, Asked{}, Permitted{})

	rows, kept := 0, map[string]bool{}
	for _, p := range places {
		if elsewhere(p) != "" {
			continue
		}
		if held, ok := answersOfTheirOwn[p.File]; ok {
			names := jsonNames(held...)
			for _, key := range p.Keys {
				if !names[key] {
					t.Errorf("%s:%s:%d puts %q into an answer (%q) whose structures have nowhere to put it: "+
						"the service drops it on the way to the screen", p.File, p.Function, p.Line, key, p.Var)
				}
			}
			continue
		}
		if strings.ToUpper(p.Var) == p.Var && p.Var != "" {
			// A table of the module, not a dict built for a reply.
			continue
		}
		where := p.File + ":" + p.Function
		if p.Unknown {
			t.Errorf("%s:%d writes into %q a key nobody can read off the source: write it as a "+
				"string, or name the dict in notTheFeed if it is no part of the feed", where, p.Line, p.Var)
		}
		if p.Row {
			rows++
		}
		for _, key := range p.Keys {
			if p.Row && row[key] {
				continue
			}
			if !p.Row && part[key] {
				continue
			}
			if keptByTheCollector[key] != "" {
				kept[key] = true
				continue
			}
			what := "a part of a row"
			holder := "Item or the parts it holds"
			if p.Row {
				what, holder = "a row", "Item"
			}
			t.Errorf("%s:%d puts %q into %s of the feed (%q) and %s has nowhere to put it: "+
				"the service drops it on the way to the screen", where, p.Line, key, what, p.Var, holder)
		}
	}
	if rows < 30 {
		t.Fatalf("only %d places write a row of the feed: the test reads the wrong place", rows)
	}
	var stale []string
	for key := range keptByTheCollector {
		if !kept[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("keptByTheCollector names %v and no row carries them any more", stale)
	}
	for name := range notTheFeed {
		found := false
		for _, p := range places {
			if placeKey(p, strings.Count(name, ":")) == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("notTheFeed names %q and the collector has no such place", name)
		}
	}
}

// A shell command run with "!" and its output reach the screen with the code it
// exited with — 0 included, which a field that drops zeros would lose — and
// the output with its command, which heads the sheet of the whole output. A
// sent file from outside the directory of the conversation keeps its mark,
// and a file inside it carries none.
func TestAShellRunAndASentFileKeepTheirMarksThroughTheService(t *testing.T) {
	const fromCollector = `[
		{"role": "shell", "text": "make check", "cut": false, "at": "2026-10-01T15:00:00Z", "pos": 10, "code": 0},
		{"role": "shellout", "text": "ok", "cut": false, "at": "2026-10-01T15:00:00Z", "pos": 10,
		 "command": "make check", "code": 0},
		{"role": "shellout", "text": "", "err": "no rule", "cut": false, "pos": 20, "command": "make chek", "code": 2},
		{"role": "sent", "pos": 30, "files": [
			{"path": "/srv/proj/report.pdf", "name": "report.pdf", "size": 10},
			{"path": "/home/u/.cache/scratch/report.pdf", "name": "report.pdf", "size": 10, "outside": true}]}
	]`
	var items []Item
	if err := json.Unmarshal([]byte(fromCollector), &items); err != nil {
		t.Fatalf("the rows do not parse: %v", err)
	}
	out, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}
	sent := string(out)
	for _, want := range []struct {
		part string
		n    int
	}{{`"code":0`, 2}, {`"code":2`, 1}, {`"command":"make check"`, 1}, {`"command":"make chek"`, 1},
		{`"outside":true`, 1}, {`"outside"`, 1}} {
		if got := strings.Count(sent, want.part); got != want.n {
			t.Errorf("%s reaches the screen %d times, want %d: %s", want.part, got, want.n, sent)
		}
	}
}

// elsewhere returns what a dict that is no part of the feed is instead, or "".
func elsewhere(p feedPlace) string {
	for depth := 2; depth >= 0; depth-- {
		if why := notTheFeed[placeKey(p, depth)]; why != "" {
			return why
		}
	}
	return ""
}

// placeKey names a place as notTheFeed does: its file, then its function,
// then its variable.
func placeKey(p feedPlace, depth int) string {
	parts := []string{p.File, p.Function, p.Var}
	return strings.Join(parts[:depth+1], ":")
}

// jsonNames returns the names the fields of these structures take in JSON.
func jsonNames(values ...any) map[string]bool {
	out := map[string]bool{}
	for _, v := range values {
		typ := reflect.TypeOf(v)
		for i := 0; i < typ.NumField(); i++ {
			name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
			if name != "" && name != "-" {
				out[name] = true
			}
		}
	}
	return out
}
