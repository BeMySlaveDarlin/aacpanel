package action

import (
	"errors"
	"time"

	"aacpanel/internal/heavy"
	"aacpanel/internal/question"
	"aacpanel/internal/sessionintent"
)

var (
	ErrTermNotFound     = errors.New("there is no panel terminal")
	ErrSessionNotFound  = errors.New("there is no live session")
	ErrSessionAmbiguous = errors.New("the session name is ambiguous")
)

const (
	TermNotFound     = "term_not_found"
	SessionNotFound  = "session_not_found"
	SessionAmbiguous = "session_ambiguous"
)

// Response is what came out.
type Response struct {
	ID string `json:"id"`
	OK bool   `json:"ok"`

	Error string `json:"error,omitempty"`
	// Lookup codes survive the executor socket so callers need not parse
	// human wording to distinguish a missing name from an ambiguous one.
	Code string `json:"code,omitempty"`

	DurationMs int64 `json:"durationMs"`

	Detail string `json:"detail,omitempty"`

	// SecretPath survives a notification failure after the file was saved.
	SecretPath string `json:"secretPath,omitempty"`

	// Session names the session a New or a resume of codex brought up, where
	// the executor knows it: a codex thread is named by its id, which nobody
	// knows before.
	Session   string         `json:"session,omitempty"`
	Ownership *OwnerTransfer `json:"ownership,omitempty"`

	Kinds []Kind `json:"kinds,omitempty"`

	Permission     *Permission     `json:"permission,omitempty"`
	Opening        *Opening        `json:"opening,omitempty"`
	SleepingAlarms *SleepingAlarms `json:"sleepingAlarms,omitempty"`

	Window *Window `json:"window,omitempty"`

	Models *Models `json:"models,omitempty"`

	Mcp *Mcp `json:"mcp,omitempty"`

	Status *Status `json:"status,omitempty"`

	Intents sessionintent.Values `json:"intents,omitempty"`

	Setup *Setup `json:"setup,omitempty"`

	Question *question.Verdict `json:"question,omitempty"`

	Side *Side `json:"side,omitempty"`

	Commands *SessionCommands `json:"commands,omitempty"`

	TermPeek *TermPeekResult `json:"termPeek,omitempty"`

	Terms []Term `json:"terms,omitempty"`

	Secrets *Secrets `json:"secrets,omitempty"`

	CodexModels []CodexModel `json:"codexModels,omitempty"`

	Processes []Process   `json:"processes,omitempty"`
	Heavy     *heavy.View `json:"heavy,omitempty"`
	Headroom  *Headroom   `json:"headroom,omitempty"`
}

// Failed builds a refusal response.
func Failed(id string, err error, took time.Duration) Response {
	resp := Response{ID: id, OK: false, Error: err.Error(), DurationMs: took.Milliseconds()}
	var saved *SavedSecretError
	if errors.As(err, &saved) {
		resp.SecretPath = saved.Path
	}
	switch {
	case errors.Is(err, ErrTermNotFound):
		resp.Code = TermNotFound
	case errors.Is(err, ErrSessionNotFound):
		resp.Code = SessionNotFound
	case errors.Is(err, ErrSessionAmbiguous):
		resp.Code = SessionAmbiguous
	}
	return resp
}

// Done builds a success response.
func Done(id, detail string, took time.Duration) Response {
	return Response{ID: id, OK: true, Detail: detail, DurationMs: took.Milliseconds()}
}
