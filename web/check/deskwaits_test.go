package check

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/webbuild"

	esbuild "github.com/evanw/esbuild/pkg/api"
)

func TestColumnShowsTheConsoleItIsRaising(t *testing.T) {
	task := func(before ...string) map[string]any {
		if before == nil {
			before = []string{}
		}
		return map[string]any{"kind": "open", "target": "kiosk", "before": before, "seen": 0}
	}
	session := func(name string) map[string]any { return map[string]any{"session": name} }

	cases := []struct {
		name string
		args []any
		want []string
	}{
		{
			"the snapshot does not see the console yet — the ghost stays",
			[]any{[]any{session("aacpanel")}, []any{task("aacpanel")}, 0},
			[]string{"kiosk"},
		},
		{
			"the console showed up in the snapshot — no ghost",
			[]any{[]any{session("aacpanel"), session("kiosk-2")}, []any{task("aacpanel")}, 0},
			[]string{},
		},
		{
			"another session came up — the ghost stays",
			[]any{[]any{session("aacpanel"), session("shop")}, []any{task("aacpanel")}, 0},
			[]string{"kiosk"},
		},
		{
			"a namesake was in the list before the press — the ghost stays",
			[]any{
				[]any{session("kiosk")},
				[]any{task("kiosk")},
				0,
			},
			[]string{"kiosk"},
		},
		{
			"the namesake in the snapshot is older than the press — the ghost stays",
			[]any{
				[]any{session("kiosk-2")},
				[]any{map[string]any{
					"kind": "open", "target": "kiosk", "before": []string{}, "seen": 1000,
				}},
				995,
			},
			[]string{"kiosk"},
		},
	}

	calls := make([][]any, 0, len(cases))
	for _, c := range cases {
		calls = append(calls, c.args)
	}
	got := runDeskSessionsJS(t, "raising", calls)
	for i, c := range cases {
		list, _ := got[i].([]any)
		names := make([]string, 0, len(list))
		for _, item := range list {
			row, _ := item.(map[string]any)
			name, _ := row["target"].(string)
			names = append(names, name)
		}
		if strings.Join(names, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: ghosts %v, expected %v", c.name, names, c.want)
		}
	}
}

func TestColumnDrawsWhatItIsWaitingFor(t *testing.T) {
	src := stripComments(srcFiles(t)["src/desktop/sessions.js"])
	if src == "" {
		t.Fatal("there is no session column: src/desktop/sessions.js — the test is looking in the wrong place")
	}

	if !strings.Contains(src, "raising(all, wait ? wait.opening() : []") {
		t.Error("the column does not ask which consoles are coming up: pressing open leaves " +
			"no trace until the next snapshot")
	}
	if !strings.Contains(src, "<${GhostLine}") {
		t.Error("a console coming up has nothing to stand on in the column — the ghost is counted and not drawn")
	}
	if !strings.Contains(src, `wait.of("close", s.session)`) {
		t.Error("the row does not ask whether it is being closed: the close stays unanswered until " +
			"the next snapshot, and the person presses close a second time")
	}
	if !strings.Contains(src, "closing ?") {
		t.Error("the close wait changes nothing in the row — it is counted and not shown")
	}
	if !strings.Contains(src, "shown.length === 0 && ghosts.length === 0") {
		t.Error("the column says \"there are no live sessions\" over the console row it shows " +
			"itself")
	}
}

func runDeskSessionsJS(t *testing.T, fn string, calls [][]any) []any {
	t.Helper()
	return runModuleJS(t, "src/desktop/sessions.js", fn, calls)
}

// runModuleJS bundles one front-end module and calls its export fn with every
// argument list in calls through node, returning what came back.
func runModuleJS(t *testing.T, module, fn string, calls [][]any) []any {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the module is run through the engine, not read out of the source")
	}
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}
	entry, err := filepath.Abs(webPath(module))
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
		t.Fatalf("%s did not build: %v", module, built.Errors[0].Text)
	}

	dir := t.TempDir()
	bundle := filepath.Join(dir, "column.mjs")
	if err := os.WriteFile(bundle, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}

	script := `
import { readFileSync } from "node:fs";
import * as column from ` + jsString("file://"+bundle) + `;
const calls = JSON.parse(readFileSync(0, "utf8"));
const fn = column[` + jsString(fn) + `];
if (!fn) throw new Error("no such function: " + ` + jsString(fn) + `);
process.stdout.write(JSON.stringify(calls.map((args) => fn(...args))));
`
	raw, err := json.Marshal(calls)
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
	var got []any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node reply did not parse: %v: %s", err, out)
	}
	if len(got) != len(calls) {
		t.Fatalf("%d replies for %d calls", len(got), len(calls))
	}
	return got
}

