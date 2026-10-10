package action

import (
	"regexp"
	"slices"
	"strings"
)

// Modes are the permission modes a person picks from. The two left out — no
// questions at all, and asking nobody — are chosen at the launch or not at all:
// a tap on a phone is not how a session stops asking before it acts.
var Modes = []string{"default", "acceptEdits", "plan", "auto"}

// modelID is a model named by its identifier rather than an alias: the older
// models of a catalogue have no alias, and /model takes them by id. The shape
// is all the check can hold the name to — which ids exist is the account's to
// say, and claude refuses one it does not know.
var modelID = regexp.MustCompile(`^claude-[a-z0-9]+(?:-[a-z0-9]+){1,6}(?:\[1m\])?$`)

// CodexModes are the permission modes a person picks for a codex session:
// read only, asking for approval, and approval left to the reviewer of codex
// itself, which asks the person only about what it finds unsafe.
var CodexModes = []string{"read-only", "ask", "auto"}

// codexWithheld are the settings of codex the panel does not set, with why.
// Like the two modes of claude left out of Modes, they stop codex asking
// before it acts, and a tap on a phone is not how that is decided.
var codexWithheld = map[string]string{
	"full-access": "full access lets codex write anywhere and reach the network without asking",
	"never":       "a codex that never asks acts with nobody to stop it",
}

// codexModel and codexEffort are the shapes of what codex names a model and
// an effort by. Codex lists both itself, and the daemon of the session's
// contour is what says which exist: the shape is all this check holds them to.
var (
	codexModel  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	codexEffort = regexp.MustCompile(`^[a-z]{1,16}$`)
)

// Setting is one setting of a session, and exactly one of its fields is set:
// a pick in a list changes one thing, and a request that changed two would be
// two actions under one entry of the journal. A model of codex may bring its
// effort along: the efforts are the model's own, and one picked for the model
// the session leaves is a pick that never runs.
//
// Scope says how long a model or an effort holds: for this session alone, or
// as the default new sessions start with. Empty is what the session does by
// itself — on the stream a pick holds for the session, in a terminal claude
// saves it as the default.
//
// Plan is codex's plan mode, a setting of its own beside the permissions;
// claude plans in a permission mode.
type Setting struct {
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
	Mode   string `json:"mode,omitempty"`
	Scope  string `json:"scope,omitempty"`
	Plan   *bool  `json:"plan,omitempty"`
}

// The scopes of a setting.
const (
	ScopeSession = "session"
	ScopeDefault = "default"
)

// Ultracode is the effort above the others: xhigh with workflows run for every
// task. Claude holds it for a session only, and never saves it as a default.
const Ultracode = "ultracode"

// Models is what a session can be switched to. A session on the stream
// lists its models as claude does, with the efforts each takes; a terminal
// lists nothing, and the panel falls back on the catalogue of the account.
type Models struct {
	Transport string  `json:"transport"`
	List      []Model `json:"list,omitempty"`
	Mode      string  `json:"mode,omitempty"`
	Effort    string  `json:"effort,omitempty"`
	Picked    string  `json:"picked,omitempty"`
}

// Model is one entry of the list: the value /model takes, the model it stands
// for, and the words claude describes it with.
type Model struct {
	Value       string   `json:"value"`
	Resolved    string   `json:"resolved,omitempty"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Efforts     []string `json:"efforts,omitempty"`
}

// ModelName says whether /model may take this: an alias of the list, or a
// model named by its id.
func ModelName(name string) bool {
	return slices.Contains(Commands["model"], name) || modelID.MatchString(name)
}

func (s *Setting) validate() error {
	if s == nil {
		return badRequest("a change of settings that names no setting")
	}
	set := 0
	for _, v := range []string{s.Model, s.Effort, s.Mode} {
		if v != "" {
			set++
		}
	}
	if s.Plan != nil {
		set++
	}
	if set != 1 && (set != 2 || s.Model == "" || s.Effort == "") {
		return badRequest("one setting at a time: model, effort, mode or plan — only a model brings its effort along")
	}
	switch {
	case s.Model != "" && !ModelName(s.Model) && !codexModel.MatchString(s.Model):
		return badRequest("there is no model %q", s.Model)
	case s.Effort != "" && !slices.Contains(Commands["effort"], s.Effort) && !codexEffort.MatchString(s.Effort):
		return badRequest("there is no effort %q; claude has %s, and codex names those of each model",
			s.Effort, strings.Join(Commands["effort"], ", "))
	case codexWithheld[s.Mode] != "":
		return badRequest("the %s mode is not set from the panel: %s — it is chosen at the start, in the map, "+
			"or in codex itself", s.Mode, codexWithheld[s.Mode])
	case s.Mode != "" && !slices.Contains(Modes, s.Mode) && !slices.Contains(CodexModes, s.Mode):
		return badRequest("there is no permission mode %q; claude has %s, codex has %s", s.Mode,
			strings.Join(Modes, ", "), strings.Join(CodexModes, ", "))
	case s.Scope != "" && s.Scope != ScopeSession && s.Scope != ScopeDefault:
		return badRequest("there is no scope %q; there are %s and %s", s.Scope, ScopeSession, ScopeDefault)
	case s.Scope != "" && s.Mode != "":
		return badRequest("a permission mode has no scope: it holds for the session it is set in")
	case s.Scope != "" && s.Plan != nil:
		return badRequest("plan mode has no scope: it holds for the session it is set in")
	case s.Scope == ScopeDefault && s.Effort == Ultracode:
		return badRequest("ultracode holds for a session only: claude never saves it as a default")
	}
	return nil
}
