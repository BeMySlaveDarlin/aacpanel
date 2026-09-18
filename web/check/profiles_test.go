package check

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"aacpanel/internal/host"
)

const profilesFile = "src/screens/profiles.js"

func TestEveryMapActionReachableFromProfilesScreen(t *testing.T) {
	registry := srcFiles(t)[registryFile]
	if registry == "" {
		t.Fatalf("%s not found", registryFile)
	}
	body := actionsBlock(t, registry)
	ids := regexp.MustCompile(`(?m)^\s{4}"((?:profile|group|project|disk)\.[a-zA-Z]+)":`).FindAllStringSubmatch(body, -1)
	if len(ids) == 0 {
		t.Fatal("not a single map action found in the registry — the check guards the wrong place")
	}

	screen := screenSrc(t, profilesFile)
	for _, m := range ids {
		if strings.Contains(screen, `"`+m[1]+`"`) {
			continue
		}
		t.Errorf("action %q is in the registry, but nobody calls it on the map screen — "+
			"from a phone it cannot be done", m[1])
	}
}

func TestProfileMapRowsCarryTheirOwnActions(t *testing.T) {
	files := srcFiles(t)
	rows := map[string]struct{ plus, pencil bool }{
		"src/screens/profiles/page.js":    {plus: true, pencil: true},
		"src/screens/profiles/group.js":   {plus: true, pencil: true},
		"src/screens/profiles/project.js": {plus: false, pencil: true},
	}
	for path, want := range rows {
		body := files[path]
		if body == "" {
			t.Fatalf("%s not found — the map row has moved, the test looks for it in the wrong place", path)
		}
		if got := strings.Contains(body, "Icon.pencil()"); got != want.pencil {
			t.Errorf("%s: the pencil on the row is %v, expected %v — there is nowhere to edit the row", path, got, want.pencil)
		}
		if got := strings.Contains(body, "Icon.plus()"); got != want.plus {
			t.Errorf("%s: the plus on the row is %v, expected %v — a nested entry is created from the row "+
				"it will land in", path, got, want.plus)
		}
	}

	screen := files[profilesFile]
	if !strings.Contains(screen, `class="pffab"`) {
		t.Errorf("%s: there is no button for creating a contour — on a host with no map there will be "+
			"nothing to create the first contour with", profilesFile)
	}
}

func TestProfileEditIsALayerNotASheet(t *testing.T) {
	screen := screenSrc(t, profilesFile)
	if strings.Contains(screen, "ui/sheet.js") {
		t.Errorf("%s: the map screen opens a sheet again — the edit form lives as a layer", profilesFile)
	}
	forms := srcFiles(t)["src/screens/profiles/forms.js"]
	if !strings.Contains(forms, "useBackClose(") || !strings.Contains(forms, "<${BackHead}") {
		t.Errorf("src/screens/profiles/forms.js: the edit layer has no header with an arrow or no " +
			"back handler")
	}
	if !strings.Contains(forms, `class="pfdanger"`) {
		t.Errorf("src/screens/profiles/forms.js: there is no delete on the edit page")
	}
	if strings.Contains(forms, "btn danger") {
		t.Errorf("src/screens/profiles/forms.js: delete is a red button in the common row again — " +
			"it turns red in the confirmation sheet, not next to Save")
	}
}

func TestProfileMapHasOneFormAndOneState(t *testing.T) {
	files := srcFiles(t)

	declares := func(needle string) []string {
		out := []string{}
		for _, path := range sortedKeys(files) {
			if strings.Contains(files[path], needle) {
				out = append(out, path)
			}
		}
		return out
	}

	if got := declares("export function EditLayer("); len(got) != 1 {
		t.Errorf("the map edit form is declared in %v — a second one drifts from the first on the first edit", got)
	}
	if got := declares("export function useProfileMap("); len(got) != 1 {
		t.Errorf("the map state is declared in %v — a second copy shows a map that no longer exists", got)
	}

	for _, path := range sortedKeys(files) {
		if strings.HasPrefix(path, "src/screens/profiles") {
			continue
		}
		if strings.Contains(files[path], `class="pffield"`) {
			t.Errorf("%s: starts its own fields for the map form — the form lives on the shared screen", path)
		}
	}

	for _, path := range sortedKeys(files) {
		if strings.Contains(files[path], "export function useProfileMap(") {
			continue
		}
		if !strings.Contains(files[path], "useProfileMap()") {
			continue
		}
		if !strings.Contains(files[path], "<${EditLayer}") {
			t.Errorf("%s: holds the map but edits it with something other than the shared form", path)
		}
	}
}

