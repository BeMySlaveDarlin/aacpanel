package check

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"aacpanel/internal/webbuild"

	esbuild "github.com/evanw/esbuild/pkg/api"
)

const settingsScreen = "src/screens/settings.js"
const settingsModel = "src/screens/settings/model.js"

const settingsSpecPath = "../internal/settings/settings.go"

type settingsItem struct {
	Key       string `json:"key"`
	Value     string `json:"value,omitempty"`
	Secret    bool   `json:"secret,omitempty"`
	Set       bool   `json:"set"`
	Source    string `json:"source"`
	Cost      string `json:"cost"`
	Group     string `json:"group"`
	FileValue string `json:"fileValue,omitempty"`
}

type settingsOut struct {
	Value []string `json:"value"`
	Tone  []string `json:"tone"`
	Clash []string `json:"clash"`
}

func TestSecretIsShownAsSetNotAsValue(t *testing.T) {
	cases := []struct {
		name  string
		in    settingsItem
		value string
		tone  string
		clash string
	}{
		{
			"a secret with a non-empty value is shown as a word",
			settingsItem{Key: "AACP_SECRET", Secret: true, Set: true, Value: "s3cret-in-the-open"},
			"set", "word", "",
		},
		{
			"a secret that is not set differs from one that is",
			settingsItem{Key: "AACP_TOKEN", Secret: true, Set: false},
			"not set", "word", "",
		},
		{
			"a clash on a secret is not shown either",
			settingsItem{Key: "AACP_TOKEN", Secret: true, Set: true, Value: "env-token", FileValue: "file-token"},
			"set", "word", "",
		},
		{
			"an ordinary setting is shown by its value",
			settingsItem{Key: "AACP_HOST", Set: true, Value: "workstation", Source: "env"},
			"workstation", "code", "",
		},
		{
			"a setting that is not set is named by a word, not by emptiness",
			settingsItem{Key: "DISPLAY", Set: false, Source: "default"},
			"not set", "word", "",
		},
		{
			"a clash between the file and the environment is shown",
			settingsItem{Key: "AACP_LANG", Set: true, Value: "ru_RU.UTF-8", Source: "env", FileValue: "en_US.UTF-8"},
			"ru_RU.UTF-8", "code", "en_US.UTF-8",
		},
	}

	ins := make([]settingsItem, 0, len(cases))
	for _, c := range cases {
		ins = append(ins, c.in)
	}
	got := runSettingsModelJS(t, ins)

	for i, c := range cases {
		if got.Value[i] != c.value {
			t.Errorf("%s: valueText = %q, expected %q", c.name, got.Value[i], c.value)
		}
		if got.Tone[i] != c.tone {
			t.Errorf("%s: valueTone = %q, expected %q", c.name, got.Tone[i], c.tone)
		}
		if got.Clash[i] != c.clash {
			t.Errorf("%s: clash = %q, expected %q", c.name, got.Clash[i], c.clash)
		}
	}
}

func TestSettingsMarkupNeverTouchesTheValueItself(t *testing.T) {
	body := settingsSrc(t, settingsScreen)

	for _, forbidden := range []string{"item.value", "item.fileValue", ".secret"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("%s: the markup touches %s itself — the value of a secret reaches the screen past "+
				"the rule that lives in the model", settingsScreen, forbidden)
		}
	}

	for _, required := range []string{"valueText(item)", "valueTone(item)", "clash(item)"} {
		if !strings.Contains(body, required) {
			t.Errorf("%s: the markup has no %s — so it prints something else", settingsScreen, required)
		}
	}
}

func TestSettingsScreenOnlyShows(t *testing.T) {
	body := settingsSrc(t, settingsScreen)

	for _, mark := range []string{"<input", "<textarea", "<select", "useAction", "onSubmit"} {
		if strings.Contains(body, mark) {
			t.Errorf("%s: %s has appeared on the screen — the page only shows, and the cost of editing "+
				"settings varies too much for them all to be pressed the same way", settingsScreen, mark)
		}
	}
}

