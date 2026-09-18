package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrNotFound means what was asked for does not exist.
var ErrNotFound = errors.New("not found")

// ErrConflict means the entry contradicts an existing one.
var ErrConflict = errors.New("conflict with an existing entry")

// ErrNotEmpty means there are projects inside.
var ErrNotEmpty = errors.New("there are projects inside")

const (
	profileNameMax = 200
	profilePathMax = 4096
	launchMax      = 8 << 10
	intentMax      = 500
)

// Profile is a contour with its own configuration directory and projects.
type Profile struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	ConfigDir string `json:"configDir"`
	Prefix    string `json:"prefix"`
	ClaudeBin string `json:"claudeBin"`
	Auth      string `json:"auth"`

	Hooks     string          `json:"hooks,omitempty"`
	Sort      int             `json:"sort"`
	Launch    json.RawMessage `json:"launch"`
	CreatedAt int64           `json:"createdAt"`
	Groups    []ProfileGroup  `json:"groups"`
}

// ProfileGroup is a group of projects inside a profile.
type ProfileGroup struct {
	ID        int              `json:"id"`
	ProfileID int              `json:"profileId"`
	Name      string           `json:"name"`
	Sort      int              `json:"sort"`
	Projects  []ProfileProject `json:"projects"`
}

// ProfileProject is a project: the thing that can be opened.
type ProfileProject struct {
	ID      int             `json:"id"`
	GroupID int             `json:"groupId"`
	Name    string          `json:"name"`
	Path    string          `json:"path"`
	Session string          `json:"session"`
	Base    string          `json:"base"`
	Sort    int             `json:"sort"`
	Launch  json.RawMessage `json:"launch"`
}

// ProfileEdit holds the profile's fields that can be set.
type ProfileEdit struct {
	Name      *string         `json:"name"`
	ConfigDir *string         `json:"configDir"`
	Prefix    *string         `json:"prefix"`
	ClaudeBin *string         `json:"claudeBin"`
	Sort      *int            `json:"sort"`
	Launch    json.RawMessage `json:"launch"`
}

// GroupEdit holds the group's fields.
type GroupEdit struct {
	Name        *string `json:"name"`
	ProfileID   *int    `json:"profileId"`
	Sort        *int    `json:"sort"`
	MoveProfile bool    `json:"moveProfile"`
	MoveContour bool    `json:"moveContour"`
}

// Agreed reports whether consent to moving the group was given.
func (e GroupEdit) Agreed() bool { return e.MoveProfile || e.MoveContour }

// ProjectEdit holds the project's fields.
type ProjectEdit struct {
	Name        *string         `json:"name"`
	Path        *string         `json:"path"`
	Session     *string         `json:"session"`
	Base        *string         `json:"base"`
	GroupID     *int            `json:"groupId"`
	Sort        *int            `json:"sort"`
	MoveProfile bool            `json:"moveProfile"`
	MoveContour bool            `json:"moveContour"`
	Launch      json.RawMessage `json:"launch"`
}

// Profiles returns the whole map: profiles, their groups and the projects inside.
func (s *Store) Profiles(ctx context.Context) (out []Profile, err error) {
	defer func() { err = Unavailable(err) }()

	pool, err := s.Pool()
	if err != nil {
		return nil, err
	}

	rows, err := pool.Query(ctx, `
		SELECT `+profileCols+`
		FROM profiles ORDER BY sort, id`)
	if err != nil {
		return nil, err
	}
	profiles, err := pgx.CollectRows(rows, scanProfile)
	if err != nil {
		return nil, err
	}

	at := make(map[int]int, len(profiles))
	for i := range profiles {
		at[profiles[i].ID] = i
	}

	rows, err = pool.Query(ctx, `
		SELECT id, profile_id, name, sort FROM profile_groups ORDER BY profile_id, sort, id`)
	if err != nil {
		return nil, err
	}
	groups, err := pgx.CollectRows(rows, scanGroup)
	if err != nil {
		return nil, err
	}
	groupAt := make(map[int][2]int, len(groups))
	for _, g := range groups {
		i, ok := at[g.ProfileID]
		if !ok {
			continue
		}
		g.Projects = []ProfileProject{}
		profiles[i].Groups = append(profiles[i].Groups, g)
		groupAt[g.ID] = [2]int{i, len(profiles[i].Groups) - 1}
	}

	rows, err = pool.Query(ctx, `
		SELECT id, group_id, name, path, session_name, base_branch, sort, launch
		FROM profile_projects ORDER BY group_id, sort, id`)
	if err != nil {
		return nil, err
	}
	projects, err := pgx.CollectRows(rows, scanProject)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		pos, ok := groupAt[p.GroupID]
		if !ok {
			continue
		}
		g := &profiles[pos[0]].Groups[pos[1]]
		g.Projects = append(g.Projects, p)
	}

	if profiles == nil {
		profiles = []Profile{}
	}
	return profiles, nil
}

const profileCols = `id, name, config_dir, prefix, claude_bin, sort, launch, created_at`

func scanProfile(r pgx.CollectableRow) (Profile, error) {
	var p Profile
	var created time.Time
	if err := r.Scan(&p.ID, &p.Name, &p.ConfigDir, &p.Prefix, &p.ClaudeBin, &p.Sort, &p.Launch, &created); err != nil {
		return p, err
	}
	p.CreatedAt = created.Unix()
	p.Groups = []ProfileGroup{}
	return p, nil
}

func scanGroup(r pgx.CollectableRow) (ProfileGroup, error) {
	var g ProfileGroup
	err := r.Scan(&g.ID, &g.ProfileID, &g.Name, &g.Sort)
	return g, err
}

func scanProject(r pgx.CollectableRow) (ProfileProject, error) {
	var p ProfileProject
	err := r.Scan(&p.ID, &p.GroupID, &p.Name, &p.Path, &p.Session, &p.Base, &p.Sort, &p.Launch)
	return p, err
}

type rowOnly struct{ row pgx.Row }

func (r rowOnly) Scan(dest ...any) error { return r.row.Scan(dest...) }
func (r rowOnly) Err() error             { return nil }
func (r rowOnly) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}
func (r rowOnly) RawValues() [][]byte { return nil }
func (r rowOnly) Values() ([]any, error) {
	return nil, errors.New("the values of a single row cannot be read")
}
func (r rowOnly) Conn() *pgx.Conn { return nil }
