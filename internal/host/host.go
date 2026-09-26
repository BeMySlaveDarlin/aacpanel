package host

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sync"
	"time"
)

type state struct {
	At           int64           `json:"at"`
	Host         json.RawMessage `json:"host"`
	Sessions     json.RawMessage `json:"sessions"`
	SessionNotes json.RawMessage `json:"sessionNotes"`
	Limits       json.RawMessage `json:"limits"`
	SessionsAt   int64           `json:"sessionsAt"`
	Probes       json.RawMessage `json:"probes"`
	Profiles     json.RawMessage `json:"profiles"`
	Models       json.RawMessage `json:"models"`
}

// Reader reads the state written by the agent and caches it for a second.
type Reader struct {
	path string

	mu     sync.Mutex
	cached []byte
	raw    []byte
	readAt time.Time
	err    error
}

func ageOrZero(at int64) int64 {
	if at == 0 {
		return 0
	}
	return time.Now().Unix() - at
}

func NewReader(path string) *Reader {
	return &Reader{path: path}
}

// Raw returns the snapshot exactly as the agent wrote it.
func (h *Reader) Raw() ([]byte, error) {
	if _, err := h.JSON(); err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.raw, nil
}

// JSON returns the agent snapshot with its age added.
func (h *Reader) JSON() ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if time.Since(h.readAt) < time.Second {
		return h.cached, h.err
	}
	h.readAt = time.Now()

	raw, err := os.ReadFile(h.path)
	h.raw = nil
	if err != nil {
		h.cached, h.err = nil, fmt.Errorf("the host snapshot is unavailable (%s): %w", h.path, err)
		if errors.Is(err, os.ErrNotExist) {
			h.err = fmt.Errorf("the agent has not written a snapshot yet (%s); is aacpanel-agent running?", h.path)
		}
		return nil, h.err
	}

	var st state
	if err := json.Unmarshal(raw, &st); err != nil {
		h.cached, h.err = nil, fmt.Errorf("the host snapshot cannot be parsed: %w", err)
		return nil, h.err
	}
	h.raw = raw

	out := map[string]any{
		"at":             st.At,
		"ageSec":         time.Now().Unix() - st.At,
		"host":           st.Host,
		"sessions":       st.Sessions,
		"sessionNotes":   st.SessionNotes,
		"limits":         st.Limits,
		"sessionsAt":     st.SessionsAt,
		"sessionsAgeSec": ageOrZero(st.SessionsAt),
		"probes":         st.Probes,
		"profiles":       st.Profiles,
		"models":         st.Models,
	}
	payload, err := json.Marshal(out)
	if err != nil {
		h.cached, h.err = nil, err
		return nil, err
	}
	h.cached, h.err = payload, nil
	return payload, nil
}

