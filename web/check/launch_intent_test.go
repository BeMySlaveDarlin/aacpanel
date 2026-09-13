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

func TestEmptyIntentSurvivesFormCleanup(t *testing.T) {
	got := runLaunchJS(t)

	if _, ok := got.CleanEmptyIntent["intent"]; !ok {
		t.Errorf("an empty intent was dropped by the form cleanup: %v — there is nothing left to cancel the profile intent with",
			got.CleanEmptyIntent)
	}
	if _, ok := got.CleanEmptyModel["model"]; ok {
		t.Errorf("an empty model reached the database: %v — it overrides the profile model with emptiness",
			got.CleanEmptyModel)
	}
	if value, ok := got.MergedMuted["intent"]; !ok || value != "" {
		t.Errorf("the project could not cancel the profile intent: %v", got.MergedMuted)
	}
	if got.MergedInherited["intent"] != "/rs" {
		t.Errorf("a silent project did not inherit the profile intent: %v", got.MergedInherited)
	}
}

type launchJS struct {
	CleanEmptyIntent map[string]any `json:"cleanEmptyIntent"`
	CleanEmptyModel  map[string]any `json:"cleanEmptyModel"`
	MergedMuted      map[string]any `json:"mergedMuted"`
	MergedInherited  map[string]any `json:"mergedInherited"`
}

func runLaunchJS(t *testing.T) launchJS {
	t.Helper()
	var got launchJS
	launchNode(t, launchBundle(t, false), `
import { clean, merged } from BUNDLE;
process.stdout.write(JSON.stringify({
    cleanEmptyIntent: clean({ intent: "" }),
    cleanEmptyModel: clean({ model: "" }),
    mergedMuted: merged({ intent: "/rs" }, { intent: "" }),
    mergedInherited: merged({ intent: "/rs" }, { model: "opus" }),
}));
`, &got)
	return got
}

func TestIntentFieldRendersOnBothSides(t *testing.T) {
	got := renderLaunchJS(t)

	for _, want := range []string{"Starting intent", "from the profile: /rs"} {
		if !strings.Contains(got.FieldsInherited, want) {
			t.Errorf("the project form did not say %q:\n%s", want, got.FieldsInherited)
		}
	}
	if !strings.Contains(got.FieldsInherited, "do not send the profile intent") {
		t.Errorf("the project has no way to refuse the profile intent:\n%s", got.FieldsInherited)
	}
	if strings.Contains(got.FieldsOwn, "do not send the profile intent") {
		t.Errorf("the switch is shown while the field is filled in:\n%s", got.FieldsOwn)
	}
	if !strings.Contains(got.ViewMuted, "the profile intent is cancelled") {
		t.Errorf("a cancelled intent is shown as an empty spot instead of a decision:\n%s", got.ViewMuted)
	}
	if !strings.Contains(got.ViewInherited, "/rs") || !strings.Contains(got.ViewInherited, "from the profile") {
		t.Errorf("an inherited intent is shown without its origin:\n%s", got.ViewInherited)
	}
}

type renderedLaunch struct {
	FieldsInherited string `json:"fieldsInherited"`
	FieldsOwn       string `json:"fieldsOwn"`
	ViewMuted       string `json:"viewMuted"`
	ViewInherited   string `json:"viewInherited"`
}

func renderLaunchJS(t *testing.T) renderedLaunch {
	t.Helper()
	var got renderedLaunch
	launchNode(t, launchBundle(t, true), renderPrelude+`
const profile = { intent: "/rs" };
process.stdout.write(JSON.stringify({
    fieldsInherited: text(LaunchFields({ value: {}, onChange: () => {}, inherited: profile })),
    fieldsOwn: text(LaunchFields({ value: { intent: "run the checks" }, onChange: () => {}, inherited: profile })),
    viewMuted: text(LaunchView({ launch: { intent: "" }, profile })),
    viewInherited: text(LaunchView({ launch: {}, profile })),
}));
`, &got)
	return got
}

// renderPrelude flattens a rendered tree into the text a test can search: tag
// props as key=value, boolean and numeric ones included, so that a disabled
// field or a checked switch is visible to the test and not only to a browser.
const renderPrelude = `
import { LaunchFields, LaunchView } from BUNDLE;
function text(node) {
    if (node == null || typeof node === "boolean") return "";
    if (typeof node !== "object") return String(node);
    if (Array.isArray(node)) return node.map(text).join(" ");
    const { type, props } = node;
    const own = Object.entries(props || {})
        .filter(([k, v]) => k !== "children" && ["string", "number", "boolean"].includes(typeof v))
        .map(([k, v]) => k + "=" + v)
        .join(" ");
    const kids = text(props && props.children);
    if (typeof type === "function") return text(type(props));
    return own + " " + kids;
}
`

// launchBundle builds launch.js on its own for node; with stubHooks the preact
// hooks are replaced by stand-ins, so that a component can be called as a
// function outside a render.
func launchBundle(t *testing.T, stubHooks bool) string {
	t.Helper()

	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}

	dir := t.TempDir()
	if stubHooks {
		hooks := filepath.Join(dir, "hooks.mjs")
		if err := os.WriteFile(hooks, []byte(
			"export const useState = (init) => [typeof init === \"function\" ? init() : init, () => {}];\n"+
				"export const useEffect = () => {};\n"+
				"export const useRef = () => ({ current: null });\n"+
				"export const useMemo = (fn) => fn();\n"+
				"export const useCallback = (fn) => fn;\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		alias["preact/hooks"] = hooks
	}

	entry, err := filepath.Abs(filepath.Join(webDir, "src", "screens", "profiles", "launch.js"))
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
		t.Fatalf("launch.js did not build: %v", built.Errors[0].Text)
	}
	bundle := filepath.Join(dir, "launch.mjs")
	if err := os.WriteFile(bundle, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return bundle
}

// launchNode runs a script against the bundle in node, with BUNDLE standing
// for the bundle's file URL, and reads its JSON output into out.
func launchNode(t *testing.T, bundle, script string, out any) {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the form is run by the engine, not by reading the source")
	}
	script = strings.ReplaceAll(script, "BUNDLE", jsString("file://"+bundle))
	raw, err := exec.Command(node, "--input-type=module", "-e", script).Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("node: %v\n%s", err, stderr)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, raw)
	}
}
