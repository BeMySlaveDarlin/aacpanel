package hostcfg

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestFixturesCarryNoHostPaths(t *testing.T) {
	root := filepath.Join("..", "..")
	forbidden := []string{
		"/home/" + defaults().UnixUser,
		"/opt/Projects",
		"-home-" + defaults().UnixUser,
		"-opt-Projects",
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		forbidden = append(forbidden, "wg-"+strings.ToLower(h))
	}
	out, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Skipf("git does not answer, the list of the repository files is out of reach: %v", err)
	}

	var hits []string
	for _, rel := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if rel == "" || rel == "internal/hostcfg/fixtures_test.go" {
			continue
		}
		switch filepath.Ext(rel) {
		case ".woff2", ".png", ".ico", ".svg", ".webmanifest":
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		for n, line := range strings.Split(string(raw), "\n") {
			for _, bad := range forbidden {
				if strings.Contains(line, bad) {
					hits = append(hits, rel+":"+strconv.Itoa(n+1)+": "+bad)
				}
			}
		}
	}
	for _, hit := range hits {
		t.Errorf("%s — a path of this machine in a fixture; the tests are due neutral ones (/srv/proj, /home/u, wg-lab)", hit)
	}
}

func TestHostIdentityLivesInDescriptionOnly(t *testing.T) {
	// The name looked for is the one of the user running the tests, so the check
	// belongs to the machine where the code is written and make check is run
	// before a commit. On a build runner the user is called something like
	// runner, which is an ordinary word in this code — the column probes.runner
	// among others — and every line holding it would be reported as a leak.
	if os.Getenv("CI") != "" {
		t.Skip("the owner's name is checked where the code is written, not on a build runner")
	}

	root := filepath.Join("..", "..")
	forbidden := []string{defaults().UnixUser, "/home/" + defaults().UnixUser}

	var hits []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if name == ".git" || name == "node_modules" || name == "__pycache__" || name == "dist" ||
				name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		isGo := strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go")
		isPy := strings.HasSuffix(name, ".py") && !strings.HasPrefix(name, "test_")
		isJS := strings.HasSuffix(name, ".js")
		if !isGo && !isPy && !isJS {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		body := string(raw)
		if isJS {
			body = withoutMarkupComments(body)
		}
		for n, line := range codeLines(body, isPy) {
			for _, bad := range forbidden {
				if strings.Contains(line, bad) {
					hits = append(hits, rel+":"+strconv.Itoa(n+1)+": "+bad)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range hits {
		t.Errorf("%s — the owner's name in working code; its place is in the description of the machine "+
			"(hostcfg + deploy/host.env.example), otherwise on somebody else's machine the panel lies silently", hit)
	}
}

func withoutMarkupComments(body string) string {
	lines := strings.Split(body, "\n")
	inComment := false
	for i, line := range lines {
		out := line
		if inComment {
			if cut := strings.Index(line, "-->"); cut >= 0 {
				out, inComment = line[cut+3:], false
			} else {
				out = ""
			}
		}
		if !inComment {
			for {
				open := strings.Index(out, "<!--")
				if open < 0 {
					break
				}
				close := strings.Index(out[open:], "-->")
				if close < 0 {
					out, inComment = out[:open], true
					break
				}
				out = out[:open] + out[open+close+3:]
			}
		}
		lines[i] = out
	}
	return strings.Join(lines, "\n")
}

func codeLines(body string, py bool) []string {
	lines := strings.Split(body, "\n")
	out := make([]string, len(lines))
	inDoc := false
	for i, line := range lines {
		s := strings.TrimSpace(line)
		if py {
			if strings.HasPrefix(s, "#") {
				continue
			}
			quotes := strings.Count(s, `"""`)
			if inDoc {
				if quotes%2 == 1 {
					inDoc = false
				}
				continue
			}
			if quotes%2 == 1 {
				inDoc = true
				continue
			}
			if quotes >= 2 && strings.HasPrefix(s, `"""`) {
				continue
			}
			out[i] = line
			continue
		}
		if strings.HasPrefix(s, "//") {
			continue
		}
		code, _, _ := strings.Cut(line, "//")
		out[i] = code
	}
	return out
}

func TestHostEnvExampleIsATemplate(t *testing.T) {
	path := filepath.Join("..", "..", "deploy", "host.env.example")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	values := parseEnv(raw)

	var marks []string
	if u := currentUser(); u != "" {
		marks = append(marks, strings.ToLower(u))
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		marks = append(marks, strings.ToLower(h))
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		marks = append(marks, strings.ToLower(home))
	}
	for key, value := range values {
		low := strings.ToLower(value)
		for _, mark := range marks {
			if mark != "" && strings.Contains(low, mark) {
				t.Errorf("%s=%s — a mark of the machine this was written on; a sample belongs in a comment", key, value)
			}
		}
	}

	for _, key := range []string{HostEnv, RepoEnv, UnixUserEnv, DisplayEnv, HomeSessionEnv, LangEnv, StateDirEnv} {
		if _, ok := values[key]; !ok {
			t.Errorf("%s is not named in the template by a value — a person going down the file will not fill it in", key)
		}
	}

	clearHostEnv(t)
	if got, want := LoadFrom(path), defaults(); got != want {
		t.Errorf("the template with no edits describes a machine that is not the typical one:\n%+v\ndefaults:\n%+v", got, want)
	}
}

// Cyrillic that is not text for a human. A key arrives from data written
// earlier, and translating it would part the code from that data; a trigger
// phrase is how a skill recognises a request said in Russian.
var (
	cyrillicKeys = map[string][]string{
		// The unit of an alert, as it was written into the database before.
		"internal/notify/events.go":         {`"байты"`, `"флаг"`},
		"web/src/screens/alerts.js":         {`"байты"`, `"флаг"`},
		"internal/hostcfg/fixtures_test.go": {`"байты"`, `"флаг"`},

		// The note the sender puts at the head of a reply; transcripts written
		// earlier still carry it, and a translated pattern would stop stripping it.
		"agent/chat/harness.py": {`^\[владелец[^\]\n]*\]\s*`},
		"agent/test_chat.py":    {`[владелец · отправлено с телефона через панель aacpanel]`},

		// The range of letters a check walks over, and a file name as the desktop
		// of a machine in another locale writes it — data the code has to accept.
		"internal/settings/settings_test.go": {`'а' && r <= 'я'`, `'А' && r <= 'Я'`, `'ё'`, `'Ё'`},
		"internal/action/validate_test.go":   {`Снимок_экрана_2026-08-31_в_01.05.png`},
	}

	cyrillicLines = map[string][]string{
		"deploy/claude/skills/restart-session/SKILL.md":       {"description:"},
		"deploy/claude/skills/cross-profile-message/SKILL.md": {"description:"},
		"deploy/claude/skills/notify/SKILL.md":                {"description:"},

		// The rows of the table above: the check names what it allows, so the
		// allowed spelling is written out here in full.
		"internal/hostcfg/fixtures_test.go": {`"agent/`, `"internal/`, `"web/`},
	}
)

func TestRepositorySpeaksOneLanguage(t *testing.T) {
	root := filepath.Join("..", "..")
	out, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Skipf("git does not answer, the list of the repository files is out of reach: %v", err)
	}
	files := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
	if len(files) < 100 {
		t.Fatalf("the repository listed %d files: the walk is checking emptiness", len(files))
	}
	seen := 0
	for _, rel := range files {
		if rel == "" || skipFromLanguageCheck(rel) {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		if !utf8.Valid(raw) {
			continue // a binary file: a font and a png carry no NUL, but they are not text either
		}
		seen++
		keys, lines := cyrillicKeys[rel], cyrillicLines[rel]
	nextLine:
		for i, line := range strings.Split(string(raw), "\n") {
			for _, prefix := range lines {
				if strings.HasPrefix(strings.TrimSpace(line), prefix) {
					continue nextLine
				}
			}
			for _, key := range keys {
				line = strings.ReplaceAll(line, key, "")
			}
			for _, r := range line {
				if unicode.Is(unicode.Cyrillic, r) {
					t.Errorf("%s:%d does not speak English — %s", rel, i+1, short(strings.TrimSpace(line)))
					continue nextLine
				}
			}
		}
	}
	if seen < 100 {
		t.Fatalf("only %d text files were read: the exceptions swallowed the check", seen)
	}
}

// skipFromLanguageCheck names what is allowed to speak otherwise: vendored code
// is somebody else's.
func skipFromLanguageCheck(rel string) bool {
	return strings.HasPrefix(rel, "web/vendor/")
}

func short(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 60 {
		return s[:60] + "…"
	}
	return s
}