// LiveSession is what the agent snapshot knows about a live session.
type LiveSession struct {
	Name      string `json:"session"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	// Transport is "stream" for a session its holder keeps on the stream, and
	// empty for a terminal.
	Transport string `json:"transport"`
}

// Worktrees maps the git worktrees the agent saw on disk to their main
// checkouts. A session run in a worktree belongs to the project of the main
// checkout, and the service cannot look at the disk to find it out.
func (h *Reader) Worktrees() map[string]string {
	raw, err := h.Raw()
	if err != nil {
		return nil
	}
	var snapshot struct {
		Projects struct {
			Dirs []struct {
				Path       string `json:"path"`
				WorktreeOf string `json:"worktreeOf"`
			} `json:"dirs"`
		} `json:"projects"`
	}
	if json.Unmarshal(raw, &snapshot) != nil {
		return nil
	}
	out := map[string]string{}
	for _, d := range snapshot.Projects.Dirs {
		if d.WorktreeOf != "" {
			out[d.Path] = d.WorktreeOf
		}
	}
	return out
}

// LiveSession returns a live session by name together with whether it was found.
func (h *Reader) LiveSession(name string) (LiveSession, bool) {
	return h.liveSession(func(s LiveSession) bool { return s.Name == name })
}

// LiveSessionOf returns the live session a conversation runs in. A session
// that asks the panel about itself knows its conversation, not the name the
// panel calls it by.
func (h *Reader) LiveSessionOf(conversation string) (LiveSession, bool) {
	if conversation == "" {
		return LiveSession{}, false
	}
	return h.liveSession(func(s LiveSession) bool { return s.SessionID == conversation })
}

func (h *Reader) liveSession(match func(LiveSession) bool) (LiveSession, bool) {
	payload, err := h.JSON()
	if err != nil {
		return LiveSession{}, false
	}
	var snapshot struct {
		Sessions []LiveSession `json:"sessions"`
	}
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return LiveSession{}, false
	}
	for _, s := range snapshot.Sessions {
		if match(s) {
			return s, true
		}
	}
	return LiveSession{}, false
}

// AuthBuiltin, AuthToken and AuthMissing are the authorization states of a contour.
const (
	AuthBuiltin = "builtin"
	AuthToken   = "token"
	AuthMissing = "missing"

	// HooksSame and the states next to it describe the profile settings against the personal ones.
	HooksSame     = "same"
	HooksDiverged = "diverged"
	HooksUnknown  = "unknown"
)

// ContourState is a contour as the host agent sees it.
type ContourState struct {
	Name      string
	ConfigDir string
	Auth      string
	Hooks     string
	// Account is what the account starts a session with — model, effort,
	// permissionMode — as its settings say; nil where they were not read.
	Account map[string]string
	// ContextGuard says whether the account runs the context guard hook; nil
	// where its settings were not read.
	ContextGuard *bool
}

// Contours returns the host contours from the agent snapshot, in snapshot order.
func (h *Reader) Contours() []ContourState {
	payload, err := h.JSON()
	if err != nil {
		return nil
	}
	var snapshot struct {
		Profiles []struct {
			Name         string            `json:"name"`
			ConfigDir    string            `json:"configDir"`
			Auth         string            `json:"auth"`
			Hooks        string            `json:"hooks"`
			Account      map[string]string `json:"account"`
			ContextGuard *bool             `json:"contextGuard"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return nil
	}
	out := make([]ContourState, 0, len(snapshot.Profiles))
	for _, c := range snapshot.Profiles {
		if c.Name == "" && c.ConfigDir == "" {
			continue
		}
		row := ContourState{Name: c.Name, ConfigDir: c.ConfigDir, Account: c.Account, ContextGuard: c.ContextGuard}
		switch c.Auth {
		case AuthBuiltin, AuthToken, AuthMissing:
			row.Auth = c.Auth
		}
		switch c.Hooks {
		case HooksSame, HooksDiverged, HooksUnknown:
			row.Hooks = c.Hooks
		}
		out = append(out, row)
	}
	return out
}

// CatalogOK, CatalogUnknown and CatalogError are the states of the model catalog of a contour.
const (
	CatalogOK      = "ok"
	CatalogUnknown = "unknown"
	CatalogError   = "error"
)

// Model is a model of a contour: identifier, human name and context window.
type Model struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Window int    `json:"window,omitempty"`
	Output int    `json:"output,omitempty"`
}

// Catalog is the model catalog of one contour.
type Catalog struct {
	State  string  `json:"state"`
	At     int64   `json:"at,omitempty"`
	Error  string  `json:"error,omitempty"`
	Models []Model `json:"models,omitempty"`
}

var errorCode = regexp.MustCompile(`^[a-z0-9-]{1,16}$`)

// ModelCatalog returns the model catalog of the host, one for all contours.
func (h *Reader) ModelCatalog() Catalog {
	rows := h.contourCatalogs()
	for _, row := range rows {
		if row.State == CatalogOK && len(row.Models) > 0 {
			return row
		}
	}
	if len(rows) > 0 {
		return Catalog{State: CatalogUnknown, Error: rows[0].Error}
	}
	return Catalog{State: CatalogUnknown}
}

func (h *Reader) contourCatalogs() []Catalog {
	payload, err := h.JSON()
	if err != nil {
		return nil
	}
	var snapshot struct {
		Models struct {
			Contours []struct {
				Profile string  `json:"profile"`
				State   string  `json:"state"`
				At      int64   `json:"at"`
				Error   string  `json:"error"`
				Models  []Model `json:"models"`
			} `json:"contours"`
		} `json:"models"`
	}
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return nil
	}
	out := make([]Catalog, 0, len(snapshot.Models.Contours))
	for _, c := range snapshot.Models.Contours {
		if c.Profile == "" {
			continue
		}
		switch c.State {
		case CatalogOK, CatalogUnknown, CatalogError:
		default:
			continue
		}
		row := Catalog{State: c.State, At: c.At}
		if errorCode.MatchString(c.Error) {
			row.Error = c.Error
		}
		for _, m := range c.Models {
			if m.ID != "" {
				row.Models = append(row.Models, m)
			}
		}
		out = append(out, row)
	}
	return out
}
