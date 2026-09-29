package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ---- S13: the map, through the panel's local listener ----

// The local listener takes a request of this machine without a sign-in:
// one without an Origin, to a loopback address. The installer talks to the
// map there, as the Profiles screen does, so the map keeps its own checks
// and its journal.

// panelURL is the local listener.
func (in *Install) panelURL() string {
	if in.Local != "" {
		return in.Local
	}
	return fmt.Sprintf("http://127.0.0.1:%d", PortLocal)
}

// apiError is an answer of the panel that is not a success, with what the
// panel said.
type apiError struct {
	Status int
	Said   string
}

func (e *apiError) Error() string {
	if e.Said == "" {
		return fmt.Sprintf("the panel answered %d", e.Status)
	}
	return fmt.Sprintf("the panel answered %d: %s", e.Status, e.Said)
}

// call asks the local listener and reads its answer into out.
func (in *Install) call(r *Run, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(raw)
	}
	url := in.panelURL() + path
	req, err := http.NewRequestWithContext(r.context(), method, url, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		if r.Journal != nil {
			_ = r.Journal.Command([]string{method, url}, "", err)
		}
		return &Failed{Diagnosis: "the panel's local listener does not answer at " + in.panelURL() + ": " + err.Error(),
			Fix: []string{"docker compose logs aacpanel; AACP_LOCAL_ADDR in .env turns the listener off when it is empty."}}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	var failed error
	if resp.StatusCode/100 != 2 {
		failed = &apiError{Status: resp.StatusCode, Said: strings.TrimSpace(string(data))}
	}
	if r.Journal != nil {
		_ = r.Journal.Command([]string{method, url}, "status "+strconv.Itoa(resp.StatusCode), failed)
	}
	if failed != nil {
		return failed
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// The map as the installer reads it.
type (
	mapProject struct {
		ID   int    `json:"id"`
		Path string `json:"path"`
	}
	mapGroup struct {
		ID       int          `json:"id"`
		Name     string       `json:"name"`
		Projects []mapProject `json:"projects"`
	}
	mapContour struct {
		ID        int        `json:"id"`
		Name      string     `json:"name"`
		ConfigDir string     `json:"configDir"`
		Groups    []mapGroup `json:"groups"`
	}
	mapView struct {
		Profiles []mapContour `json:"profiles"`
	}
)

// contourOf is the contour of an account: the one of its directory, or one
// of its name without a directory, which stands for the account of that
// name as the map reads the machine.
func (v mapView) contourOf(dir, name string) *mapContour {
	for i, c := range v.Profiles {
		if c.ConfigDir != "" && filepath.Clean(c.ConfigDir) == filepath.Clean(dir) {
			return &v.Profiles[i]
		}
	}
	for i, c := range v.Profiles {
		if c.ConfigDir == "" && c.Name == name {
			return &v.Profiles[i]
		}
	}
	return nil
}

func (c *mapContour) group(name string) *mapGroup {
	for i, g := range c.Groups {
		if g.Name == name {
			return &c.Groups[i]
		}
	}
	return nil
}

// holds tells whether a project of the path is anywhere on the map: a path
// is one project, and one already there stays where the person put it.
func (v mapView) holds(path string) bool {
	for _, c := range v.Profiles {
		for _, g := range c.Groups {
			for _, p := range g.Projects {
				if filepath.Clean(p.Path) == filepath.Clean(path) {
					return true
				}
			}
		}
	}
	return false
}

// mapAnswered tells whether the answers say anything of the map: a run that
// kept the settings leaves the map as it is.
func (in *Install) mapAnswered() bool {
	for _, dir := range in.accounts() {
		if _, ok := in.S.Value("contour:" + dir); ok {
			return true
		}
	}
	return false
}

func (in *Install) contourName(dir string) string { return in.S.valueOr("contour:"+dir, "") }

// localOff tells whether .env turns the local listener off: an empty value
// does, a missing key keeps the default.
func (in *Install) localOff(r *Run) bool {
	_, env, err := in.readEnv(r)
	if err != nil {
		return false
	}
	v, ok := env["AACP_LOCAL_ADDR"]
	return ok && strings.TrimSpace(v) == ""
}

// mapMissing is what the answers want on the map and it does not hold.
func (in *Install) mapMissing(v mapView) []string {
	c, g := in.S.mapMissing(v)
	return append(c, g...)
}

// mapMissing is what the answers want on the map and it does not hold: the
// contours, and the group with its projects.
func (s *Survey) mapMissing(v mapView) (contours, group []string) {
	accounts := split(s.valueOr("accounts", ""))
	name := func(dir string) string { return s.valueOr("contour:"+dir, "") }
	for _, dir := range accounts {
		if v.contourOf(dir, name(dir)) == nil {
			contours = append(contours, "contour "+name(dir))
		}
	}
	g := s.valueOr("group", "")
	if g == "" || len(accounts) == 0 {
		return contours, nil
	}
	first := v.contourOf(accounts[0], name(accounts[0]))
	if first == nil || first.group(g) == nil {
		group = append(group, "group "+g)
	}
	for _, p := range split(s.valueOr("projects", "")) {
		if !v.holds(p) {
			group = append(group, "project "+p)
		}
	}
	return contours, group
}

// mapNow is the map as the panel's local listener gives it, for a plan and a
// check that only look: what this machine answers, or nothing when the
// panel is not up.
func mapNow(m Machine, url string) (mapView, bool) {
	var v mapView
	status, body, err := m.Fetch(url + "/api/profiles")
	if err != nil || status != 200 || json.Unmarshal(body, &v) != nil {
		return mapView{}, false
	}
	return v, true
}

func (in *Install) readMap(r *Run) (mapView, error) {
	var v mapView
	err := in.call(r, http.MethodGet, "/api/profiles", nil, &v)
	return v, err
}

func (in *Install) mapStep() *Step {
	return &Step{ID: "map", Title: "Map",
		Done: func(r *Run) (bool, error) {
			if in.localOff(r) {
				return false, nil
			}
			v, err := in.readMap(r)
			return err == nil && len(in.mapMissing(v)) == 0, nil
		},
		Apply: func(r *Run) error {
			if in.localOff(r) {
				r.Say(Warn, "the local listener is off (AACP_LOCAL_ADDR is empty in .env): the map is left to you")
				r.Remind("The map has no contours of this install: take them on the Profiles screen.")
				return nil
			}
			v, err := in.readMap(r)
			if err != nil {
				return err
			}
			accounts := in.accounts()
			ids := make([]int, len(accounts))
			for i, dir := range accounts {
				name := in.contourName(dir)
				if c := v.contourOf(dir, name); c != nil {
					ids[i] = c.ID
					r.Say(Pass, fmt.Sprintf("contour %s · on the map already", c.Name))
					continue
				}
				if err := r.Record(Profile, "contour "+name, "created"); err != nil {
					return err
				}
				var made struct {
					Profile mapContour `json:"profile"`
				}
				body := map[string]any{"name": name, "configDir": dir,
					"launch": map[string]any{"transport": in.S.valueOr("transport", "stream")}}
				if err := in.call(r, http.MethodPost, "/api/profiles", body, &made); err != nil {
					return mapFailed("the contour "+name+" of "+in.short(dir)+" was not made", err,
						"A contour of that name stands for another account: pass --contour "+in.short(dir)+"=<name>, or rename it on the Profiles screen.")
				}
				ids[i] = made.Profile.ID
				r.Say(Pass, fmt.Sprintf("contour %s · %s · sessions %s", name, in.short(dir), in.S.valueOr("transport", "stream")))
			}
			group := in.S.valueOr("group", "")
			if group == "" || len(accounts) == 0 {
				return nil
			}
			var gid int
			first := v.contourOf(accounts[0], in.contourName(accounts[0]))
			if first != nil && first.group(group) != nil {
				gid = first.group(group).ID
			} else {
				if err := r.Record(Profile, "group "+group, "created"); err != nil {
					return err
				}
				var made struct {
					Group mapGroup `json:"group"`
				}
				if err := in.call(r, http.MethodPost, fmt.Sprintf("/api/profiles/%d/groups", ids[0]), map[string]any{"name": group}, &made); err != nil {
					return mapFailed("the group "+group+" was not made", err, "")
				}
				gid = made.Group.ID
			}
			var added []string
			for _, p := range split(in.S.valueOr("projects", "")) {
				if v.holds(p) {
					r.Say(Pass, "project "+in.short(p)+" · on the map already")
					continue
				}
				if err := r.Record(Profile, "project "+p, "created"); err != nil {
					return err
				}
				body := map[string]any{"name": filepath.Base(p), "path": p}
				if err := in.call(r, http.MethodPost, fmt.Sprintf("/api/groups/%d/projects", gid), body, nil); err != nil {
					return mapFailed("the project "+in.short(p)+" was not put on the map", err, "")
				}
				added = append(added, in.short(p))
			}
			line := "group " + group
			if len(added) > 0 {
				line += " · " + strings.Join(added, ", ")
			}
			r.Say(Pass, line)
			return nil
		},
		Verify: func(r *Run) error {
			if in.localOff(r) {
				return nil
			}
			v, err := in.readMap(r)
			if err != nil {
				return err
			}
			if miss := in.mapMissing(v); len(miss) > 0 {
				return &Failed{Diagnosis: "the map does not hold " + strings.Join(miss, ", ") + " after it was made"}
			}
			return nil
		},
		Undo: UndoKind,
	}
}

// mapFailed is a request of the map the panel refused: what it said, and
// what to do about a name it took for another account.
func mapFailed(what string, err error, onConflict string) error {
	var api *apiError
	if !errors.As(err, &api) {
		return err
	}
	f := &Failed{Diagnosis: what + ": " + api.Error()}
	if api.Status == http.StatusConflict && onConflict != "" {
		f.Fix = []string{onConflict}
	}
	return f
}
