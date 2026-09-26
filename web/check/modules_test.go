package check

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestEveryNamedImportHasItsExport(t *testing.T) {
	files := srcFiles(t)

	decl := regexp.MustCompile(`^export\s+(?:async\s+)?(?:function\*?|const|let|var|class)\s+([A-Za-z_$][\w$]*)`)
	list := regexp.MustCompile(`^export\s+\{([^}]*)\}`)
	exports := map[string]map[string]bool{}
	for path, body := range files {
		names := map[string]bool{}
		for _, line := range strings.Split(withoutComments(body), "\n") {
			if m := decl.FindStringSubmatch(line); m != nil {
				names[m[1]] = true
			}
			if m := list.FindStringSubmatch(line); m != nil {
				for _, item := range strings.Split(m[1], ",") {
					parts := strings.Fields(item)
					if len(parts) == 0 {
						continue
					}
					names[parts[len(parts)-1]] = true
				}
			}
		}
		exports[path] = names
	}

	imp := regexp.MustCompile(`^import\s+\{([^}]*)\}\s+from\s+"(\.{1,2}/[^"]+)"`)
	checked := 0
	for _, path := range sortedKeys(files) {
		for _, line := range strings.Split(withoutComments(files[path]), "\n") {
			m := imp.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			target := filepath.ToSlash(filepath.Join(filepath.Dir(path), m[2]))
			have, ok := exports[target]
			if !ok {
				t.Errorf("%s imports %q — there is no such module (%s)", path, m[2], target)
				continue
			}
			for _, item := range strings.Split(m[1], ",") {
				parts := strings.Fields(item)
				if len(parts) == 0 {
					continue
				}
				name := parts[0]
				checked++
				if !have[name] {
					t.Errorf("%s imports %s from %s, which does not export it — esbuild builds silently, the live screen falls over",
						path, name, target)
				}
			}
		}
	}
	if checked < 300 {
		t.Fatalf("%d names checked — the import regexp looks past the code", checked)
	}
}

