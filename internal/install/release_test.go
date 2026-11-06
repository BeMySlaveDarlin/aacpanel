package install

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A release is what the installer arrives by: make release tags it, and
// install.sh taken from the tag with curl clones it. The tests below make
// releases in a repository of their own with the real deploy/release.sh, the
// real release rule of the Makefile and a check that only notes it ran, and
// download them from a bare repository standing for GitHub.

// isolated is an environment with nothing of the machine's git or make in
// it: no configuration of the user, and none of the jobserver of the make
// that may be running the tests.
func isolated(home string, extra ...string) []string {
	return append([]string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_AUTHOR_NAME=Release Test", "GIT_AUTHOR_EMAIL=release@example.com",
		"GIT_COMMITTER_NAME=Release Test", "GIT_COMMITTER_EMAIL=release@example.com",
	}, extra...)
}

// run runs a command with no terminal to reach, stdin given. A script that
// hands over to itself for good is stopped in a minute, with everything it
// started.
func run(t *testing.T, dir string, env []string, stdin string, name string, args ...string) scriptRun {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Env = dir, env
	cmd.Stdin = strings.NewReader(stdin)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("%s %s did not end in a minute:\n%s%s", name, strings.Join(args, " "), stdout.String(), stderr.String())
	}
	status := 0
	if exit, ok := err.(*exec.ExitError); ok {
		status = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return scriptRun{status, stdout.String(), stderr.String()}
}

// releaseRepo is where releases are made: a clone of the panel in miniature
// with an origin of its own.
type releaseRepo struct {
	work, origin, home, checkLog string
	// bin holds the fake gh; ghLog and ghNotes are what it noted.
	bin, ghLog, ghNotes string
}

func (r *releaseRepo) git(t *testing.T, args ...string) string {
	t.Helper()
	out := run(t, r.work, isolated(r.home), "", "git", args...)
	if out.status != 0 {
		t.Fatalf("git %s: %s", strings.Join(args, " "), out.stderr)
	}
	return strings.TrimSpace(out.stdout)
}

// originGit asks the bare origin, the stand-in for GitHub.
func (r *releaseRepo) originGit(t *testing.T, args ...string) string {
	t.Helper()
	out := run(t, r.origin, isolated(r.home), "", "git", args...)
	if out.status != 0 {
		t.Fatalf("git %s in the origin: %s", strings.Join(args, " "), out.stderr)
	}
	return strings.TrimSpace(out.stdout)
}

// makeRule is a rule of the Makefile, as it is: release makes a release,
// release-notes prints the notes of one.
func makeRule(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	var rule []string
	for _, line := range strings.Split(string(body), "\n") {
		switch {
		case line == name+":":
			rule = append(rule, line)
		case len(rule) > 0 && strings.HasPrefix(line, "\t"):
			rule = append(rule, line)
		case len(rule) > 0:
			return strings.Join(rule, "\n") + "\n"
		}
	}
	if len(rule) == 0 {
		t.Fatalf("the Makefile has no %s rule", name)
	}
	return strings.Join(rule, "\n") + "\n"
}

// fakeGH stands for gh: it notes every call, and the notes a release is
// created with, and whether its tag was in the origin by then. GH_SIGNED_OUT
// fails its auth status, GH_RED the creation of a release.
const fakeGH = `#!/bin/sh
printf '%s\n' "$*" >>"$GH_LOG"
case "$1" in
auth) test -z "$GH_SIGNED_OUT" ;;
release)
	cat >"$GH_NOTES"
	if git -C "$GH_ORIGIN" rev-parse -q --verify "refs/tags/$3" >/dev/null; then
		echo "the tag is in the origin" >>"$GH_LOG"
	fi
	test -z "$GH_RED" ;;
*) exit 2 ;;
esac
`

// stubCheck stands for make check: it notes each run, fails with CHECK_RED
// and leaves a file behind with CHECK_WRITES.
const stubCheck = "check:\n" +
	"\t@echo check >>\"$$CHECK_LOG\"\n" +
	"\t@test -z \"$$CHECK_RED\"\n" +
	"\t@if [ -n \"$$CHECK_WRITES\" ]; then touch made-by-check; fi\n\n"

