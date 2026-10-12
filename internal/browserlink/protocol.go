// Package browserlink describes the closed browser bridge, never raw CDP.
package browserlink

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The image limits are shared by the holder, service and viewer. Keeping them
// here prevents an advertised viewport from exceeding the holder budget.
const (
	// Protocol versions the attachment handshake between service, bridge and holder.
	Protocol = 1
	// MaxFrame includes the local JSON envelope and base64 representation of a JPEG.
	MaxFrame = 8 << 20
	// MaxText bounds one inserted text event in bytes.
	MaxText = 16 << 10
	// MaxWidth caps the emulated CSS viewport width.
	MaxWidth = 2560
	// MaxHeight caps the emulated CSS viewport height.
	MaxHeight = 2560
	// MaxPixels caps the rendered surface after its device scale is applied.
	MaxPixels = 6_000_000
	// RenderDPR caps Chrome rendering and the scale a phone may request.
	RenderDPR = 2
	// JPEGQuality balances legible text against image traffic over the WAN.
	JPEGQuality = 60
	// FramesPerSecond bounds delivery while preserving the latest static frame.
	FramesPerSecond = 12
	// MaxProfiles caps simultaneously open holders, not saved profiles.
	MaxProfiles = 2
)

// Size describes the emulated CSS viewport and the requested image scale.
type Size struct {
	Width  int     `json:"width"`
	Height int     `json:"height"`
	DPR    float64 `json:"dpr"`
	Mobile bool    `json:"mobile"`
}

// Check caps dimensions and total pixels before Chrome allocates a surface.
func (s Size) Check() error {
	if s.Width < 100 ||
		s.Height < 100 ||
		s.Width > MaxWidth ||
		s.Height > MaxHeight ||
		math.IsNaN(s.DPR) ||
		math.IsInf(s.DPR, 0) ||
		s.DPR < 1 ||
		s.DPR > RenderDPR ||
		float64(s.Width*s.Height)*s.DPR*s.DPR > MaxPixels {
		return errors.New("the browser viewport is outside its limits")
	}
	return nil
}

// ID accepts only panel-generated opaque ids, which also form directory names.
func ID(s string) bool {
	if len(s) != 18 || !strings.HasPrefix(s, "b-") {
		return false
	}
	for _, c := range s[2:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// Name validates display metadata without interpreting it as a path.
func Name(s string) error {
	if !utf8.ValidString(s) || strings.TrimSpace(s) == "" || utf8.RuneCountInString(s) > 40 {
		return errors.New("a browser profile name must have 1 to 40 characters")
	}
	for _, c := range s {
		if !unicode.IsPrint(c) {
			return errors.New("a browser profile name must be printable")
		}
	}
	return nil
}

// Navigation limits the address bar to the web. In-page data and javascript
// links remain Chrome behavior; file access has a separate request guard.
func Navigation(raw string) error {
	if raw == "about:blank" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil ||
		len(raw) > 8192 ||
		u == nil ||
		u.Hostname() == "" ||
		u.User != nil ||
		(u.Scheme != "http" && u.Scheme != "https") {
		return errors.New("the browser opens only http, https and about:blank addresses")
	}
	return nil
}

// Profile exposes metadata and holder liveness, never stored browser credentials.
type Profile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Open bool   `json:"open"`
}

// Limits tells viewers how large a viewport the host can render.
type Limits struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	DPR    int `json:"dpr"`
	Pixels int `json:"pixels"`
}

// ViewLimits publishes the same bounds enforced on every attachment and resize.
func ViewLimits() Limits { return Limits{MaxWidth, MaxHeight, RenderDPR, MaxPixels} }

// Status is an explicit capability; a missing status never enables browsing.
type Status struct {
	Limits   Limits    `json:"limits"`
	Enabled  bool      `json:"enabled"`
	Reason   string    `json:"reason"`
	Profiles []Profile `json:"profiles"`
}

// Tab contains only the identity and labels needed by the tab strip.
type Tab struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// State names the selected tab and whether its history can move.
type State struct {
	Tabs    []Tab  `json:"tabs"`
	Current string `json:"current"`
	Back    bool   `json:"back"`
	Forward bool   `json:"forward"`
}