func TestAPIBaseInterceptLivesInOneModule(t *testing.T) {
	files := srcFiles(t)
	api := withoutComments(files["src/api.js"])
	if api == "" {
		t.Fatal("src/api.js not found")
	}
	for name, body := range files {
		code := withoutComments(body)
		if name != "src/api.js" && (strings.Contains(code, "window.fetch =") || strings.Contains(code, "window.EventSource =")) {
			t.Errorf("%s: a second fetch/EventSource intercept — the route of requests is described by one module", name)
		}
		if strings.Contains(code, "location.replace(") {
			t.Errorf("%s: a location.replace jump — the page does not move", name)
		}
	}
	for _, want := range []string{`startsWith("/api/")`, `credentials: "omit"`, `"Bearer " + token`, `"token=" + encodeURIComponent(token)`, "window.fetch =", "window.EventSource ="} {
		if !strings.Contains(api, want) {
			t.Errorf("src/api.js: no %s", want)
		}
	}
	if n := strings.Count(api, `startsWith("`); n != 1 {
		t.Errorf("src/api.js: %d address rewrite rules, expected one — /api/", n)
	}
	for _, foreign := range []string{`"/auth/`, `"/login`, `"/logout`, `"/static/`, `"/dist/`} {
		if strings.Contains(api, foreign) {
			t.Errorf("src/api.js: mentions %s — those addresses are never rewritten", foreign)
		}
	}
	main := withoutComments(files["src/main.js"])
	if at, render := strings.Index(main, "api.install()"), strings.Index(main, "render(html"); at < 0 || render < 0 || at > render {
		t.Error("src/main.js: the intercept is installed after the first render — the first request of the tree goes past it")
	}
	app := withoutComments(files["src/app.js"])
	if strings.Count(app, "pick({") != 1 || !strings.Contains(app, "api.use({ base: r.base") {
		t.Error("src/app.js: the base is not chosen by a single repick — a second choice elsewhere would be a second opinion on where to go")
	}
	if !strings.Contains(app, "api.watchFailures(") || !strings.Contains(app, "repick(dead)") {
		t.Error("src/app.js: a failed request through the base does not lead to picking the next one on the map")
	}
	if !strings.Contains(app, "failed(dead") {
		t.Error("src/app.js: an address is crossed out on a failed request without being asked by a probe")
	}
	if !strings.Contains(app, "tellEndpoints(r.endpoints") {
		t.Error("src/app.js: the service worker never learns the panel addresses — answers through the base never reach the data cache")
	}
	if !strings.Contains(app, "}, PICK_MS);") || !strings.Contains(app, "setInterval(() =>") {
		t.Error("src/app.js: the base is not re-picked on a timer — a nearer address that appears after sign-in stays unnoticed until a reload")
	}
	if !strings.Contains(app, `if (document.visibilityState === "visible" && routed.current) repick();`) {
		t.Error("src/app.js: the timed re-measurement ignores tab visibility — a background tab pings every address on the map for nothing")
	}
	router := withoutComments(files["src/router.js"])
	for _, want := range []string{`mode: "cors"`, `credentials: "omit"`, "r.panel !== map.panel", "usable(e, protocol)"} {
		if !strings.Contains(router, want) {
			t.Errorf("src/router.js: no %s", want)
		}
	}
	for _, gone := range []string{"handoff", "stay", "travel", "ROUTE_KEY"} {
		if strings.Contains(router, gone) {
			t.Errorf("src/router.js: %q is left over — the page-relocation machinery is gone entirely", gone)
		}
	}
	sw := withoutComments(files["src/sw.js"])
	if strings.Count(sw, "await fetchWithin(request)") < 3 || strings.Contains(sw, "await fetch(request)") {
		t.Error("src/sw.js: navigation, data and code have to reach the network with a ceiling (fetchWithin), otherwise a dead address keeps the PWA on the logo")
	}
	if !strings.Contains(sw, `type === "ENDPOINTS"`) || !strings.Contains(sw, "ENDPOINTS.includes(url.origin)") || !strings.Contains(sw, "dataKey(request)") {
		t.Error("src/sw.js: API answers from addresses on the map are not cached by path — the offline snapshot lives only on the home origin")
	}
	for _, path := range []string{"src/ui/header.js", "src/desktop/shell.js"} {
		if !strings.Contains(withoutComments(files[path]), "route.onOpen") {
			t.Errorf("%s: the address chip in the header does not open the sheet — the address table cannot be reached at all", path)
		}
	}
	if !strings.Contains(withoutComments(files["src/desktop/shell.js"]), "route.here && html") {
		t.Error("src/desktop/shell.js: the addresses button is conditional — normally the table is out of reach")
	}
	// On the phone the chip is the connection itself: it is drawn in every
	// state, with a map of addresses or without, and always opens the sheet.
	if head := withoutComments(files["src/ui/header.js"]); strings.Contains(head, "route.here && html") ||
		!strings.Contains(head, "onClick=${route && route.onOpen}") {
		t.Error("src/ui/header.js: the connection chip is drawn only with a map, or does not open the sheet — " +
			"with no map the connection has no door at all")
	}
	for _, path := range []string{"src/screens/machine.js", "src/desktop/machine.js"} {
		if strings.Contains(withoutComments(files[path]), "RouteTable") {
			t.Errorf("%s: the address table is back in the hardware screen — it has one door, the button in the header", path)
		}
	}
	if route := withoutComments(files["src/ui/route.js"]); !strings.Contains(route, "export function RouteSheet(") ||
		!strings.Contains(route, "<${Sheet}") {
		t.Error("src/ui/route.js: the address table does not live in the shared sheet — the back gesture closes the whole app")
	}
}

func TestHookCallersImportThem(t *testing.T) {
	hook := regexp.MustCompile(`\buse(State|Effect|Ref|Memo|Callback|Context|Reducer|LayoutEffect|ErrorBoundary|Id)\b`)
	for path, body := range srcFiles(t) {
		used := map[string]bool{}
		for _, m := range hook.FindAllString(body, -1) {
			used[m] = true
		}
		if len(used) == 0 {
			continue
		}
		for name := range used {
			declared := regexp.MustCompile(`(?m)^(export\s+)?function\s+` + name + `\b`).MatchString(body)
			imported := regexp.MustCompile(`(?ms)^import\s*\{[^}]*\b` + name + `\b[^}]*\}\s*from`).MatchString(body)
			if !declared && !imported {
				t.Errorf("%s: %s is called but neither imported nor declared — the screen falls over in the browser while the build stays silent", path, name)
			}
		}
	}
}