func newReleaseRepo(t *testing.T) *releaseRepo {
	t.Helper()
	base := t.TempDir()
	r := &releaseRepo{work: filepath.Join(base, "work"), origin: filepath.Join(base, "origin.git"),
		home: filepath.Join(base, "home"), checkLog: filepath.Join(base, "check.log"),
		bin: filepath.Join(base, "bin"), ghLog: filepath.Join(base, "gh.log"), ghNotes: filepath.Join(base, "gh.notes")}
	for _, d := range []string{r.work, r.origin, r.home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	real := func(name string) string {
		body, err := os.ReadFile(filepath.Join("..", "..", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	// The tree is one before its first release, whatever release the real
	// install.sh names.
	write(t, filepath.Join(r.work, "install.sh"), withRelease(t, real("install.sh"), ""), 0o755)
	write(t, filepath.Join(r.work, "deploy", "release.sh"), real(filepath.Join("deploy", "release.sh")), 0o755)
	write(t, filepath.Join(r.work, "deploy", "release-notes.sh"), real(filepath.Join("deploy", "release-notes.sh")), 0o755)
	write(t, filepath.Join(r.bin, "gh"), fakeGH, 0o755)
	write(t, filepath.Join(r.work, "deploy", "install", "root.sh"), "", 0o644)
	write(t, filepath.Join(r.work, "go.mod"), "module aacpanel\n\ngo 1.26.0\n", 0o644)
	write(t, filepath.Join(r.work, "Makefile"), stubCheck+makeRule(t, "release")+"\n"+makeRule(t, "release-notes"), 0o644)
	write(t, filepath.Join(r.work, "README.md"), "aacpanel\n", 0o644)

	r.originGit(t, "init", "--quiet", "--bare")
	r.git(t, "init", "--quiet", "--initial-branch=main")
	r.git(t, "add", "-A")
	r.git(t, "commit", "--quiet", "-m", "start")
	r.git(t, "remote", "add", "origin", r.origin)
	return r
}

func write(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

// release runs make release with the given variables; the database the
// check needs is named unless the test takes it away, and gh is the fake one.
func (r *releaseRepo) release(t *testing.T, env []string, vars ...string) scriptRun {
	t.Helper()
	return run(t, r.work, r.env(env...), "", "make", append([]string{"--no-print-directory", "release"}, vars...)...)
}

// env is the environment a release runs in: GH names the fake gh, so no
// test reaches GitHub, and the fake is the gh of the PATH too.
func (r *releaseRepo) env(extra ...string) []string {
	env := isolated(r.home, "CHECK_LOG="+r.checkLog, "AACP_TEST_DSN=postgres://test",
		"GH="+filepath.Join(r.bin, "gh"), "GH_LOG="+r.ghLog, "GH_NOTES="+r.ghNotes, "GH_ORIGIN="+r.origin)
	env[0] = "PATH=" + r.bin + string(os.PathListSeparator) + os.Getenv("PATH")
	return append(env, extra...)
}

// gh is what the fake gh was asked, a call a line.
func (r *releaseRepo) gh(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(r.ghLog)
	if os.IsNotExist(err) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// notes runs the notes script of the repository with args.
func (r *releaseRepo) notes(t *testing.T, args ...string) scriptRun {
	t.Helper()
	return run(t, r.work, r.env(), "", filepath.Join(r.work, "deploy", "release-notes.sh"), args...)
}

// commit makes a commit of the subject, with a change of its own.
func (r *releaseRepo) commit(t *testing.T, subject string) {
	t.Helper()
	write(t, filepath.Join(r.work, "README.md"), subject+"\n", 0o644)
	r.git(t, "commit", "--quiet", "-am", subject)
}

func (r *releaseRepo) checks(t *testing.T) int {
	body, err := os.ReadFile(r.checkLog)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(body), "check\n")
}

// releaseLine is the line of install.sh a release is written into. Between
// releases it holds the last one, so nothing here counts on it being empty.
var releaseLine = regexp.MustCompile(`(?m)^RELEASE=.*$`)

// withRelease is the script with tag written in; "" makes a script of no
// release.
func withRelease(t *testing.T, script, tag string) string {
	t.Helper()
	if n := len(releaseLine.FindAllStringIndex(script, -1)); n != 1 {
		t.Fatalf("install.sh has %d RELEASE= lines, want one", n)
	}
	return releaseLine.ReplaceAllLiteralString(script, "RELEASE="+tag)
}

func TestReleaseTagsACleanTreeThatPassedCheck(t *testing.T) {
	r := newReleaseRepo(t)
	start := r.git(t, "rev-parse", "HEAD")
	before, _ := os.ReadFile(filepath.Join(r.work, "install.sh"))

	out := r.release(t, nil, "VERSION=1.2.3")
	if out.status != 0 {
		t.Fatalf("make release ended with %d:\n%s%s", out.status, out.stdout, out.stderr)
	}
	if want := "v1.2.3 is tagged here and not pushed. Publish it with: git push --atomic origin HEAD refs/tags/v1.2.3 && " +
		"deploy/release-notes.sh v1.2.3 | gh release create v1.2.3 --title 'aacpanel v1.2.3' --latest --verify-tag --notes-file -\n"; out.stdout != want {
		t.Errorf("make release said %q, want %q", out.stdout, want)
	}
	if asked := r.gh(t); asked != "" {
		t.Errorf("a release without PUSH=1 asked gh:\n%s", asked)
	}
	if n := r.checks(t); n != 1 {
		t.Errorf("make check ran %d times, want once", n)
	}
	if got := r.git(t, "cat-file", "-t", "v1.2.3"); got != "tag" {
		t.Errorf("v1.2.3 is a %s, not an annotated tag", got)
	}
	if tagged, head := r.git(t, "rev-parse", "v1.2.3^{commit}"), r.git(t, "rev-parse", "HEAD"); tagged != head {
		t.Errorf("the tag is on %s, the branch on %s", tagged, head)
	}
	if parent := r.git(t, "rev-parse", "HEAD~1"); parent != start {
		t.Errorf("the release commit stands on %s, not on the commit that was checked (%s)", parent, start)
	}
	if files := r.git(t, "diff", "--name-only", "HEAD~1", "HEAD"); files != "install.sh" {
		t.Errorf("the release commit changes %q, want install.sh alone", files)
	}
	want := withRelease(t, string(before), "v1.2.3")
	if got := r.git(t, "show", "v1.2.3:install.sh"); got+"\n" != want {
		t.Errorf("install.sh of the tag is not the script with the release written in:\n%s", got)
	}
	if st := r.git(t, "status", "--porcelain"); st != "" {
		t.Errorf("the tree is left with changes:\n%s", st)
	}
	if refs := r.originGit(t, "for-each-ref"); refs != "" {
		t.Errorf("the release reached the origin without PUSH=1:\n%s", refs)
	}
}

func TestReleaseMadeAgainAfterItsTagWasDroppedTagsTheSameCommit(t *testing.T) {
	r := newReleaseRepo(t)
	if out := r.release(t, nil, "VERSION=1.2.3"); out.status != 0 {
		t.Fatalf("the first make release: %s%s", out.stdout, out.stderr)
	}
	head := r.git(t, "rev-parse", "HEAD")
	r.git(t, "tag", "-d", "v1.2.3")
	if out := r.release(t, nil, "VERSION=1.2.3"); out.status != 0 {
		t.Fatalf("make release after the tag was dropped ended with %d:\n%s%s", out.status, out.stdout, out.stderr)
	}
	if got := r.git(t, "rev-parse", "HEAD", "v1.2.3^{commit}"); got != head+"\n"+head {
		t.Errorf("the branch and the tag are at\n%s\nwant both on the release commit %s", got, head)
	}
}

func TestReleaseWithPushPublishesTheTagAndTheBranch(t *testing.T) {
	r := newReleaseRepo(t)
	out := r.release(t, nil, "VERSION=0.1.0", "PUSH=1")
	if out.status != 0 || out.stdout != "v0.1.0 is released, pushed and published on GitHub.\n" {
		t.Fatalf("make release ended with %d:\n%s%s", out.status, out.stdout, out.stderr)
	}
	// gh is asked whether it is signed in before anything, and the release
	// is created once the tag is out, marked the latest, with the notes.
	want := "auth status\n" +
		"release create v0.1.0 --title aacpanel v0.1.0 --latest --verify-tag --notes-file -\n" +
		"the tag is in the origin\n"
	if asked := r.gh(t); asked != want {
		t.Errorf("gh was asked:\n%s\nwant:\n%s", asked, want)
	}
	if notes, err := os.ReadFile(r.ghNotes); err != nil || string(notes) != r.notes(t, "v0.1.0").stdout {
		t.Errorf("the release was created with the notes:\n%s(%v)\nwant those of deploy/release-notes.sh v0.1.0", notes, err)
	}
	head := r.git(t, "rev-parse", "HEAD")
	if got := r.originGit(t, "rev-parse", "refs/heads/main", "v0.1.0^{commit}"); got != head+"\n"+head {
		t.Errorf("the origin has main and the tag at\n%s\nwant both at %s", got, head)
	}
	if got := r.originGit(t, "cat-file", "-t", "v0.1.0"); got != "tag" {
		t.Errorf("the origin has v0.1.0 as a %s", got)
	}
}

// A GitHub release that failed after the push cannot be taken back with the
// tag, which is out for good: the stop names the command that creates it,
// and that command run as it is creates it.
func TestAGitHubReleaseThatFailedIsCreatedByTheCommandTheStopNames(t *testing.T) {
	r := newReleaseRepo(t)
	out := r.release(t, []string{"GH_RED=1"}, "VERSION=0.1.0", "PUSH=1")
	const prefix = "stop: v0.1.0 is pushed, and its GitHub release was not created: the output above says why. Create it with: "
	const again = "deploy/release-notes.sh v0.1.0 | gh release create v0.1.0 --title 'aacpanel v0.1.0' --latest --verify-tag --notes-file -"
	if out.status == 0 || !slices.Contains(strings.Split(out.stderr, "\n"), prefix+again) {
		t.Fatalf("make release ended with %d; stderr:\n%s\nwant the line:\n%s", out.status, out.stderr, prefix+again)
	}
	if got := r.originGit(t, "cat-file", "-t", "v0.1.0"); got != "tag" {
		t.Errorf("the origin has v0.1.0 as a %s", got)
	}

	if err := os.Remove(r.ghLog); err != nil {
		t.Fatal(err)
	}
	if rerun := run(t, r.work, r.env(), "", "bash", "-c", again); rerun.status != 0 {
		t.Fatalf("the command the stop names ended with %d:\n%s%s", rerun.status, rerun.stdout, rerun.stderr)
	}
	if asked, want := r.gh(t), "release create v0.1.0 --title aacpanel v0.1.0 --latest --verify-tag --notes-file -\nthe tag is in the origin\n"; asked != want {
		t.Errorf("the command asked gh:\n%s\nwant:\n%s", asked, want)
	}
	if notes, err := os.ReadFile(r.ghNotes); err != nil || !strings.HasPrefix(string(notes), "The first release of the panel.\n") {
		t.Errorf("the command created the release with the notes:\n%s(%v)", notes, err)
	}
}

func TestReleaseRefusesWhatIsNotReady(t *testing.T) {
	for _, c := range []struct {
		name    string
		prepare func(*testing.T, *releaseRepo)
		env     []string
		version string
		stop    string
		checked int
	}{
		{"no version", nil, nil, "", "stop: VERSION= is not P.M.m: plain numbers, no v, like make release VERSION=1.4.0", 0},
		{"two numbers", nil, nil, "1.2", "stop: VERSION=1.2 is not P.M.m: plain numbers, no v, like make release VERSION=1.4.0", 0},
		{"four numbers", nil, nil, "1.2.3.4", "stop: VERSION=1.2.3.4 is not P.M.m: plain numbers, no v, like make release VERSION=1.4.0", 0},
		{"the tag's own v", nil, nil, "v1.2.3", "stop: VERSION=v1.2.3 is not P.M.m: plain numbers, no v, like make release VERSION=1.4.0", 0},
		{"a leading zero", nil, nil, "1.02.3", "stop: VERSION=1.02.3 is not P.M.m: plain numbers, no v, like make release VERSION=1.4.0", 0},
		{"a suffix", nil, nil, "1.2.3-rc1", "stop: VERSION=1.2.3-rc1 is not P.M.m: plain numbers, no v, like make release VERSION=1.4.0", 0},
		{"a changed file", func(t *testing.T, r *releaseRepo) {
			write(t, filepath.Join(r.work, "README.md"), "changed\n", 0o644)
		}, nil, "1.2.3", "stop: the tree has changes: a release is made of commits only. Commit them or put them aside, then run again.", 0},
		{"a new file", func(t *testing.T, r *releaseRepo) {
			write(t, filepath.Join(r.work, "notes.txt"), "draft\n", 0o644)
		}, nil, "1.2.3", "stop: the tree has changes: a release is made of commits only. Commit them or put them aside, then run again.", 0},
		{"a staged file", func(t *testing.T, r *releaseRepo) {
			write(t, filepath.Join(r.work, "README.md"), "staged\n", 0o644)
			r.git(t, "add", "README.md")
		}, nil, "1.2.3", "stop: the tree has changes: a release is made of commits only. Commit them or put them aside, then run again.", 0},
		{"the tag is there", func(t *testing.T, r *releaseRepo) {
			r.git(t, "tag", "v1.2.3")
		}, nil, "1.2.3", "stop: v1.2.3 is there already: a release is never made twice. Take the next number.", 0},
		{"no test database", nil, []string{"AACP_TEST_DSN="}, "1.2.3",
			"stop: AACP_TEST_DSN is not set, and make check would skip the tests with a database. Point it at the test database, then run again.", 0},
		{"no gh to publish with", nil, []string{"PUSH=1", "GH=gh-nowhere"}, "1.2.3",
			"stop: there is no gh here, and with PUSH=1 the release is published on GitHub with it. Install it (https://cli.github.com), sign in with gh auth login, then run again.", 0},
		{"gh signed out", nil, []string{"PUSH=1", "GH_SIGNED_OUT=1"}, "1.2.3",
			"stop: gh is not signed in to GitHub, and with PUSH=1 the release is published there with it. Sign in with gh auth login, then run again.", 0},
		{"a red check", nil, []string{"CHECK_RED=1"}, "1.2.3", "stop: make check is red: a release is made of a tree that passes it.", 1},
		{"a check that writes", nil, []string{"CHECK_WRITES=1"}, "1.2.3",
			"stop: make check changed the tree: what it made is not in the commits. Commit it, then run again.", 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newReleaseRepo(t)
			if c.prepare != nil {
				c.prepare(t, r)
			}
			head := r.git(t, "rev-parse", "HEAD")
			tags := r.git(t, "tag", "--list")
			out := r.release(t, c.env, "VERSION="+c.version)
			if out.status == 0 || !slices.Contains(strings.Split(out.stderr, "\n"), c.stop) {
				t.Errorf("make release ended with %d; stderr:\n%s\nwant the line:\n%s", out.status, out.stderr, c.stop)
			}
			if n := r.checks(t); n != c.checked {
				t.Errorf("make check ran %d times, want %d", n, c.checked)
			}
			if got := r.git(t, "rev-parse", "HEAD"); got != head {
				t.Errorf("a refused release moved the branch to %s", got)
			}
			if got := r.git(t, "tag", "--list"); got != tags {
				t.Errorf("a refused release left tags %q, there were %q", got, tags)
			}
			if diff := r.git(t, "diff", "HEAD", "--", "install.sh"); diff != "" {
				t.Errorf("a refused release wrote into install.sh:\n%s", diff)
			}
			if refs := r.originGit(t, "for-each-ref"); refs != "" {
				t.Errorf("a refused release reached the origin:\n%s", refs)
			}
		})
	}
}

// published is an origin with releases v1.0.0 and v1.1.0 made by make
// release, and the install.sh of each as curl would get it from the tag.
func published(t *testing.T) (*releaseRepo, map[string]string) {
	t.Helper()
	r := newReleaseRepo(t)
	scripts := map[string]string{}
	for _, v := range []string{"1.0.0", "1.1.0"} {
		if out := r.release(t, nil, "VERSION="+v, "PUSH=1"); out.status != 0 {
			t.Fatalf("make release %s: %s%s", v, out.stdout, out.stderr)
		}
		scripts["v"+v] = r.git(t, "show", "v"+v+":install.sh") + "\n"
		write(t, filepath.Join(r.work, "README.md"), "after "+v+"\n", 0o644)
		r.git(t, "commit", "--quiet", "-am", "after "+v)
	}
	return r, scripts
}

// curlPipe runs a script the way curl … | bash -s -- args does in the home
// directory: read from stdin, with no file of its own.
func curlPipe(t *testing.T, script, home, origin, downloads string, extra []string, args ...string) scriptRun {
	t.Helper()
	return curlPipeIn(t, home, script, home, origin, downloads, extra, args...)
}

func curlPipeIn(t *testing.T, cwd, script, home, origin, downloads string, extra []string, args ...string) scriptRun {
	t.Helper()
	cache := filepath.Join(home, ".cache")
	env := isolated(home, append([]string{
		"XDG_CACHE_HOME=" + cache,
		"AACP_ORIGIN=" + origin,
		"AACP_GO_DOWNLOADS=" + downloads,
		"FAKE_GO_LOG=" + filepath.Join(home, "go.log"),
	}, extra...)...)
	return run(t, cwd, env, script, "bash", append([]string{"-s", "--"}, args...)...)
}

func TestTheDownloadClonesItsReleaseAndRunsTheInstallerFromIt(t *testing.T) {
	needArch(t)
	r, scripts := published(t)
	dev := newGoDev(t, "1.26.0")
	home := t.TempDir()
	clone := filepath.Join(home, "aacpanel")

	first := curlPipe(t, scripts["v1.0.0"], home, r.origin, dev.srv.URL, nil, "plan", "--plain")
	if first.status != 0 {
		t.Fatalf("the download ended with %d:\n%s%s", first.status, first.stdout, first.stderr)
	}
	if want := "installer: plan --plain\nclone: " + clone + "\n"; first.stdout != want {
		t.Errorf("the installer said:\n%s\nwant:\n%s", first.stdout, want)
	}
	if !strings.HasPrefix(first.stderr, "aacpanel v1.0.0: cloning it into "+clone+"\n") {
		t.Errorf("the download said:\n%s", first.stderr)
	}
	cloneGit := func(args ...string) string {
		out := run(t, clone, isolated(home), "", "git", args...)
		return strings.TrimSpace(out.stdout)
	}
	if head, tag := cloneGit("rev-parse", "HEAD"), r.git(t, "rev-parse", "v1.0.0^{commit}"); head != tag {
		t.Errorf("the clone is on %s, v1.0.0 is %s", head, tag)
	}

	// The same line again goes on in the clone it made.
	again := curlPipe(t, scripts["v1.0.0"], home, r.origin, dev.srv.URL, nil, "plan")
	if again.status != 0 || again.stdout != "installer: plan\nclone: "+clone+"\n" || strings.Contains(again.stderr, "cloning") {
		t.Errorf("the second run ended with %d:\n%s%s", again.status, again.stdout, again.stderr)
	}

	// AACP_VERSION picks another release, with or without its v.
	for i, v := range []string{"1.1.0", "v1.1.0"} {
		dir := filepath.Join(home, "other", v)
		out := curlPipe(t, scripts["v1.0.0"], home, r.origin, dev.srv.URL, []string{"AACP_VERSION=" + v, "AACP_DIR=" + dir})
		if out.status != 0 || out.stdout != "installer: \nclone: "+dir+"\n" {
			t.Errorf("AACP_VERSION=%s ended with %d:\n%s%s", v, out.status, out.stdout, out.stderr)
			continue
		}
		head := strings.TrimSpace(run(t, dir, isolated(home), "", "git", "rev-parse", "HEAD").stdout)
		if tag := r.git(t, "rev-parse", "v1.1.0^{commit}"); head != tag {
			t.Errorf("run %d: the clone is on %s, v1.1.0 is %s", i, head, tag)
		}
	}

	// Piped inside another clone, the line still means its own: the clone
	// around where it runs is not the script's.
	inside := curlPipeIn(t, filepath.Join(home, "other", "1.1.0"), scripts["v1.0.0"], home, r.origin, dev.srv.URL, nil, "plan")
	if inside.status != 0 || inside.stdout != "installer: plan\nclone: "+clone+"\n" {
		t.Errorf("the line run inside the clone of v1.1.0 ended with %d:\n%s%s", inside.status, inside.stdout, inside.stderr)
	}
}

func TestTheDownloadRefusesWhatItCannotClone(t *testing.T) {
	r, scripts := published(t)
	script, err := os.ReadFile(filepath.Join("..", "..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	unreleased := withRelease(t, string(script), "")
	home := t.TempDir()
	clone := filepath.Join(home, "aacpanel")
	taken, project := filepath.Join(home, "taken"), filepath.Join(home, "project")
	write(t, filepath.Join(taken, "notes.txt"), "mine\n", 0o644)
	// A project of the person's own that has a v1.0.0 of its own.
	write(t, filepath.Join(project, "main.c"), "int main(void) { return 0; }\n", 0o644)
	for _, args := range [][]string{{"init", "--quiet"}, {"add", "-A"}, {"commit", "--quiet", "-m", "mine"}, {"tag", "v1.0.0"}} {
		if out := run(t, project, isolated(home), "", "git", args...); out.status != 0 {
			t.Fatalf("git %v: %s", args, out.stderr)
		}
	}
	notTheClone := func(dir string) string {
		return "stop: " + dir + " is there already and is not the clone of v1.0.0. Run the installer of the clone that is there (" +
			dir + "/install.sh, or " + dir + "/install.sh update to move it on), or clone elsewhere: AACP_DIR=/another/place"
	}
	for _, c := range []struct {
		name, script string
		env          []string
		stop         string
	}{
		{"a script of no release", unreleased, nil,
			"stop: this install.sh belongs to no release, so it cannot tell which to clone. Take it from a tag (…/aacpanel/<tag>/install.sh), name one with AACP_VERSION=vP.M.m, or clone it yourself: git clone " + r.origin + " ~/aacpanel && ~/aacpanel/install.sh"},
		{"not a release", scripts["v1.0.0"], []string{"AACP_VERSION=1.1"},
			"stop: v1.1 is not a release: a release is vP.M.m, v1.0.0 say."},
		{"a release that is not out", scripts["v1.0.0"], []string{"AACP_VERSION=v9.0.0"},
			"stop: v9.0.0 did not clone from " + r.origin + ": the output above says why. Check the network and the name of the release, then run the same command again."},
		{"a directory of something else", scripts["v1.0.0"], []string{"AACP_DIR=" + taken}, notTheClone(taken)},
		{"a repository with the same tag", scripts["v1.0.0"], []string{"AACP_DIR=" + project}, notTheClone(project)},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := curlPipe(t, c.script, home, r.origin, "http://127.0.0.1:1", c.env)
			if out.status != 1 || !strings.HasSuffix(out.stderr, c.stop+"\n") || out.stdout != "" {
				t.Errorf("status %d, stdout %q, stderr:\n%s\nwant it to end with:\n%s", out.status, out.stdout, out.stderr, c.stop)
			}
			if _, err := os.Stat(clone); !os.IsNotExist(err) {
				t.Errorf("%s is made: %v", clone, err)
			}
		})
	}

	// A clone of another release is not taken over by the line of this one.
	curlPipe(t, scripts["v1.1.0"], home, r.origin, newGoDev(t, "1.26.0").srv.URL, nil)
	if _, err := os.Stat(filepath.Join(clone, "deploy", "install", "root.sh")); err != nil {
		t.Fatalf("the clone of v1.1.0 was not made: %v", err)
	}
	want := "stop: " + clone + " is there already and is not the clone of v1.0.0."
	if out := curlPipe(t, scripts["v1.0.0"], home, r.origin, "http://127.0.0.1:1", nil); out.status != 1 || !strings.Contains(out.stderr, want) {
		t.Errorf("the line of v1.0.0 over a clone of v1.1.0 ended with %d:\n%s", out.status, out.stderr)
	}
}

func TestADownloadCutShortRunsNothing(t *testing.T) {
	r, scripts := published(t)
	script := scripts["v1.0.0"]
	last := strings.LastIndex(strings.TrimSuffix(script, "\n"), "\n") + 1
	var cuts []int
	for i, ch := range script[:last] {
		if ch == '\n' {
			cuts = append(cuts, i+1)
		}
	}
	for i := last + 1; i < len(script)-1; i++ {
		cuts = append(cuts, i) // every byte of the call itself
	}
	home := t.TempDir()
	for _, cut := range cuts {
		out := curlPipe(t, script[:cut], home, r.origin, "http://127.0.0.1:1", nil, "uninstall")
		if out.stdout != "" || strings.Contains(out.stderr, "clon") {
			t.Errorf("the download cut after %q ran:\n%s%s", script[max(0, cut-40):cut], out.stdout, out.stderr)
		}
		for _, made := range []string{"aacpanel", ".cache"} {
			if _, err := os.Stat(filepath.Join(home, made)); !os.IsNotExist(err) {
				t.Fatalf("the download cut after %q made %s", script[max(0, cut-40):cut], made)
			}
		}
	}
	whole := curlPipe(t, script, home, r.origin, newGoDev(t, "1.26.0").srv.URL, nil, "uninstall")
	if !strings.Contains(whole.stderr, "cloning") {
		t.Errorf("the whole script did not run either, so the cuts prove nothing:\n%s", whole.stderr)
	}
}
