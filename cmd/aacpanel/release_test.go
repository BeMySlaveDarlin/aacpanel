package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"aacpanel/internal/chat"
	"aacpanel/internal/docker"
	"aacpanel/internal/release"
)

// answered is a check of a service built from build that GitHub answered
// with latest.
func answered(t *testing.T, build, latest string) *release.Check {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name": "` + latest + `"}`))
	}))
	t.Cleanup(srv.Close)
	check := release.NewCheck(build, false)
	check.URL = srv.URL
	if err := check.Once(t.Context()); err != nil {
		t.Fatal(err)
	}
	return check
}

// The screens read the release of the panel off the host snapshot: the
// release it was built from, whether it asks GitHub, and a newer release
// with its page and the command that takes the install there — only when
// one is out.
func TestTheHostSnapshotSaysTheReleaseOfThePanel(t *testing.T) {
	off := release.NewCheck("v1.2.0", true)
	for _, c := range []struct {
		name  string
		check *release.Check
		want  string
	}{
		{"a newer release out", answered(t, "v1.2.0", "v1.3.0"),
			`{"version":"v1.2.0","check":true,"newer":{"tag":"v1.3.0","page":"https://github.com/BeMySlaveDarlin/aacpanel/releases/tag/v1.3.0","update":"./install.sh update"}}`},
		{"a release of a later paradigm", answered(t, "v1.2.0", "v2.0.0"),
			`{"version":"v1.2.0","check":true,"newer":{"tag":"v2.0.0","page":"https://github.com/BeMySlaveDarlin/aacpanel/releases/tag/v2.0.0","update":"./install.sh update --to 2"}}`},
		{"on the latest release", answered(t, "v1.2.0", "v1.2.0"), `{"version":"v1.2.0","check":true}`},
		{"ahead of the latest release", answered(t, "v1.3.0", "v1.2.0"), `{"version":"v1.3.0","check":true}`},
		{"a build of no release", answered(t, "", "v1.3.0"), `{"version":"","check":false}`},
		{"the check switched off", off, `{"version":"v1.2.0","check":false}`},
		{"no check at all", nil, `{"version":"","check":false}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := &Server{host: hostWith(t, `{"at": 1}`), releases: c.check}
			rec := httptest.NewRecorder()
			srv.apiHost(rec, httptest.NewRequest(http.MethodGet, "/api/host", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("the snapshot: %d %s", rec.Code, rec.Body.String())
			}
			var got struct {
				Panel json.RawMessage `json:"panel"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if string(got.Panel) != c.want {
				t.Errorf("the panel reads\n  %s\nwant\n  %s", got.Panel, c.want)
			}
		})
	}
}

// AACP_UPDATE_CHECK=0 in the panel's env turns the check off, so that no
// request goes out; anything else, or nothing, leaves it on.
func TestTheSettingTurnsTheCheckOff(t *testing.T) {
	for value, asks := range map[string]bool{"0": false, "": true, "1": true} {
		t.Setenv("AACP_UPDATE_CHECK", value)
		if got := releaseCheck("v1.2.0").Asks(); got != asks {
			t.Errorf("AACP_UPDATE_CHECK=%q: the check asks %v, want %v", value, got, asks)
		}
	}
}

// The look of the host carries the newer release once GitHub answered, and
// says it answered: before that the push of a release is neither raised nor
// let go.
func TestWatcherReadsTheNewerRelease(t *testing.T) {
	look := func(check *release.Check) (seen bool, latest string) {
		srv := &Server{host: hostWith(t, `{"at": 1}`), docker: docker.New("http://127.0.0.1:1"), chat: chat.New(""), releases: check}
		cur := newWatcher(srv, nil).look(t.Context())
		if cur.Release != nil {
			latest = cur.Release.Latest + " " + cur.Release.Running + " " + cur.Release.Update
		}
		return cur.ReleaseSeen, latest
	}
	if seen, latest := look(release.NewCheck("v1.2.0", false)); seen || latest != "" {
		t.Errorf("a check GitHub has not answered was seen %v with %q", seen, latest)
	}
	if seen, latest := look(answered(t, "v1.2.0", "v1.3.0")); !seen || latest != "v1.3.0 v1.2.0 ./install.sh update" {
		t.Errorf("a newer release reads as seen %v with %q", seen, latest)
	}
	if seen, latest := look(answered(t, "v1.2.0", "v1.2.0")); !seen || latest != "" {
		t.Errorf("the latest release the panel is on reads as seen %v with %q", seen, latest)
	}
}

// The image takes its release from the RELEASE= line of install.sh: the
// build context has no .git. A -X naming a variable that is not there is
// dropped by the linker without a word, so the name is held here against
// the variable it writes.
func TestTheImageWritesItsReleaseIntoTheService(t *testing.T) {
	_ = version // the variable -X main.version writes; renamed, this stops compiling
	read := func(name string) string {
		raw, err := os.ReadFile(filepath.Join("..", "..", name))
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	dockerfile := read("Dockerfile")
	if !regexp.MustCompile(`(?m)^COPY install\.sh \./$`).MatchString(dockerfile) {
		t.Error("the Dockerfile does not copy install.sh into the build: the release is read off it")
	}
	if !strings.Contains(dockerfile, `-X main.version=$(sed -n 's/^RELEASE=//p' install.sh)`) {
		t.Error("the build of the service does not write the RELEASE= line of install.sh into main.version")
	}
	line := regexp.MustCompile(`(?m)^RELEASE=(.*)$`).FindStringSubmatch(read("install.sh"))
	if line == nil {
		t.Fatal("install.sh has no RELEASE= line")
	}
	if _, ok := release.Parse(line[1]); !ok {
		t.Errorf("install.sh names the release %q, which a service built from it would not take for one", line[1])
	}
}
