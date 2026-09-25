package action

import "time"

// Response is what came out.
type Response struct {
	ID string `json:"id"`
	OK bool   `json:"ok"`

	Error string `json:"error,omitempty"`

	DurationMs int64 `json:"durationMs"`

	Detail string `json:"detail,omitempty"`

	Kinds []Kind `json:"kinds,omitempty"`

	Permission *Permission `json:"permission,omitempty"`

	Window *Window `json:"window,omitempty"`

	Models *Models `json:"models,omitempty"`

	Mcp *Mcp `json:"mcp,omitempty"`

	Status *Status `json:"status,omitempty"`

	Side *Side `json:"side,omitempty"`

	Commands *SessionCommands `json:"commands,omitempty"`
}

// Failed builds a refusal response.
func Failed(id string, err error, took time.Duration) Response {
	return Response{ID: id, OK: false, Error: err.Error(), DurationMs: took.Milliseconds()}
}

// Done builds a success response.
func Done(id, detail string, took time.Duration) Response {
	return Response{ID: id, OK: true, Detail: detail, DurationMs: took.Milliseconds()}
}
