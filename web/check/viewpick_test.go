package check

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"aacpanel/internal/webbuild"

	esbuild "github.com/evanw/esbuild/pkg/api"
)

type viewCase struct {
	Saved   string `json:"saved"`
	CanTerm bool   `json:"canTerm"`
	Wide    bool   `json:"wide"`
}

func TestChatViewFollowsTheSavedChoice(t *testing.T) {
	cases := []struct {
		name string
		in   viewCase
		want string
	}{
		{"nothing saved on a wide screen — the terminal", viewCase{"", true, true}, "term"},
		{"nothing saved on a narrow screen — the feed", viewCase{"", true, false}, "feed"},
		{"the feed is chosen on a wide screen", viewCase{"feed", true, true}, "feed"},
		{"the terminal is chosen on a narrow screen", viewCase{"term", true, false}, "term"},
		{"there is no terminal — the feed, even though the terminal is chosen", viewCase{"term", false, true}, "feed"},
		{"no terminal and no choice — the feed", viewCase{"", false, true}, "feed"},
		{"junk in the storage — the default by width", viewCase{"terminal", true, true}, "term"},
		{"junk in the storage on a narrow screen — the feed", viewCase{"terminal", true, false}, "feed"},
	}
	ins := make([]viewCase, 0, len(cases))
	for _, c := range cases {
		ins = append(ins, c.in)
	}
	got := runViewPickJS(t, ins)
	for i, c := range cases {
		if got.Views[i] != c.want {
			t.Errorf("%s: viewOf(%q, %v, %v) = %q, expected %q",
				c.name, c.in.Saved, c.in.CanTerm, c.in.Wide, got.Views[i], c.want)
		}
	}
}

// The choice is the session's own: kept on the device by its name, read back
// after the app is gone, and seen by no other session.
func TestChatViewOutlivesTheAppAndSparesSessionsWithoutTerminal(t *testing.T) {
	got := runViewPickJS(t, nil)
	for _, c := range []struct {
		name, got, want string
	}{
		{"an empty storage holds no choice", got.Storage["fresh"], ""},
		{"what was saved reads back", got.Storage["saved"], "feed"},
		{"another session does not see the choice", got.Storage["otherSession"], ""},
		{"two sessions keep a choice each", got.Storage["twoSessions"], "feed,term"},
		{"a second pick replaces the first and is kept once", got.Storage["again"], "term:1"},
		{"junk in the key reads as \"nothing was chosen\"", got.Storage["junk"], ""},
		{"a broken entry is skipped, the rest read back", got.Storage["junkEntry"], ",feed"},
		{"reading from an unavailable storage", got.Storage["readThrows"], ""},
		{"writing into an unavailable storage does not break the screen", got.Storage["writeThrows"], "ok"},
		{"after a re-entry the choice is shown, not the default", got.Storage["afterReload"], "feed"},
		{"a session without a terminal shows the feed", got.Storage["noTermView"], "feed"},
		{"a session without a terminal leaves the setting alone", got.Storage["noTermKept"], "term"},
		{"the next session opens by the width, not by the earlier choice", got.Storage["nextSession"], "term"},
		{"past the ceiling the oldest choice goes and a fresh pick stays", got.Storage["ceiling"], "200:,feed,feed,term"},
		{"the device storage is not touched past the one passed in", got.Storage["strayed"], ""},
	} {
		if c.got != c.want {
			t.Errorf("%s: got %q, expected %q", c.name, c.got, c.want)
		}
	}
}

type viewPickOut struct {
	Views   []string          `json:"views"`
	Storage map[string]string `json:"storage"`
}

