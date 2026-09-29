package install

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
)

// fakeGo is the go of the test: it notes how it was called and builds, in
// place of the installer, a script that says what it was given.
const fakeGo = `#!/bin/sh
echo "GOTOOLCHAIN=$GOTOOLCHAIN CGO_ENABLED=$CGO_ENABLED GOCACHE=$GOCACHE GOMODCACHE=$GOMODCACHE XDG_CONFIG_HOME=$XDG_CONFIG_HOME $*" >>"$FAKE_GO_LOG"
out=
while [ $# -gt 0 ]; do
	if [ "$1" = -o ]; then out=$2; fi
	shift
done
printf '#!/bin/sh\necho "installer: $*"\necho "clone: $AACP_INSTALL_CLONE"\n' >"$out"
chmod +x "$out"
`

// goDev is go.dev/dl for the test: the listing with its checksums and the
// archives, each download counted.
type goDev struct {
	srv      *httptest.Server
	mu       sync.Mutex
	hits     map[string]int
	archives map[string][]byte // by file name
	wrongSum bool
}

func archive(t *testing.T, version string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := []struct {
		name, body string
		mode       int64
	}{
		{"go/bin/go", fakeGo, 0o755},
		{"go/VERSION", "go" + version + "\n", 0o644},
	}
	for _, f := range files {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(f.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(f.body))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func newGoDev(t *testing.T, versions ...string) *goDev {
	g := &goDev{hits: map[string]int{}, archives: map[string][]byte{}}
	for _, v := range versions {
		g.archives["go"+v+".linux-"+runtime.GOARCH+".tar.gz"] = archive(t, v)
	}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		defer g.mu.Unlock()
		if r.URL.Path == "/" && r.URL.Query().Get("mode") == "json" {
			g.hits["list"]++
			w.Write(g.listing(versions))
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		body, ok := g.archives[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		g.hits[name]++
		w.Write(body)
	}))
	t.Cleanup(g.srv.Close)
	return g
}

// listing is shaped as go.dev gives it: indented, a release an object with
// its files, the source archive first.
func (g *goDev) listing(versions []string) []byte {
	type file struct {
		Filename string `json:"filename"`
		OS       string `json:"os"`
		Arch     string `json:"arch"`
		Version  string `json:"version"`
		Sha256   string `json:"sha256"`
		Size     int    `json:"size"`
		Kind     string `json:"kind"`
	}
	type release struct {
		Version string `json:"version"`
		Stable  bool   `json:"stable"`
		Files   []file `json:"files"`
	}
	var list []release
	for _, v := range versions {
		name := "go" + v + ".linux-" + runtime.GOARCH + ".tar.gz"
		sum := sha256.Sum256(g.archives[name])
		hexSum := hex.EncodeToString(sum[:])
		if g.wrongSum {
			hexSum = strings.Repeat("0", 64)
		}
		list = append(list, release{Version: "go" + v, Stable: true, Files: []file{
			{Filename: "go" + v + ".src.tar.gz", Version: "go" + v, Sha256: strings.Repeat("a", 64), Kind: "source"},
			{Filename: "go" + v + ".linux-386.tar.gz", OS: "linux", Arch: "386", Version: "go" + v, Sha256: strings.Repeat("b", 64), Kind: "archive"},
			{Filename: name, OS: "linux", Arch: runtime.GOARCH, Version: "go" + v, Sha256: hexSum, Size: len(g.archives[name]), Kind: "archive"},
		}})
	}
	out, _ := json.MarshalIndent(list, "", " ")
	return out
}

func (g *goDev) count(name string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.hits[name]
}

// clonedWith is a clone of the panel with the real install.sh and a go.mod
// naming the given Go.
func clonedWith(t *testing.T, gomod string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "aacpanel")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile(filepath.Join("..", "..", "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "install.sh"), script, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

type scriptRun struct {
	status         int
	stdout, stderr string
}

// runScript runs install.sh of the clone the way a person would, but in a
// session of its own: there is no terminal to reach through /dev/tty.
func runScript(t *testing.T, repo, cache, downloads string, path string, args ...string) scriptRun {
	t.Helper()
	cmd := exec.Command("bash", append([]string{filepath.Join(repo, "install.sh")}, args...)...)
	cmd.Env = []string{
		"PATH=" + path,
		"HOME=" + filepath.Dir(cache),
		"XDG_CACHE_HOME=" + cache,
		"AACP_GO_DOWNLOADS=" + downloads,
		"FAKE_GO_LOG=" + filepath.Join(filepath.Dir(cache), "go.log"),
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	status := 0
	if exit, ok := err.(*exec.ExitError); ok {
		status = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return scriptRun{status, stdout.String(), stderr.String()}
}

func goArchive(v string) string { return "go" + v + ".linux-" + runtime.GOARCH + ".tar.gz" }

func needArch(t *testing.T) {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skipf("install.sh knows x86_64 and aarch64, and this is %s", runtime.GOARCH)
	}
}

func TestInstallScriptGetsItsGoOnceAndChecksIt(t *testing.T) {
	needArch(t)
	dev := newGoDev(t, "1.26.0", "1.26.1")
	repo := clonedWith(t, "module aacpanel\n\ngo 1.26.0\n")
	cache := filepath.Join(t.TempDir(), "cache")
	path := os.Getenv("PATH")
	root := filepath.Join(cache, "aacpanel-install")

	first := runScript(t, repo, cache, dev.srv.URL, path, "plan", "--plain")
	if first.status != 0 {
		t.Fatalf("the first run ended with %d:\n%s%s", first.status, first.stdout, first.stderr)
	}
	if want := "installer: plan --plain\nclone: " + repo + "\n"; first.stdout != want {
		t.Errorf("the installer said:\n%s\nwant:\n%s", first.stdout, want)
	}
	if dev.count("list") != 1 || dev.count(goArchive("1.26.0")) != 1 {
		t.Errorf("the first run fetched the listing %d times and the archive %d", dev.count("list"), dev.count(goArchive("1.26.0")))
	}
	if _, err := os.Stat(filepath.Join(root, "go1.26.0", "bin", "go")); err != nil {
		t.Errorf("Go is not in the cache: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "download")); !os.IsNotExist(err) {
		t.Errorf("the download was left behind: %v", err)
	}
	log, _ := os.ReadFile(filepath.Join(filepath.Dir(cache), "go.log"))
	want := "GOTOOLCHAIN=local CGO_ENABLED=0 GOCACHE=" + root + "/gocache GOMODCACHE=" + root + "/gomod XDG_CONFIG_HOME=" + root + "/config " +
		"build -trimpath -buildvcs=false -o " + root + "/bin/aacpanel-install.new ./cmd/aacpanel-install\n"
	if string(log) != want {
		t.Errorf("go was called as:\n%s\nwant:\n%s", log, want)
	}

	again := runScript(t, repo, cache, dev.srv.URL, path, "plan")
	if again.status != 0 || dev.count("list") != 1 || dev.count(goArchive("1.26.0")) != 1 {
		t.Errorf("a second run went to go.dev: listing %d, archive %d, status %d\n%s",
			dev.count("list"), dev.count(goArchive("1.26.0")), again.status, again.stderr)
	}

	// A new version in go.mod downloads the new Go and removes the old.
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module aacpanel\n\ngo 1.26.0\n\ntoolchain go1.26.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	moved := runScript(t, repo, cache, dev.srv.URL, path, "plan")
	if moved.status != 0 || dev.count(goArchive("1.26.1")) != 1 {
		t.Fatalf("the toolchain line was not followed: status %d, archive %d\n%s", moved.status, dev.count(goArchive("1.26.1")), moved.stderr)
	}
	entries, _ := os.ReadDir(root)
	var gos []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "go1") {
			gos = append(gos, e.Name())
		}
	}
	if strings.Join(gos, " ") != "go1.26.1" {
		t.Errorf("the cache holds %q after the move to 1.26.1", gos)
	}
}

func TestInstallScriptRefusesAnArchiveThatDoesNotMatch(t *testing.T) {
	needArch(t)
	dev := newGoDev(t, "1.26.0")
	dev.wrongSum = true
	repo := clonedWith(t, "module aacpanel\n\ngo 1.26.0\n")
	cache := filepath.Join(t.TempDir(), "cache")
	r := runScript(t, repo, cache, dev.srv.URL, os.Getenv("PATH"), "plan")
	want := "stop: go1.26.0 does not match the checksum go.dev publishes: the download is damaged or replaced. Run again; if it repeats, report it.\n"
	if r.status != 1 || !strings.HasSuffix(r.stderr, want) || r.stdout != "" {
		t.Errorf("status %d, stdout %q, stderr:\n%s", r.status, r.stdout, r.stderr)
	}
	root := filepath.Join(cache, "aacpanel-install")
	for _, left := range []string{"go1.26.0", "go1.26.0.part", "download"} {
		if _, err := os.Stat(filepath.Join(root, left)); !os.IsNotExist(err) {
			t.Errorf("%s is left after the refusal: %v", left, err)
		}
	}
}

func TestInstallScriptStopsWithoutGoDev(t *testing.T) {
	needArch(t)
	dev := newGoDev(t, "1.26.0")
	dev.srv.Close()
	repo := clonedWith(t, "module aacpanel\n\ngo 1.26.0\n")
	r := runScript(t, repo, filepath.Join(t.TempDir(), "cache"), dev.srv.URL, os.Getenv("PATH"), "plan")
	want := "stop: go.dev does not answer: the installer builds itself with Go it downloads from there. Check the network or a proxy (HTTPS_PROXY) and run again.\n"
	if r.status != 1 || !strings.HasSuffix(r.stderr, want) {
		t.Errorf("status %d, stderr:\n%s", r.status, r.stderr)
	}
}

func TestInstallScriptRefusesRoot(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "id"), []byte("#!/bin/sh\necho 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	repo := clonedWith(t, "module aacpanel\n\ngo 1.26.0\n")
	r := runScript(t, repo, filepath.Join(t.TempDir(), "cache"), "http://127.0.0.1:1", bin+":"+os.Getenv("PATH"), "plan")
	want := "stop: run the installer as the user whose claude sessions the panel will manage, not as root: the executor refuses to run as root. As that user: ./install.sh\n"
	if r.status != 1 || r.stderr != want {
		t.Errorf("status %d, stderr:\n%s", r.status, r.stderr)
	}
}