func TestLooseScreenSendsOnlyThroughGate(t *testing.T) {
	files := srcFiles(t)
	loose := files["src/screens/profiles/loose.js"]
	if loose == "" {
		t.Fatal("there is no triage screen: src/screens/profiles/loose.js")
	}
	if !strings.Contains(loose, `run("project.add"`) {
		t.Errorf("the triage screen does not create a project through the gate — create either does " +
			"not work or goes its own way")
	}
	if strings.Contains(loose, "fetch(") {
		t.Errorf("the triage screen goes to the server itself, past the gate")
	}
	pick := files["src/screens/profiles/pick.js"]
	if pick == "" {
		t.Fatal("there is no pure map logic: src/screens/profiles/pick.js")
	}
	if strings.Contains(pick, "import ") {
		t.Errorf("src/screens/profiles/pick.js: an import showed up — the module stops running " +
			"under the engine, and the ownership logic gets checked by eye again")
	}
}

func TestProfileScreenReadsOnlyFieldsTheAPISends(t *testing.T) {
	const deskFile = "src/desktop/panels/projects.js"
	desk := srcFiles(t)[deskFile]
	if desk == "" {
		t.Fatalf("%s not found — the desktop layout of the map has moved", deskFile)
	}
	screen := stripComments(screenSrc(t, profilesFile) + "\n" + desk)
	for _, level := range []struct{ variable, structure string }{
		{"profile", "Profile"},
		{"group", "ProfileGroup"},
		{"project", "ProfileProject"},
	} {
		known := jsonTags(t, level.structure)
		seen := regexp.MustCompile(`(^|[^\w"'`+"`"+`/])`+level.variable+
			`\.([a-zA-Z][a-zA-Z0-9]*)`).FindAllStringSubmatch(screen, -1)
		if len(seen) == 0 {
			t.Fatalf("the screen reads no field of %s at all — the check guards the wrong place", level.variable)
		}
		for _, m := range seen {
			if known[m[2]] {
				continue
			}
			t.Errorf("the map screen reads %s.%s, but the handler does not return such a field (store.%s): "+
				"undefined stands in place of the value, and the screen lies silently",
				level.variable, m[2], level.structure)
		}
	}
}

func jsonTags(t *testing.T, name string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(repoPath("internal/store/profiles.go"))
	if err != nil {
		t.Fatalf("reading the map structures: %v", err)
	}
	block := regexp.MustCompile(`(?s)type ` + name + ` struct \{(.*?)\n\}`).FindStringSubmatch(string(raw))
	if block == nil {
		t.Fatalf("struct %s is not found — the map has moved, the test looks for it in the wrong place", name)
	}
	out := map[string]bool{}
	for _, m := range regexp.MustCompile("json:\"([a-zA-Z]+)").FindAllStringSubmatch(block[1], -1) {
		out[m[1]] = true
	}
	if len(out) == 0 {
		t.Fatalf("no json field found on %s", name)
	}
	return out
}

func TestDeleteButtonSaysWhatTheSheetWillSay(t *testing.T) {
	registry := srcFiles(t)[registryFile]
	forms := srcFiles(t)["src/screens/profiles/forms.js"]
	if registry == "" || forms == "" {
		t.Fatal("the action registry or the edit form is not found")
	}
	block := regexp.MustCompile(`(?s)const DANGER = \{(.*?)\n\};`).FindStringSubmatch(forms)
	if block == nil {
		t.Fatal("no DANGER found in forms.js — the test guards the wrong place")
	}
	found := regexp.MustCompile(`(?m)^\s+([a-z]+): "([^"]+)"`).FindAllStringSubmatch(block[1], -1)
	if len(found) != 3 {
		t.Fatalf("%d delete levels, expected three (contour, group, project)", len(found))
	}
	for _, m := range found {
		want := regexp.MustCompile(`"` + m[1] + `\.remove":\s*"([^"]+)"`).FindStringSubmatch(registry)
		if want == nil {
			t.Errorf("the registry has no label for %s.remove — the button names an action that does not exist", m[1])
			continue
		}
		if m[2] != want[1] {
			t.Errorf("the button says %q while the confirmation sheet will say %q — that reads as "+
				"two different deletions in a row", m[2], want[1])
		}
	}
}

