package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strings"

	"aacpanel/internal/action"
	"aacpanel/internal/store"
)

// homeLabel is what the home directory is called in the list of terminals.
const homeLabel = "Home"

// termRow is a terminal of the panel as the screen lists it.
type termRow struct {
	ID       string `json:"id"`
	Place    string `json:"place"`
	Label    string `json:"label"`
	Name     string `json:"name"`
	Command  string `json:"command"`
	Activity int64  `json:"activity"`
	Created  int64  `json:"created"`
	Clients  int    `json:"clients"`
}

// apiTerms lists the terminals of the panel, grouped by place and in the order
// they were started within one.
func (s *Server) apiTerms(w http.ResponseWriter, r *http.Request) {
	if s.exec == nil {
		http.Error(w, "the executor is not configured: there is nobody to keep terminals", http.StatusServiceUnavailable)
		return
	}
	list, err := s.exec.Terms(r.Context())
	if err != nil {
		code := http.StatusBadGateway
		if errors.Is(err, action.ErrUnavailable) {
			code = http.StatusServiceUnavailable
		}
		http.Error(w, err.Error(), code)
		return
	}
	places, err := s.termPlaces(r.Context())
	if err != nil {
		log.Printf("terminals: the map is unavailable, places go by their directories: %v", err)
	}
	rows := make([]termRow, 0, len(list))
	for _, t := range list {
		rows = append(rows, termRow{
			ID: t.ID, Place: t.Place, Label: placeLabel(places, t.Place), Name: t.Name,
			Command: t.Command, Activity: t.Activity, Created: t.Created, Clients: t.Clients,
		})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Place != rows[j].Place {
			return rows[i].Place < rows[j].Place
		}
		if rows[i].Created != rows[j].Created {
			return rows[i].Created < rows[j].Created
		}
		return rows[i].ID < rows[j].ID
	})
	writeJSON(w, map[string]any{"terms": rows})
}

// termPlaces are the places a terminal of the panel opens in, each with the
// label it goes by. A map that cannot be read leaves the home directory alone.
func (s *Server) termPlaces(ctx context.Context) (map[string]string, error) {
	if s.db == nil {
		return placesFrom(s.home, nil), nil
	}
	list, err := s.db.Profiles(ctx)
	if err != nil {
		return placesFrom(s.home, nil), err
	}
	return placesFrom(s.home, list), nil
}

// placesFrom lists the home directory and the directory of every project of
// the map. A project is called by its name, the home directory by its own
// unless a project of the map is kept there; the first project of a
// directory kept by two gives it its name.
func placesFrom(home string, list []store.Profile) map[string]string {
	places := map[string]string{}
	if dir := cleanPlace(home); dir != "" {
		places[dir] = homeLabel
	}
	named := map[string]bool{}
	for _, profile := range list {
		for _, group := range profile.Groups {
			for _, p := range group.Projects {
				dir := cleanPlace(p.Path)
				if dir == "" || named[dir] {
					continue
				}
				named[dir] = true
				places[dir] = strings.TrimSpace(p.Name)
			}
		}
	}
	return places
}

func placeLabel(places map[string]string, place string) string {
	if label := places[place]; label != "" {
		return label
	}
	return filepath.Base(place)
}

func cleanPlace(dir string) string {
	dir = strings.TrimSpace(dir)
	if !filepath.IsAbs(dir) {
		return ""
	}
	return filepath.Clean(dir)
}

// termRequest fills in an action on a terminal of the panel. The phone names
// the terminal by its id and the place by its directory, in the parameters or
// as the target; the place goes to the executor as the map lists it, and a new
// terminal gets its id here.
func (s *Server) termRequest(ctx context.Context, req *action.Request, target string, params map[string]any) (map[string]any, error) {
	if req.Kind == action.TermStart {
		asked, err := oneOf(params, "place", target)
		if err != nil {
			return nil, err
		}
		places, err := s.termPlaces(ctx)
		if err != nil {
			return nil, fmt.Errorf("the places of the map cannot be read: %w", err)
		}
		place := cleanPlace(asked)
		if _, ok := places[place]; place == "" || !ok {
			return nil, fmt.Errorf("%q is neither the home directory nor a project of the map: "+
				"a terminal opens only there", asked)
		}
		req.Target, req.Place = newTermID(), place
		return map[string]any{"place": place}, nil
	}

	id, err := oneOf(params, "id", target)
	if err != nil {
		return nil, err
	}
	req.Target = id
	if req.Kind == action.TermRename {
		req.Rename, _ = params["name"].(string)
		return map[string]any{"name": req.Rename}, nil
	}
	return nil, nil
}

// oneOf reads a value the phone may send as a parameter or as the target;
// sent both ways, it has to be the same.
func oneOf(params map[string]any, key, target string) (string, error) {
	value, _ := params[key].(string)
	switch {
	case value == "":
		return target, nil
	case target != "" && target != value:
		return "", fmt.Errorf("the target %q and the %s %q name different things", target, key, value)
	}
	return value, nil
}

func newTermID() string {
	var buf [4]byte
	rand.Read(buf[:])
	return "t-" + hex.EncodeToString(buf[:])
}