func TestSessionActionsDeclareWhatToWaitFor(t *testing.T) {
	files := srcFiles(t)

	watch := map[string]string{
		"session.open":    "open",
		"session.resume":  "open",
		"session.close":   "close",
		"session.restart": "restart",
		// A switch keeps the name and the conversation: the wait clears when the
		// session under the name lives on the other side.
		"session.switch":  "switch",
		"session.kill":    "close",
		"session.send":    "",
		"session.answer":  "",
		"session.permit":  "",
		"session.dismiss": "",
		"session.stop":    "",
		// Esc changes nothing in the list of sessions: the session goes on
		// standing where it stood, only its composer comes back.
		"session.escape":  "",
		"session.unqueue": "",
		"session.file":    "",
		"session.command": "",
		// A setting changes the session in place: nothing leaves the list or joins it.
		"session.set": "",
	}

	for id, body := range sessionActions(t, files[registryFile]) {
		want, listed := watch[id]
		if !listed {
			t.Errorf("action %s does not say what to wait for in the session list after it: "+
				"add it to this list — nothing is an answer too, but it has to be given", id)
			continue
		}
		has := strings.Contains(body, `watch: "`)
		if want == "" {
			if has {
				t.Errorf("%s declares a wait although it does not change the make-up of the live session list: "+
					"the panel will wait for what does not happen and keep a pending row up for a minute", id)
			}
			continue
		}
		if !strings.Contains(body, `watch: "`+want+`"`) {
			t.Errorf("%s does not declare watch: %q — the press stays unanswered until the next "+
				"agent snapshot, that is up to fifteen seconds", id, want)
		}
	}

	gate := stripComments(files["src/actions/gate.js"])
	if !strings.Contains(gate, "noteAction(ACTIONS[id].watch, target)") {
		t.Error("the gate does not start the wait from the declared consequence: then every button " +
			"starts it itself again, and the next button forgets again")
	}
	if at := strings.Index(gate, "noteAction("); at >= 0 && !strings.Contains(gate[:at], "if (!response.ok)") {
		t.Error("the wait starts before the response is checked: the panel will wait for the " +
			"consequences of a refused action")
	}

	if strings.Contains(stripComments(files["src/catchup.js"]), "start:") {
		t.Error("the wait can again be started from outside — by the same path along which it was forgotten")
	}
	for name, src := range files {
		if strings.Contains(stripComments(src), "wait.start(") {
			t.Errorf("%s starts a wait itself, past the gate: one action gets two waits "+
				"and the press next to it gets none", name)
		}
	}
}

func sessionActions(t *testing.T, registry string) map[string]string {
	t.Helper()
	block := actionsBlock(t, registry)
	out := map[string]string{}
	var id string
	var body []string
	flush := func() {
		if id != "" {
			out[id] = strings.Join(body, "\n")
		}
		id, body = "", nil
	}
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, `    "`) && strings.HasSuffix(strings.TrimSpace(line), ": {") {
			flush()
			key := strings.TrimPrefix(strings.TrimSpace(line), `"`)
			key = key[:strings.Index(key, `"`)]
			if strings.HasPrefix(key, "session.") {
				id = key
			}
			continue
		}
		if id != "" {
			body = append(body, line)
		}
	}
	flush()
	if len(out) == 0 {
		t.Fatal("no session action found in the registry — the parsing is looking in the wrong place")
	}
	return out
}