func TestProjectMoveRidesTheEditAction(t *testing.T) {
	forms := srcFiles(t)["src/screens/profiles/forms.js"]
	if forms == "" {
		t.Fatal("src/screens/profiles/forms.js not found")
	}
	body := jsBlock(t, "src/screens/profiles/forms.js", forms, "function ProjectForm(")

	if !strings.Contains(body, "groupId: Number(group)") {
		t.Error("the project form does not put the chosen group into the body — the server never learns about the move")
	}
	if !strings.Contains(body, "editing && group ? { groupId:") {
		t.Error("the group goes into the body on creation too: there the handler address names it, and " +
			"two names for one group drift apart on the very first edit")
	}
	if !strings.Contains(body, "moveTo") {
		t.Error("the confirmation sheet never learns about the move — it will name only the save")
	}

	if !strings.Contains(body, "form.profile.groups") {
		t.Error("the group list in the form is built from something other than the contour of the project")
	}

	code := stripComments(body)
	for _, gone := range []string{
		"moveProfile", "moveContour",
		"toProfile", "toContour",
		"fromProfile", "fromContour",
		"optgroup",
	} {
		if strings.Contains(code, gone) {
			t.Errorf("the project form still knows about moving into a foreign contour (%q): "+
				"the record on the map moves while the sessions, the launch history and the transcripts "+
				"stay in the previous contour", gone)
		}
	}

	registry := stripComments(srcFiles(t)[registryFile])
	effect := after(registry, `"project.edit":`)
	if !strings.Contains(effect[:min(len(effect), 600)], "params.moveTo\n") &&
		!strings.Contains(effect[:min(len(effect), 600)], "params.moveTo ?") &&
		!strings.Contains(effect[:min(len(effect), 600)], "(params.moveTo") {
		t.Error("the project.edit sheet does not branch on moveTo — the move stays unnamed")
	}
}

func TestGroupMoveSpeaksOneWordAboutTheProfile(t *testing.T) {
	const file = "src/screens/profiles/forms.js"
	forms := srcFiles(t)[file]
	if forms == "" {
		t.Fatalf("%s not found", file)
	}
	code := stripComments(jsBlock(t, file, forms, "function GroupForm("))

	if !strings.Contains(code, "moveProfile: true") {
		t.Error("the group form sends the consent to move under a different word than the screen uses for the profile")
	}
	for _, param := range []string{"toProfile", "fromProfile:"} {
		if !strings.Contains(code, param) {
			t.Errorf("the group form does not put %q into the action parameters — the sheet has nothing to name the move with", param)
		}
	}

	registry := stripComments(srcFiles(t)[registryFile])
	effect := after(registry, `"group.edit":`)
	if effect == "" {
		t.Fatal("the registry has no group.edit — the test guards the wrong place")
	}
	for _, param := range []string{"params.toProfile", "params.fromProfile"} {
		if !strings.Contains(effect, param) {
			t.Errorf("the group.edit sheet does not read %q — an empty space will name the price of the move", param)
		}
	}
}

func TestContourBinaryRidesTheProfileForm(t *testing.T) {
	const file = "src/screens/profiles/forms.js"
	forms := srcFiles(t)[file]
	if forms == "" {
		t.Fatalf("%s not found", file)
	}
	body := jsBlock(t, file, forms, "function ProfileForm(")

	if !strings.Contains(body, `useState(was.claudeBin || "")`) {
		t.Error("the contour form does not read claudeBin from the record — it shows the path set on " +
			"the host as empty and clears it on save")
	}

	const only = `...((editing ? binNow !== binWas : binNow !== "") ? { claudeBin: binNow } : {})`
	if !strings.Contains(body, only) {
		t.Error("claudeBin goes out not only when edited — the pointer on the server tells " +
			"do not touch from clear the path, and the form has to tell them apart too")
	}
	if n := strings.Count(body, "claudeBin: binNow"); n != 1 {
		t.Errorf("claudeBin is put into the body %d times — a second place goes around the condition", n)
	}

	if !strings.Contains(body, "${binNow") {
		t.Error("the field hint does not depend on whether the path is set — an empty value " +
			"is left unexplained")
	}
	for _, word := range []string{"PATH", "personal account"} {
		if !strings.Contains(body, word) {
			t.Errorf("the hint on an empty field says nothing about %q — a dash instead of a price", word)
		}
	}
}

