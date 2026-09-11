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

func TestChatViewOutlivesTheAppAndSparesSessionsWithoutTerminal(t *testing.T) {
	got := runViewPickJS(t, nil)
	for _, c := range []struct {
		name, got, want string
	}{
		{"an empty storage holds no choice", got.Storage["fresh"], ""},
		{"what was saved reads back", got.Storage["saved"], "feed"},
		{"junk in the key reads as \"nothing was chosen\"", got.Storage["junk"], ""},
		{"reading from an unavailable storage", got.Storage["readThrows"], ""},
		{"writing into an unavailable storage does not break the screen", got.Storage["writeThrows"], "ok"},
		{"after a re-entry the choice is shown, not the default", got.Storage["afterReload"], "feed"},
		{"a session without a terminal shows the feed", got.Storage["noTermView"], "feed"},
		{"a session without a terminal leaves the setting alone", got.Storage["noTermKept"], "term"},
		{"the next session with a terminal sees the earlier choice", got.Storage["nextSession"], "term"},
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

safe("fresh", () => readView(store()));
safe("saved", () => {
    const s = store();
    saveView("feed", s);
    return readView(s);
});
safe("junk", () => readView(store({ [VIEW_KEY]: "terminal" })));
safe("readThrows", () => readView(store({ [VIEW_KEY]: "term" }, "read")));
safe("writeThrows", () => {
    const s = store({}, "write");
    saveView("term", s);
    return readView(s) === "" ? "ok" : "written into an unavailable storage";
});
safe("afterReload", () => {
    const s = store();
    saveView("feed", s);
    return viewOf(readView(s), true, true);
});
const noTerm = store();
saveView("term", noTerm);
safe("noTermView", () => viewOf(readView(noTerm), false, true));
safe("noTermKept", () => noTerm.data[VIEW_KEY] || "");
safe("nextSession", () => viewOf(readView(noTerm), true, false));
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