func runViewPickJS(t *testing.T, cases []viewCase) viewPickOut {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the conversation layout is run by the engine, not by reading the source")
	}
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}
	entry, err := filepath.Abs(filepath.Join(webDir, "src", "screens", "chat", "viewpick.js"))
	if err != nil {
		t.Fatal(err)
	}
	built := esbuild.Build(esbuild.BuildOptions{
		EntryPoints: []string{entry},
		Bundle:      true,
		Format:      esbuild.FormatESModule,
		Platform:    esbuild.PlatformNeutral,
		Alias:       alias,
		Write:       false,
	})
	if len(built.Errors) > 0 {
		t.Fatalf("viewpick.js did not build: %v", built.Errors[0].Text)
	}

	dir := t.TempDir()
	bundle := filepath.Join(dir, "viewpick.mjs")
	if err := os.WriteFile(bundle, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}

	script := `
import { readFileSync } from "node:fs";
import { readView, saveView, viewOf, VIEW_KEY } from ` + jsString("file://"+bundle) + `;

function store(seed, trouble) {
    const data = { ...(seed || {}) };
    return {
        data,
        getItem(key) {
            if (trouble === "read") throw new Error("storage unavailable");
            return key in data ? data[key] : null;
        },
        setItem(key, value) {
            if (trouble === "write") throw new Error("storage unavailable");
            data[key] = value;
        },
    };
}

let strayed = "";
globalThis.localStorage = {
    getItem() { strayed = "a read past the storage passed in"; return null; },
    setItem() { strayed = "a write past the storage passed in"; },
};

const storage = {};

function safe(name, fn) {
    try {
        storage[name] = fn();
    } catch (err) {
        storage[name] = "threw: " + err.message;
    }
}

safe("fresh", () => readView("evirma", store()));
safe("saved", () => {
    const s = store();
    saveView("evirma", "feed", s);
    return readView("evirma", s);
});
safe("otherSession", () => {
    const s = store();
    saveView("evirma", "feed", s);
    return readView("evirma-c", s);
});
safe("twoSessions", () => {
    const s = store();
    saveView("evirma", "feed", s);
    saveView("evirma-c", "term", s);
    return readView("evirma", s) + "," + readView("evirma-c", s);
});
safe("again", () => {
    const s = store();
    saveView("evirma", "feed", s);
    saveView("evirma", "term", s);
    return readView("evirma", s) + ":" + JSON.parse(s.data[VIEW_KEY]).length;
});
safe("junk", () => readView("evirma", store({ [VIEW_KEY]: "terminal" })));
safe("junkEntry", () => {
    const s = store({ [VIEW_KEY]: JSON.stringify([["evirma", "terminal"], "evirma-c", [7, "feed"], ["evirma-c", "feed"]]) });
    return readView("evirma", s) + "," + readView("evirma-c", s);
});
safe("readThrows", () => readView("evirma", store({ [VIEW_KEY]: JSON.stringify([["evirma", "term"]]) }, "read")));
safe("writeThrows", () => {
    const s = store({}, "write");
    saveView("evirma", "term", s);
    return readView("evirma", s) === "" ? "ok" : "written into an unavailable storage";
});
safe("afterReload", () => {
    const s = store();
    saveView("evirma", "feed", s);
    return viewOf(readView("evirma", s), true, true);
});
const noTerm = store();
saveView("evirma", "term", noTerm);
safe("noTermView", () => viewOf(readView("evirma", noTerm), false, true));
safe("noTermKept", () => readView("evirma", noTerm));
safe("nextSession", () => {
    const s = store();
    saveView("evirma", "feed", s);
    return viewOf(readView("evirma-c", s), true, true);
});
// Two hundred choices fill the storage; the oldest was picked again, so the
// next one past the ceiling pushes out the one after it.
safe("ceiling", () => {
    const s = store();
    for (let i = 0; i < 200; i++) saveView("s" + i, "feed", s);
    saveView("s0", "feed", s);
    saveView("s200", "term", s);
    const kept = JSON.parse(s.data[VIEW_KEY]).length;
    return kept + ":" + ["s1", "s0", "s2", "s200"].map((n) => readView(n, s)).join(",");
});
storage.strayed = strayed;

const cases = JSON.parse(readFileSync(0, "utf8")) || [];
const views = cases.map((c) => viewOf(c.saved, c.canTerm, c.wide));
process.stdout.write(JSON.stringify({ views, storage }));
`
	raw, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, "--input-type=module", "-e", script)
	cmd.Stdin = bytes.NewReader(raw)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("node: %v\n%s", err, stderr)
	}
	var got viewPickOut
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	if len(got.Views) != len(cases) {
		t.Fatalf("%d answers to %d cases", len(got.Views), len(cases))
	}
	return got
}