func TestHiddenDirsCanComeBack(t *testing.T) {
	loose := srcFiles(t)["src/screens/profiles/loose.js"]
	if loose == "" {
		t.Fatal("there is no triage screen: src/screens/profiles/loose.js")
	}
	if !strings.Contains(loose, `run("disk.hide"`) {
		t.Error("on the triage screen there is nothing to take a directory out of the queue with — " +
			"nothing clears it but creating a project")
	}
	if !strings.Contains(loose, `run("disk.show"`) {
		t.Error("what is hidden cannot be taken back: a list you cannot pull the hidden out of " +
			"will one day hide something needed")
	}
	if !strings.Contains(loose, "hiddenOf(") {
		t.Error("the hidden list is built by a different rule than the queue — a directory " +
			"disappears from both lists at once")
	}

	pick := stripComments(srcFiles(t)["src/screens/profiles/pick.js"])
	for _, fn := range []string{"export function looseOf(", "export function hiddenOf("} {
		block := pick[strings.Index(pick, fn):]
		if !strings.Contains(block[:min(len(block), 400)], "contourOf(") {
			t.Errorf("%s cuts the list by something other than the contour — the two lists drift apart", fn)
		}
	}
}

func TestHiddenBelongsToTheMachineNotTheContour(t *testing.T) {
	registry := stripComments(srcFiles(t)[registryFile])
	for _, id := range []string{"disk.hide", "disk.show"} {
		block := after(registry, `"`+id+`":`)
		head := block[:min(len(block), 400)]
		if !strings.Contains(head, "path: target") && !strings.Contains(head, "{ path: target }") {
			t.Errorf("%s sends more than one path — something else showed up in the body", id)
		}
		for _, extra := range []string{"profile", "contour", "groupId"} {
			if strings.Contains(head, extra) {
				t.Errorf("%s carries %q: what is hidden belongs to the machine, not to the contour — "+
					"the contour of a found directory follows from the prefixes, and those get edited", id, extra)
			}
		}
	}
}

func TestContourOfFollowsLongestPrefix(t *testing.T) {
	profiles := []any{
		map[string]any{"name": "personal", "prefix": ""},
		map[string]any{"name": "work", "prefix": "/srv/proj/Labs"},
		map[string]any{"name": "work-shop", "prefix": "/srv/proj/Labs/shop"},
	}
	cases := []struct {
		name, path, want string
	}{
		{"no prefix — the personal one", "/srv/proj/Beta/aacpanel", "personal"},
		{"its own prefix", "/srv/proj/Labs/other", "work"},
		{"a long prefix beats a short one", "/srv/proj/Labs/shop/api", "work-shop"},
		{"the prefix directory itself", "/srv/proj/Labs", "work"},
		{"a similar neighbour does not count as its own", "/srv/proj/Labs-2/x", "personal"},
		{"a trailing slash in the prefix does not get in the way", "/home/u/lab", "personal"},
	}
	calls := make([][]any, 0, len(cases))
	for _, c := range cases {
		calls = append(calls, []any{c.path, profiles})
	}
	got := runPickJS(t, "contourOf", calls)
	for i, c := range cases {
		if s, _ := got[i].(string); s != c.want {
			t.Errorf("%s: contourOf(%q) = %v, expected %q", c.name, c.path, got[i], c.want)
		}
	}
}

func TestContourOfFallsBackWhenNothingMatches(t *testing.T) {
	profiles := []any{
		map[string]any{"name": "work", "prefix": "/srv/proj/Labs"},
		map[string]any{"name": "acme", "prefix": "/srv/proj/Acme"},
	}
	got := runPickJS(t, "contourOf", [][]any{
		{"/home/u/lab", profiles},
		{"/home/u/lab", []any{}},
	})
	if s, _ := got[0].(string); s != "work" {
		t.Errorf("a map of work contours only: the directory went to %v, expected the first contour", got[0])
	}
	if s, _ := got[1].(string); s != "" {
		t.Errorf("an empty map: contour %v, expected an empty string", got[1])
	}
}