// Input is the complete remote-control surface. It cannot name a CDP method
// or send a host path, and coordinates belong to one displayed generation.
type Input struct {
	Generation uint64  `json:"generation,omitempty"`
	Kind       string  `json:"kind"`
	Tab        string  `json:"tab,omitempty"`
	URL        string  `json:"url,omitempty"`
	Text       string  `json:"text,omitempty"`
	Key        string  `json:"key,omitempty"`
	Phase      string  `json:"phase,omitempty"`
	X          float64 `json:"x,omitempty"`
	Y          float64 `json:"y,omitempty"`
	DX         float64 `json:"dx,omitempty"`
	DY         float64 `json:"dy,omitempty"`
	Button     string  `json:"button,omitempty"`
	Modifiers  int     `json:"modifiers,omitempty"`
}

// Check rejects unknown operations and bounds their arguments before they
// leave the service bridge. The holder also checks the active viewport.
func (i Input) Check() error {
	for _, n := range []float64{i.X, i.Y, i.DX, i.DY} {
		if math.IsNaN(n) || math.IsInf(n, 0) || math.Abs(n) > 10000 {
			return errors.New("invalid browser input coordinates")
		}
	}
	if i.Modifiers < 0 || i.Modifiers > 15 {
		return errors.New("invalid browser key modifiers")
	}
	switch i.Kind {
	case "navigate":
		return Navigation(i.URL)
	case "text":
		if len(i.Text) > MaxText || !utf8.ValidString(i.Text) || strings.ContainsRune(i.Text, 0) {
			return errors.New("invalid browser text")
		}
	case "key":
		if _, ok := Keys[i.Key]; !ok {
			return errors.New("unsupported browser key")
		}
	case "touch":
		if i.Phase != "start" && i.Phase != "move" && i.Phase != "end" && i.Phase != "cancel" {
			return errors.New("invalid touch phase")
		}
	case "mouse":
		if i.Phase != "down" && i.Phase != "up" && i.Phase != "move" {
			return errors.New("invalid mouse phase")
		}
		if i.Button != "left" && i.Button != "right" && i.Button != "middle" && i.Button != "none" {
			return errors.New("invalid mouse button")
		}
	case "wheel", "back", "forward", "reload", "new-tab":
	case "select-tab", "close-tab":
		if len(i.Tab) == 0 || len(i.Tab) > 128 {
			return errors.New("invalid browser tab")
		}
	default:
		return fmt.Errorf("unknown browser input %q", i.Kind)
	}
	return nil
}

// Keys is the closed set of non-text keys; Unicode text travels through insertText.
var Keys = map[string]int{
	"Enter":      13,
	"Backspace":  8,
	"Delete":     46,
	"ArrowLeft":  37,
	"ArrowUp":    38,
	"ArrowRight": 39,
	"ArrowDown":  40,
	"Tab":        9,
	"Escape":     27,
	"Home":       36,
	"End":        35,
	"PageUp":     33,
	"PageDown":   34,
	"a":          65,
	"c":          67,
	"v":          86,
	"x":          88,
	"z":          90,
}

// Frame carries one typed message. JPEG bytes are encoded only on local
// sockets; the service delivers them to the viewer as a binary image response.
type Frame struct {
	Type       string `json:"type"`
	Version    int    `json:"version,omitempty"`
	Profile    string `json:"profile,omitempty"`
	Size       *Size  `json:"size,omitempty"`
	Input      *Input `json:"input,omitempty"`
	Sequence   uint64 `json:"sequence,omitempty"`
	Generation uint64 `json:"generation,omitempty"`
	Data       []byte `json:"data,omitempty"`
	State      *State `json:"state,omitempty"`
	Error      string `json:"error,omitempty"`
}

// Decode reads a transport frame; its recipient validates the allowed message
// type and arguments for that stage of the connection.
func Decode(raw []byte) (Frame, error) {
	var f Frame
	err := json.Unmarshal(raw, &f)
	return f, err
}