func TestGateStartsTheWaitItAnnounced(t *testing.T) {
	const shim = `
let slots = [];
let cursor = 0;
let queue = [];

let cleanups = [];

export function __render(fn) {
    cursor = 0;
    queue = [];
    const out = fn();
    for (const effect of queue) {
        const off = effect();
        if (typeof off === "function") cleanups.push(off);
    }
    return out;
}

export function __unmount() {
    for (const off of cleanups) off();
    cleanups = [];
}

export function useState(init) {
    const i = cursor++;
    if (!(i in slots)) slots[i] = { value: typeof init === "function" ? init() : init };
    const slot = slots[i];
    return [slot.value, (next) => { slot.value = typeof next === "function" ? next(slot.value) : next; }];
}

export function useRef(init) {
    const i = cursor++;
    if (!(i in slots)) slots[i] = { current: init };
    return slots[i];
}

export function useEffect(fn, deps) {
    const i = cursor++;
    const prev = slots[i];
    const same = prev && prev.deps && deps && deps.length === prev.deps.length
        && deps.every((d, k) => d === prev.deps[k]);
    slots[i] = { deps };
    if (!same) queue.push(fn);
}
`
	const probe = `
import { readFileSync } from "node:fs";
import { useCatchUp, noteAction } from %CATCHUP%;
import { __render, __unmount } from %SHIM%;

let asked = 0;
const refresh = () => { asked += 1; };
const before = { at: 1000, sessions: [{ session: "aacpanel" }, { session: "home" }] };
const after = { at: 1006, sessions: [{ session: "aacpanel" }, { session: "home" }, { session: "kiosk" }] };

let wait = __render(() => useCatchUp(before, refresh));
const beforeAction = Boolean(wait.of("open", "kiosk"));

noteAction("open", "kiosk");

wait = __render(() => useCatchUp(before, refresh));
const task = wait.of("open", "kiosk");

wait = __render(() => useCatchUp(after, refresh));
wait = __render(() => useCatchUp(after, refresh));

const answer = JSON.stringify({
    beforeAction,
    started: Boolean(task),
    seen: task ? task.seen : -1,
    before: task ? task.before : [],
    asked,
    settled: wait.of("open", "kiosk") === null,
});
__unmount();
process.stdout.write(answer);
`
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the link between the gate and the wait is run through the engine")
	}
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}
	dir := t.TempDir()
	shimPath := filepath.Join(dir, "hooks.mjs")
	if err := os.WriteFile(shimPath, []byte(shim), 0o600); err != nil {
		t.Fatal(err)
	}
	alias["preact/hooks"] = shimPath

	entry, err := filepath.Abs(filepath.Join(webDir, "src", "catchup.js"))
	if err != nil {
		t.Fatal(err)
	}
	built := esbuild.Build(esbuild.BuildOptions{
		EntryPoints: []string{entry},
		Bundle:      true,
		Format:      esbuild.FormatESModule,
		Platform:    esbuild.PlatformNeutral,
		Alias:       alias,
		External:    []string{shimPath},
		Write:       false,
	})
	if len(built.Errors) > 0 {
		t.Fatalf("catchup.js did not build: %v", built.Errors[0].Text)
	}
	bundle := filepath.Join(dir, "catchup.mjs")
	if err := os.WriteFile(bundle, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}

	script := strings.NewReplacer(
		"%CATCHUP%", jsString("file://"+bundle),
		"%SHIM%", jsString("file://"+shimPath),
	).Replace(probe)
	cmd := exec.Command(node, "--input-type=module", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("node: %v\n%s", err, stderr)
	}

	var got struct {
		BeforeAction bool     `json:"beforeAction"`
		Started      bool     `json:"started"`
		Seen         int      `json:"seen"`
		Before       []string `json:"before"`
		Asked        int      `json:"asked"`
		Settled      bool     `json:"settled"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node reply did not parse: %v: %s", err, out)
	}

	if got.BeforeAction {
		t.Error("a wait is found before the action — the check is looking in the wrong place")
	}
	if !got.Started {
		t.Fatal("the gate reported the console coming up but no wait started: the screen shows " +
			"no row, and the press stays unanswered until the next snapshot")
	}
	if got.Seen != 1000 {
		t.Errorf("the wait remembered snapshot mark %d, while the snapshot was taken at 1000", got.Seen)
	}
	if strings.Join(got.Before, ",") != "aacpanel,home" {
		t.Errorf("the wait remembered the list %v, while the snapshot held aacpanel and home", got.Before)
	}
	if got.Asked == 0 {
		t.Error("the wait did not hurry the snapshot: polling comes back to it in fifteen seconds")
	}
	if !got.Settled {
		t.Error("the snapshot caught up but the wait did not clear: the ghost stays in the column " +
			"next to the real row of the same console")
	}
}