func TestLooseOfStaysSilentWhenDiskIsUnknown(t *testing.T) {
	profiles := []any{
		map[string]any{"name": "personal", "prefix": ""},
		map[string]any{"name": "work", "prefix": "/srv/proj/Labs"},
	}
	ok := map[string]any{"state": "ok", "dirs": []any{
		map[string]any{"path": "/srv/proj/Beta/one"},
		map[string]any{"path": "/srv/proj/Labs/two"},
	}}
	unknown := map[string]any{"state": "unknown", "dirs": []any{
		map[string]any{"path": "/srv/proj/Beta/one"},
	}}
	got := runPickJS(t, "looseOf", [][]any{
		{ok, profiles, "personal"},
		{ok, profiles, "work"},
		{unknown, profiles, "personal"},
		{nil, profiles, "personal"},
	})
	for i, want := range []int{1, 1, 0, 0} {
		list, _ := got[i].([]any)
		if len(list) != want {
			t.Errorf("looseOf #%d: %d directories, expected %d", i, len(list), want)
		}
	}
}

func TestGuessGroupOnlyGuessesWhenNeighboursAgree(t *testing.T) {
	roots := []any{"/srv/proj"}
	profile := map[string]any{"groups": []any{
		map[string]any{"id": 1, "projects": []any{
			map[string]any{"path": "/srv/proj/Beta/one"},
			map[string]any{"path": "/srv/proj/Beta/two"},
		}},
		map[string]any{"id": 2, "projects": []any{
			map[string]any{"path": "/srv/proj/Beta/three"},
			map[string]any{"path": "/srv/proj/Notes/four"},
		}},
	}}
	got := runPickJS(t, "guessGroup", [][]any{
		{profile, "/srv/proj/Beta/new", roots},
		{profile, "/srv/proj/Notes/new", roots},
		{profile, "/srv/proj/Plays/new", roots},
	})
	want := []string{"1", "2", ""}
	for i := range want {
		if s, _ := got[i].(string); s != want[i] {
			t.Errorf("guessGroup #%d = %v, expected %q", i, got[i], want[i])
		}
	}
}

func TestFilterGroupsKeepsWholeGroupByName(t *testing.T) {
	groups := []any{
		map[string]any{"name": "Services", "projects": []any{
			map[string]any{"name": "aacpanel", "path": "/srv/proj/Beta/service/aacpanel"},
			map[string]any{"name": "proxy", "path": "/srv/proj/Beta/service/proxy"},
		}},
		map[string]any{"name": "Texts", "projects": []any{
			map[string]any{"name": "book2", "path": "/srv/proj/Notes/book2"},
		}},
	}
	got := runPickJS(t, "filterGroups", [][]any{
		{groups, ""},
		{groups, "service"},
		{groups, "aacpanel"},
		{groups, "/Notes/"},
		{groups, "nosuchthing"},
	})

	whole, _ := got[0].([]any)
	if len(whole) != 2 {
		t.Errorf("an empty query: %d groups, expected 2 — the search must hide nothing", len(whole))
	}
	byName, _ := got[1].([]any)
	if len(byName) != 1 || len(projectsOf(byName[0])) != 2 {
		t.Errorf("a match on the group name has to keep it whole, not as an empty shelf: %v", got[1])
	}
	byProject, _ := got[2].([]any)
	if len(byProject) != 1 || len(projectsOf(byProject[0])) != 1 {
		t.Errorf("a match on a project has to keep only the rows that matched: %v", got[2])
	}
	byPath, _ := got[3].([]any)
	if len(byPath) != 1 || len(projectsOf(byPath[0])) != 1 {
		t.Errorf("the path is searched alongside the name — projects are named short and alike: %v", got[3])
	}
	if none, _ := got[4].([]any); len(none) != 0 {
		t.Errorf("nothing found — it has to stay empty, not a list of headings: %v", got[4])
	}
}

func projectsOf(group any) []any {
	m, _ := group.(map[string]any)
	list, _ := m["projects"].([]any)
	return list
}

