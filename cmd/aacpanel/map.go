package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"path"
	"sort"
	"strings"

	"aacpanel/internal/action"
	"aacpanel/internal/chat"
	"aacpanel/internal/store"
)

type projectNode struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Comment string `json:"comment"`
	Path    string `json:"path"`
	Session string `json:"session"`
}

type groupNode struct {
	Name     string        `json:"name"`
	Projects []projectNode `json:"projects"`
}

type profileNode struct {
	ID      int         `json:"id"`
	Profile string      `json:"profile"`
	Groups  []groupNode `json:"groups"`
}

func (s *Server) hostSnapshot(ctx context.Context) ([]byte, error) {
	payload, err := s.host.JSON()
	if err != nil {
		return nil, err
	}
	payload, err = spliceField(payload, "hostName", s.hostName)
	if err != nil {
		return nil, err
	}
	if s.insecureSeen.Load() {
		if payload, err = spliceField(payload, "cookieInsecure", true); err != nil {
			return nil, err
		}
	}
	var list []store.Profile
	if s.db != nil {
		got, err := s.db.Profiles(ctx)
		if err != nil {
			log.Printf("profile map: %v", err)
		}
		list = got
	}
	payload = limitsByContour(payload, list)
	payload = sessionsByContour(payload, list)
	if len(list) == 0 {
		return payload, nil
	}
	payload = sessionsByProject(payload, list, s.worktrees(), s.db.ProjectRoots())
	tree := profileMap(list)
	if len(tree) == 0 {
		return payload, nil
	}
	spliced, err := spliceField(payload, "profileMap", tree)
	if err != nil {
		log.Printf("profile map: %v", err)
		return payload, nil
	}
	return spliced, nil
}

func spliceField(payload []byte, key string, value any) ([]byte, error) {
	var out map[string]json.RawMessage
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	out[key] = raw
	return json.Marshal(out)
}

func profileMap(list []store.Profile) []profileNode {
	order := make([]store.Profile, len(list))
	copy(order, list)
	sort.SliceStable(order, func(i, j int) bool {
		return order[i].Prefix == "" && order[j].Prefix != ""
	})

	out := make([]profileNode, 0, len(order))
	for _, profile := range order {
		groups := make([]groupNode, 0, len(profile.Groups))
		for _, group := range profile.Groups {
			projects := make([]projectNode, 0, len(group.Projects))
			for _, p := range group.Projects {
				projects = append(projects, projectNode{
					ID:      p.ID,
					Name:    p.Name,
					Path:    p.Path,
					Session: sessionNameOf(p),
				})
			}
			if len(projects) == 0 {
				continue
			}
			groups = append(groups, groupNode{Name: group.Name, Projects: projects})
		}
		if len(groups) == 0 {
			continue
		}
		out = append(out, profileNode{ID: profile.ID, Profile: profile.Name, Groups: groups})
	}
	return out
}

func sessionNameOf(p store.ProfileProject) string {
	if name := strings.TrimSpace(p.Session); name != "" {
		return name
	}
	return path.Base(strings.TrimRight(p.Path, "/"))
}

type mapProject struct {
	profile store.Profile
	group   string
	project store.ProfileProject
	// at is where the conversation runs when that is not the project's own
	// directory: a worktree of it or a directory inside it. The conversation is
	// resumed there — its transcript is kept by that directory — with the
	// launch parameters of the project.
	at string
}

func (s *Server) launchProject(ctx context.Context, params map[string]any, cwd, target string) (*action.Project, int, error) {
	id, err := projectFromParams(params)
	if err != nil {
		return nil, 0, err
	}
	if s.db == nil {
		if id > 0 {
			return nil, 0, fmt.Errorf("the project from the map cannot be found: the database is not configured")
		}
		return nil, 0, nil
	}
	list, err := s.db.Profiles(ctx)
	if err != nil {
		if id > 0 {
			return nil, 0, fmt.Errorf("the project from the map cannot be found: %w", err)
		}
		log.Printf("the project to launch in: the map is unavailable, the executor will look for it: %v", err)
		return nil, 0, nil
	}

	found, err := locateProject(list, id, cwd, target, s.worktrees(), s.db.ProjectRoots())
	if err != nil {
		return nil, 0, err
	}
	if found == nil {
		return nil, 0, nil
	}

	at := found.project.Path
	if found.at != "" {
		at = found.at
	}
	dir, err := store.CheckProjectPath(at, s.db.ProjectRoots())
	if err != nil {
		return nil, 0, fmt.Errorf("project %q cannot be opened: %w", found.project.Name, err)
	}
	launch, err := store.EffectiveLaunch(found.profile.Launch, found.project.Launch)
	if err != nil {
		return nil, 0, fmt.Errorf("project %q cannot be opened: %w", found.project.Name, err)
	}
	return &action.Project{
		Path:      dir,
		Session:   sessionNameOf(found.project),
		Launch:    launch,
		ClaudeBin: found.profile.ClaudeBin,
		ConfigDir: found.profile.ConfigDir,
	}, found.project.ID, nil
}

func (s *Server) worktrees() map[string]string {
	if s.host == nil {
		return nil
	}
	return s.host.Worktrees()
}

