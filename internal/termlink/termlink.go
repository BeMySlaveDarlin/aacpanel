// Package termlink describes the terminal stream between the service and the executor.
package termlink

import "fmt"

const (
	// FrameOpen is the first frame from the service: which session to attach to and at which window size.
	FrameOpen = "open"
	// FrameIn carries input from the person.
	FrameIn = "in"
	// FrameSize carries a new window size.
	FrameSize = "size"

	// FrameReady reports that the executor attached, and to what.
	FrameReady = "ready"
	// FrameOut carries bytes from the screen.
	FrameOut = "out"
	// FrameEnd reports that the bridge has ended.
	FrameEnd = "end"
)

// Frame is one frame travelling in either direction.
type Frame struct {
	Type   string `json:"t"`
	Data   []byte `json:"d,omitempty"`
	Target string `json:"target,omitempty"`
	Cols   uint16 `json:"cols,omitempty"`
	Rows   uint16 `json:"rows,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

const (
	// MaxFrame is the ceiling for one frame line.
	MaxFrame = 64 << 10
	// MaxChunk is how many screen bytes travel in one frame.
	MaxChunk = 32 << 10
	// MaxTerminals is how many bridges are held at once.
	MaxTerminals = 8
	// MaxCols and MaxRows are the ceiling for the window size.
	MaxCols = 1000
	MaxRows = 500
)

// ValidateOpen checks the first frame.
func ValidateOpen(f Frame) error {
	if f.Type != FrameOpen {
		return fmt.Errorf("the first frame was expected to be %q, got %q", FrameOpen, f.Type)
	}
	if f.Target == "" {
		return fmt.Errorf("no session to attach to was named")
	}
	return ValidateSize(f.Cols, f.Rows)
}

// ValidateSize checks a window size.
func ValidateSize(cols, rows uint16) error {
	if cols == 0 || rows == 0 {
		return fmt.Errorf("a window of %dx%d is not a terminal size", cols, rows)
	}
	if cols > MaxCols || rows > MaxRows {
		return fmt.Errorf("a window of %dx%d is beyond reason (%dx%d)", cols, rows, MaxCols, MaxRows)
	}
	return nil
}