func TestContourAuthWordsMatchTheHost(t *testing.T) {
	pick := srcFiles(t)["src/screens/profiles/pick.js"]
	if pick == "" {
		t.Fatal("src/screens/profiles/pick.js not found")
	}
	for _, value := range []string{host.AuthBuiltin, host.AuthToken, host.AuthMissing} {
		if strings.Contains(pick, `"`+value+`"`) {
			continue
		}
		t.Errorf("the panel does not know the authorization state %q while the host returns it — "+
			"the contour shows an unknown authorization where it is known", value)
	}
}

func TestGroupDotWarnsAboutMissingDirectory(t *testing.T) {
	full := map[string]any{"projects": []any{
		map[string]any{"id": 1}, map[string]any{"id": 2},
	}}
	empty := map[string]any{"projects": []any{}}
	set := func(ids ...int) map[string]any {
		if ids == nil {
			ids = []int{}
		}
		return map[string]any{"__set": ids}
	}

	got := runPickJS(t, "dotOf", [][]any{
		{full, set()},
		{full, set(2)},
		{full, set(7)},
		{empty, set()},
		{empty, set(1)},
	})
	want := []string{"ok", "warn", "ok", "off", "off"}
	for i := range want {
		if s, _ := got[i].(string); s != want[i] {
			t.Errorf("dotOf #%d = %v, expected %q", i, got[i], want[i])
		}
	}
}

func TestAuthStateNeverInventsAnAnswer(t *testing.T) {
	got := runPickJS(t, "authState", [][]any{
		{map[string]any{"auth": host.AuthBuiltin}},
		{map[string]any{"auth": host.AuthToken}},
		{map[string]any{"auth": host.AuthMissing}},
		{map[string]any{}},
		{map[string]any{"auth": "nothing-like-it"}},
	})
	want := []string{"ok", "ok", "warn", "off", "off"}
	for i := range want {
		state, _ := got[i].(map[string]any)
		if dot, _ := state["dot"].(string); dot != want[i] {
			t.Errorf("authState #%d: dot %v, expected %q", i, state["dot"], want[i])
		}
	}
}

func TestHiddenOfSplitsByTheSameRuleAsTheQueue(t *testing.T) {
	profiles := []any{
		map[string]any{"name": "personal", "prefix": ""},
		map[string]any{"name": "work", "prefix": "/srv/proj/Labs"},
	}
	disk := map[string]any{"state": "ok", "hidden": []any{
		"/srv/proj/Storage/old",
		"/srv/proj/Labs/junk",
	}}
	got := runPickJS(t, "hiddenOf", [][]any{
		{disk, profiles, "personal"},
		{disk, profiles, "work"},
		{map[string]any{"state": "ok"}, profiles, "personal"},
		{nil, profiles, "personal"},
	})
	want := [][]string{{"/srv/proj/Storage/old"}, {"/srv/proj/Labs/junk"}, {}, {}}
	for i := range want {
		list, _ := got[i].([]any)
		if len(list) != len(want[i]) {
			t.Errorf("hiddenOf #%d: %v, expected %v", i, got[i], want[i])
			continue
		}
		for j := range want[i] {
			if s, _ := list[j].(string); s != want[i][j] {
				t.Errorf("hiddenOf #%d: %v, expected %v", i, got[i], want[i])
			}
		}
	}
}