// projectOwning finds the project a directory outside the map belongs to:
// the nearest directory above it that is a project, or the main checkout of
// the git worktree it is in. Worktrees kept inside a project and directories
// inside a project are found the first way, worktrees kept beside the
// repository the second. The climb stops below a root: a project that is a
// root itself — the home directory — holds the machine's own session, not the
// projects under it, and taking it for their owner would start them in its
// contour.
func projectOwning(list []store.Profile, dir string, worktreeOf map[string]string, roots []string) (mapProject, bool) {
	for d := dir; belowRoot(d, roots); d = path.Dir(d) {
		if found, ok := projectByPath(list, d); ok {
			return found, true
		}
		if main := worktreeOf[d]; main != "" && main != dir {
			if found, ok := projectOwning(list, main, nil, roots); ok {
				return found, true
			}
		}
	}
	return mapProject{}, false
}

func belowRoot(dir string, roots []string) bool {
	for _, root := range roots {
		root = strings.TrimRight(root, "/")
		if root != "" && strings.HasPrefix(dir, root+"/") {
			return true
		}
	}
	return false
}

func locateProject(list []store.Profile, id int, cwd, target string, worktreeOf map[string]string, roots []string) (*mapProject, error) {
	if id > 0 {
		found, ok := projectByID(list, id)
		if !ok {
			return nil, fmt.Errorf("there is no project with id %d in the map — refresh the page", id)
		}
		return &found, nil
	}
	if found, ok := projectAt(list, cwd, worktreeOf, roots); ok {
		return &found, nil
	}
	if name := strings.TrimSpace(target); name != "" {
		switch hits := projectsBySession(list, name); len(hits) {
		case 0:
		case 1:
			return &hits[0], nil
		default:
			where := make([]string, 0, len(hits))
			for _, hit := range hits {
				where = append(where, hit.profile.Name+" · "+hit.project.Path)
			}
			return nil, fmt.Errorf("the map holds several projects named %q (%s) — open the right one from the projects list",
				name, strings.Join(where, ", "))
		}
	}
	return nil, nil
}

// projectAt finds the project a working directory belongs to: the project
// whose directory it is, or the one that owns it — see projectOwning. at is set
// when the directory is not the project's own.
func projectAt(list []store.Profile, cwd string, worktreeOf map[string]string, roots []string) (mapProject, bool) {
	dir := strings.TrimRight(strings.TrimSpace(cwd), "/")
	if dir == "" {
		return mapProject{}, false
	}
	if found, ok := projectByPath(list, dir); ok {
		return found, true
	}
	if found, ok := projectOwning(list, path.Clean(dir), worktreeOf, roots); ok {
		found.at = path.Clean(dir)
		return found, true
	}
	return mapProject{}, false
}

// ref is the entry of the map a session row carries: which project it ran in
// and the session name a new one of that project comes up under.
func (m mapProject) ref() *chat.ArchiveProject {
	return &chat.ArchiveProject{
		ID: m.project.ID, Name: m.project.Name, Session: sessionNameOf(m.project), Path: m.project.Path,
		Group: m.group,
	}
}

// sessionsByProject tells every live session the project of the map it
// belongs to, found the way a resume of it finds it: by its directory — the
// project's own, one inside it, or a git worktree of it — and failing that by
// its name. A session no project owns carries null: the screens show it
// outside the map instead of guessing on their own, and a worktree kept
// beside its repository is known only here.
func sessionsByProject(payload []byte, list []store.Profile, worktreeOf map[string]string, roots []string) []byte {
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return payload
	}
	var rows []map[string]json.RawMessage
	if err := json.Unmarshal(snapshot["sessions"], &rows); err != nil {
		return payload
	}
	for _, row := range rows {
		var ref *chat.ArchiveProject
		found, err := locateProject(list, 0, textField(row, "cwd"), textField(row, "session"), worktreeOf, roots)
		if err == nil && found != nil {
			ref = found.ref()
		}
		raw, err := json.Marshal(ref)
		if err != nil {
			return payload
		}
		row["project"] = raw
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		return payload
	}
	snapshot["sessions"] = raw
	out, err := json.Marshal(snapshot)
	if err != nil {
		return payload
	}
	return out
}

// textField reads a string field of a snapshot row; anything else reads as empty.
func textField(row map[string]json.RawMessage, key string) string {
	var out string
	if json.Unmarshal(row[key], &out) != nil {
		return ""
	}
	return out
}

func projectByID(list []store.Profile, id int) (mapProject, bool) {
	for _, profile := range list {
		for _, group := range profile.Groups {
			for _, p := range group.Projects {
				if p.ID == id {
					return mapProject{profile: profile, group: group.Name, project: p}, true
				}
			}
		}
	}
	return mapProject{}, false
}

func projectByPath(list []store.Profile, dir string) (mapProject, bool) {
	for _, profile := range list {
		for _, group := range profile.Groups {
			for _, p := range group.Projects {
				if strings.TrimRight(p.Path, "/") == dir {
					return mapProject{profile: profile, group: group.Name, project: p}, true
				}
			}
		}
	}
	return mapProject{}, false
}

func projectsBySession(list []store.Profile, name string) []mapProject {
	var out []mapProject
	for _, profile := range list {
		for _, group := range profile.Groups {
			for _, p := range group.Projects {
				base := path.Base(strings.TrimRight(p.Path, "/"))
				if sessionNameOf(p) == name || base == name {
					out = append(out, mapProject{profile: profile, group: group.Name, project: p})
				}
			}
		}
	}
	return out
}

func projectFromParams(params map[string]any) (int, error) {
	raw, ok := params["project"]
	if !ok || raw == nil {
		return 0, nil
	}
	num, ok := raw.(float64)
	if !ok || num != math.Trunc(num) || num < 1 {
		return 0, fmt.Errorf("the project id did not arrive as a positive integer")
	}
	return int(num), nil
}
