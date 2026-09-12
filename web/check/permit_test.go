package check

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/webbuild"

	esbuild "github.com/evanw/esbuild/pkg/api"
)

func TestPermitShowsTheDialogItCouldNotParse(t *testing.T) {
	got := renderPermitJS(t)

	for _, want := range []string{
		`<pre class="permitaction">`,
		"Bash command",
		"touch ~/permtest-tmux",
		"Do you want to proceed?",
		"Esc to cancel",
	} {
		if !strings.Contains(got.WithLines, want) {
			t.Errorf("the card does not show %q of the dialog it could not parse:\n%s", want, got.WithLines)
		}
	}
	if strings.Contains(got.WithLines, "cannot be seen from here") {
		t.Errorf("the card says the dialog cannot be seen while showing it:\n%s", got.WithLines)
	}

	if strings.Contains(got.NoLines, "permitaction") {
		t.Errorf("an empty block stands in place of the dialog lines:\n%s", got.NoLines)
	}
	if !strings.Contains(got.NoLines, "cannot be seen from here") {
		t.Errorf("the host sent no dialog lines and the card does not say so:\n%s", got.NoLines)
	}
	for _, want := range []string{"Close the dialog", "Esc cancels"} {
		if !strings.Contains(got.WithLines, want) || !strings.Contains(got.NoLines, want) {
			t.Errorf("%q is gone from the card: the only way out of an unanswerable dialog is the console", want)
		}
	}
}

type renderedPermit struct {
	WithLines string `json:"withLines"`
	NoLines   string `json:"noLines"`
}

func renderPermitJS(t *testing.T) renderedPermit {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the card is drawn by the engine, not read out of the source")
	}
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}

	// The card holds what it was told in state, so the hooks keep it between the two
	// renders: the first starts the request, the second draws the answer.
	const shim = `
let slots = [];
let cursor = 0;
let queue = [];

export function __render(fn) {
    cursor = 0;
    queue = [];
    const out = fn();
    for (const effect of queue) effect();
    return out;
}

export function __forget() {
    slots = [];
}

export function useState(init) {
    const i = cursor++;
    if (!(i in slots)) slots[i] = typeof init === "function" ? init() : init;
    return [slots[i], (next) => { slots[i] = typeof next === "function" ? next(slots[i]) : next; }];
}

export function useEffect(fn) {
    queue.push(fn);
}

export const useLayoutEffect = useEffect;
export const useRef = () => ({ current: null });
export const useMemo = (fn) => fn();
export const useCallback = (fn) => fn;
export const useReducer = (reduce, init) => [init, () => {}];
export const useContext = () => ({ run: async () => ({ ok: true }) });
`

	dir := t.TempDir()
	shimPath := filepath.Join(dir, "hooks.mjs")
	if err := os.WriteFile(shimPath, []byte(shim), 0o600); err != nil {
		t.Fatal(err)
	}
	alias["preact/hooks"] = shimPath

	entry, err := filepath.Abs(filepath.Join(webDir, "src", "screens", "chat", "permit.js"))
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
		t.Fatalf("permit.js did not build: %v", built.Errors[0].Text)
	}
	bundle := filepath.Join(dir, "permit.mjs")
	if err := os.WriteFile(bundle, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}

	lines := []string{
		"Bash command",
		"  touch ~/permtest-tmux",
		"  Create test file in home directory",
		"Do you want to proceed?",
		"❯ Yes",
		"  No",
		"Esc to cancel · Tab to amend",
	}
	raw, err := json.Marshal(lines)
	if err != nil {
		t.Fatal(err)
	}

	script := `
import { Permit } from ` + jsString("file://"+bundle) + `;
import { __render, __forget } from ` + jsString("file://"+shimPath) + `;

function markup(node) {
    if (node == null || typeof node === "boolean") return "";
    if (typeof node !== "object") return String(node);
    if (Array.isArray(node)) return node.map(markup).join("");
    const { type, props } = node;
    if (typeof type === "function") return markup(type(props));
    const cls = props && typeof props.class === "string" ? ' class="' + props.class + '"' : "";
    return "<" + type + cls + ">" + markup(props && props.children) + "</" + type + ">";
}

const exec = { available: true, kinds: ["session.permit", "session.stop"] };

async function draw(permission) {
    globalThis.fetch = async () => new Response(JSON.stringify({ state: "ok", permission }), {
        status: 200, headers: { "Content-Type": "application/json" },
    });
    __forget();
    const props = { name: "aacpanel", exec, waitingFor: "dialog open", onAnswered: () => {} };
    __render(() => Permit(props));
    await new Promise((resolve) => setTimeout(resolve, 0));
    return markup(__render(() => Permit(props)));
}

const lines = ` + string(raw) + `;
process.stdout.write(JSON.stringify({
    withLines: await draw({ unknown: true, raw: lines }),
    noLines: await draw({ unknown: true, raw: [] }),
}));
`
	out, err := exec.Command(node, "--input-type=module", "-e", script).Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("the card did not render: %v\n%s", err, stderr)
	}
	var got renderedPermit
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	return got
}