func TestEveryEditCostIsSpelledOut(t *testing.T) {
	spec := settingsSrc(t, settingsSpecPath)
	model := settingsSrc(t, settingsModel)

	server := map[string]bool{}
	for _, m := range regexp.MustCompile(`Cost\w+\s+Cost\s+= "(\w+)"`).FindAllStringSubmatch(spec, -1) {
		server[m[1]] = true
	}
	if len(server) == 0 {
		t.Fatalf("%s: the edit costs are not found — the test is useless", settingsSpecPath)
	}

	type words struct{ text, tone string }
	front := map[string]words{}
	for _, m := range regexp.MustCompile(`(\w+): \{ text: "([^"]+)", tone: "(\w+)" \}`).FindAllStringSubmatch(model, -1) {
		front[m[1]] = words{text: m[2], tone: m[3]}
	}

	for cost := range server {
		got, ok := front[cost]
		if !ok {
			t.Errorf("cost %q exists on the server, but has no words — on the screen it shows as a key", cost)
			continue
		}
		if !strings.Contains(got.text, " ") || strings.EqualFold(got.text, cost) {
			t.Errorf("cost %q is captioned as %q — that is not a phrase but the same code in other letters", cost, got.text)
		}
	}
	for cost := range front {
		if !server[cost] {
			t.Errorf("cost %q is captioned on the screen, but the server never sends it — the caption is never shown", cost)
		}
	}

	for _, pair := range [][2]string{{"live", "recreate"}, {"live", "never"}, {"recreate", "never"}} {
		a, b := front[pair[0]], front[pair[1]]
		if a.tone == "" || b.tone == "" {
			continue
		}
		if a.tone == b.tone {
			t.Errorf("%q and %q are painted the same (%q) — the difference between \"takes effect at once\" "+
				"and \"does not take effect at all\" disappears from the screen", pair[0], pair[1], a.tone)
		}
	}

	screen := settingsSrc(t, settingsScreen)
	if !strings.Contains(screen, "${cost.text}") {
		t.Errorf("%s: the words about the edit cost are not printed", settingsScreen)
	}
	if !strings.Contains(screen, "setcost ${cost.tone}") {
		t.Errorf("%s: the edit cost is printed without its color — an expensive one looks like a cheap one", settingsScreen)
	}
}

func TestSettingsGroupsMatchTheServer(t *testing.T) {
	spec := settingsSrc(t, settingsSpecPath)
	model := settingsSrc(t, settingsModel)

	front := map[string]bool{}
	for _, m := range regexp.MustCompile(`id: "(\w+)", title:`).FindAllStringSubmatch(model, -1) {
		front[m[1]] = true
	}
	if len(front) == 0 {
		t.Fatalf("%s: the groups are not found — the test is useless", settingsModel)
	}

	for _, m := range regexp.MustCompile(`group\w+\s+= "(\w+)"`).FindAllStringSubmatch(spec, -1) {
		if !front[m[1]] {
			t.Errorf("group %q exists on the server, but the screen has neither a title nor an order for it — "+
				"its settings land in \"Other\" with no heading", m[1])
		}
	}
}

func settingsSrc(t *testing.T, path string) string {
	t.Helper()

	raw, err := os.ReadFile(webPath(path))
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	body := string(raw)
	if strings.TrimSpace(body) == "" {
		t.Fatalf("%s is empty — the test is useless", path)
	}
	body = regexp.MustCompile(`(?s)<!--.*?-->`).ReplaceAllString(body, "")
	return withoutComments(body)
}

func runSettingsModelJS(t *testing.T, items []settingsItem) settingsOut {
	t.Helper()

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the settings screen model is run by the engine, not by reading the source")
	}
	root, err := webbuild.FindRoot(".")
	if err != nil {
		t.Fatalf("repository root: %v", err)
	}
	alias, err := webbuild.Aliases(root)
	if err != nil {
		t.Fatalf("map of vendored libraries: %v", err)
	}
	entry, err := filepath.Abs(webPath(settingsModel))
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
		t.Fatalf("the settings model did not build: %v", built.Errors[0].Text)
	}

	dir := t.TempDir()
	bundle := filepath.Join(dir, "settings-model.mjs")
	if err := os.WriteFile(bundle, built.OutputFiles[0].Contents, 0o600); err != nil {
		t.Fatal(err)
	}

	in, err := json.Marshal(items)
	if err != nil {
		t.Fatal(err)
	}

	script := `
import { clash, valueText, valueTone } from ` + jsString("file://"+bundle) + `;

const items = ` + string(in) + `;
process.stdout.write(JSON.stringify({
    value: items.map(valueText),
    tone: items.map(valueTone),
    clash: items.map(clash),
}));
`
	path := filepath.Join(dir, "run.mjs")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := exec.Command(node, path).Output()
	if err != nil {
		t.Fatalf("node did not finish: %v: %s", err, out)
	}
	var got settingsOut
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("the node output did not parse: %v: %s", err, out)
	}
	if len(got.Value) != len(items) {
		t.Fatalf("%d answers to %d cases", len(got.Value), len(items))
	}
	return got
}