func runPickJS(t *testing.T, fn string, calls [][]any) []any {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: the ownership logic is run through the engine, not read out of the source")
	}
	path, err := filepath.Abs(filepath.Join(webDir, "src", "screens", "profiles", "pick.js"))
	if err != nil {
		t.Fatal(err)
	}
	script := `
import { readFileSync } from "node:fs";
import * as pick from ` + jsString("file://"+path) + `;
const revive = (v) => (v && typeof v === "object" && Array.isArray(v.__set)) ? new Set(v.__set) : v;
const calls = JSON.parse(readFileSync(0, "utf8"));
const fn = pick[` + jsString(fn) + `];
if (!fn) throw new Error("no such function: " + ` + jsString(fn) + `);
process.stdout.write(JSON.stringify(calls.map((args) => fn(...args.map(revive)))));
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

func TestPermitCardShowsWhatIsBeingAllowed(t *testing.T) {
	card := srcFiles(t)["src/screens/chat/permit.js"]
	if card == "" {
		t.Fatal("there is no permission card: src/screens/chat/permit.js")
	}
	if !strings.Contains(card, "perm.action") {
		t.Error("the card does not show what exactly is being allowed — a person presses blind")
	}
	if !strings.Contains(card, "o.lasting") || !strings.Contains(card, `class="permitlast">from now on`) {
		t.Error("the card does not tell allow now from allow from now on — " +
			"from a phone that is one touch with a different price for a mistake")
	}
	if !strings.Contains(card, "perm.partial") {
		t.Error("the card says nothing about an unparsed dialog line — not everything is visible, " +
			"yet it looks like the whole choice")
	}
	if !strings.Contains(card, "perm.fingerprint") {
		t.Error("the press goes out without the dialog fingerprint — there will be nothing to check it against")
	}
	if !strings.Contains(card, `run("session.permit"`) {
		t.Error("the card does not send the press through the gate")
	}
}

func TestPermitAsksTheHostOnDemand(t *testing.T) {
	card := srcFiles(t)["src/screens/chat/permit.js"]
	if !strings.Contains(card, "/api/session/permission") {
		t.Fatal("the card does not ask the host for the dialog")
	}
	if strings.Contains(card, "setInterval") || strings.Contains(card, "setTimeout") {
		t.Error("the card polls the host on a timer — that is load on the very machine being watched")
	}

	for path, body := range srcFiles(t) {
		if strings.HasSuffix(path, "chat/permit.js") {
			continue
		}
		if strings.Contains(stripComments(body), "permission") && strings.Contains(body, "snapshot") {
			t.Errorf("%s: the permission is read from the shared snapshot — the session screen would be "+
				"captured on every collection cycle", path)
		}
	}
}

func TestCascadeSaysWhatItTakesAndWhatItLeaves(t *testing.T) {
	registry := srcFiles(t)[registryFile]
	screen := screenSrc(t, profilesFile)
	if registry == "" {
		t.Fatal("the action registry is not found")
	}

	drop := regexp.MustCompile(`(?s)function drop\(run, spec\) \{.*?\n\}`).FindString(screen)
	if drop == "" {
		t.Fatal("no drop() found on the map screen — the test guards the wrong place")
	}
	if strings.Contains(drop, "cascade: true") {
		t.Error("the cascade is asked for unconditionally: the sheet will say an empty group goes, and projects go instead")
	}
	if n := strings.Count(drop, "cascade: projects > 0"); n != 2 {
		t.Errorf("the cascade follows from the number of projects in %d cases out of two (contour and group)", n)
	}
	if strings.Contains(drop, `run("project.remove"`) && strings.Contains(
		drop[strings.Index(drop, `run("project.remove"`):], "cascade") {
		t.Error("a project has no cascade — nothing lies inside it, and the flag is out of place there")
	}

	for _, id := range []string{"profile.remove", "group.remove"} {
		block := mustAction(t, registry, id)
		if !strings.Contains(block, "?cascade=1") {
			t.Errorf("%s cannot do a cascade: a non-empty node stays undeletable", id)
			continue
		}
		if !strings.Contains(block, "params.cascade") {
			t.Errorf("%s sends the cascade past the consent — the flag has to arrive as a parameter", id)
		}
		if !strings.Contains(block, "count(params.projects") {
			t.Errorf("%s does not name how many projects will go", id)
		}
	}

	for _, id := range []string{"profile.remove", "group.remove", "project.remove"} {
		block := mustAction(t, registry, id)
		if !strings.Contains(block, "KEPT") && !strings.Contains(block, "stays where it is") {
			t.Errorf("%s does not promise that the directories on the disk stay — that is the one thing "+
				"a person does not expect from a delete button", id)
		}
	}
	if kept := regexp.MustCompile(`(?s)const KEPT = .*?;`).FindString(registry); !strings.Contains(kept, "on disk") ||
		!strings.Contains(kept, "consoles") {
		t.Error("KEPT stopped speaking about the disk and the live consoles — the promise on the sheet is gone")
	}
}

func mustAction(t *testing.T, registry, id string) string {
	t.Helper()
	block := actionBlock(registry, id)
	if block == "" {
		t.Fatalf("the registry has no action %q — the test guards the wrong place", id)
	}
	return block
}
