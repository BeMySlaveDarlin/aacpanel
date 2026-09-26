// Package schema describes the launch parameters of the profile map as data:
// what each one is, where it may be stored, what its options mean, what an
// absent value leaves to and when a live session takes a change. The service
// validates the map and draws its screens by it; the launcher reads exactly
// the keys it lists, and a test holds the two lists equal.
package schema

import "slices"

// Kind is how a value is typed, and so how a screen edits it.
type Kind string

const (
	KindEnum   Kind = "enum"
	KindModel  Kind = "model"
	KindBool   Kind = "bool"
	KindInt    Kind = "int"
	KindText   Kind = "text"
	KindKV     Kind = "kv"
	KindTokens Kind = "tokens"
)

// Level is where a value may be stored.
type Level string

const (
	LevelContour Level = "contour"
	LevelProject Level = "project"
)

// Merge is how a project's value lies over its contour's.
type Merge string

const (
	// MergeOverride: the project's value replaces the contour's.
	MergeOverride Merge = "override"
	// MergeByKey: the project's keys replace the contour's one by one.
	MergeByKey Merge = "merge_by_key"
)

// Live is when a change reaches a session that is already running.
type Live string

const (
	// LiveNow: the session takes it by an action, without a restart.
	LiveNow Live = "now"
	// LiveOnMove: a move between the console and the feed restarts the
	// process with the project's parameters, and the change goes with it.
	LiveOnMove Live = "on move"
	// LiveNextStart: only a new process takes it.
	LiveNextStart Live = "next start"
)

// Transports a live mark is given for.
const (
	TransportTmux   = "tmux"
	TransportStream = "stream"
)

// Option is one value an enum takes, with what choosing it means.
type Option struct {
	Value   string `json:"value"`
	Label   string `json:"label"`
	Meaning string `json:"meaning,omitempty"`
}

// Param is one launch parameter.
type Param struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Help  string `json:"help,omitempty"`
	Kind  Kind   `json:"kind"`
	// Levels are where a value may be stored.
	Levels []Level `json:"levels"`
	// Options are the values offered, in order. Held are accepted where they
	// are stored but never offered: they are chosen on the host or not at all.
	Options []Option `json:"options,omitempty"`
	Held    []string `json:"held,omitempty"`
	// Unset is what an absent value leaves to — the outcome, never "not set".
	Unset string `json:"unset"`
	Merge Merge  `json:"merge"`
	// Live says per transport when a change reaches a running session.
	Live map[string]Live `json:"live"`

	Unit   string `json:"unit,omitempty"`
	Min    int    `json:"min,omitempty"`
	Max    int    `json:"max,omitempty"`
	MaxLen int    `json:"maxLen,omitempty"`
	// Reserved are env keys a map may not set, each with the reason.
	Reserved map[string]string `json:"reserved,omitempty"`
	// Forbidden are arguments a map may not add, each with the reason.
	Forbidden map[string]string `json:"forbidden,omitempty"`
}

// Retired is a key that means nothing any more.
type Retired struct {
	Key string `json:"key"`
	Why string `json:"why"`
}

const accountRoute = "moves the session into another account — change the contour instead"

const ownFlag = "the panel sets it from its own parameter"

var params = []Param{
	{
		Key: "transport", Label: "Where it lives", Kind: KindEnum,
		Levels: []Level{LevelContour, LevelProject},
		Options: []Option{
			{Value: TransportTmux, Label: "Console",
				Meaning: "a terminal in tmux on the host; the panel types keys and reads the screen"},
			{Value: TransportStream, Label: "Feed",
				Meaning: "claude -p under a holder; the panel answers with structure; no window on the host"},
		},
		Unset: "Console", Merge: MergeOverride,
		Live: map[string]Live{TransportTmux: LiveOnMove, TransportStream: LiveOnMove},
	},
	{
		Key: "model", Label: "Model", Kind: KindModel,
		Levels: []Level{LevelContour, LevelProject},
		Unset:  "the account's model", Merge: MergeOverride,
		Live: map[string]Live{TransportTmux: LiveNow, TransportStream: LiveNow},
	},
	{
		Key: "effort", Label: "Effort", Kind: KindEnum,
		Levels: []Level{LevelContour, LevelProject},
		Help:   "ultracode is not among them: claude does not take it at launch, a live session takes it from the composer",
		Options: []Option{
			{Value: "low", Label: "Low"},
			{Value: "medium", Label: "Medium"},
			{Value: "high", Label: "High"},
			{Value: "xhigh", Label: "Extra"},
			{Value: "max", Label: "Max"},
		},
		Unset: "the account's effort", Merge: MergeOverride,
		Live: map[string]Live{TransportTmux: LiveNow, TransportStream: LiveNow},
	},
	{
		Key: "permissionMode", Label: "Permissions", Kind: KindEnum,
		Levels: []Level{LevelContour, LevelProject},
		Options: []Option{
			{Value: "default", Label: "Manual", Meaning: "asks before every change"},
			{Value: "acceptEdits", Label: "Accept edits", Meaning: "file edits go through, the rest asks"},
			{Value: "plan", Label: "Plan", Meaning: "plans first, changes nothing"},
			{Value: "auto", Label: "Auto", Meaning: "claude decides what to ask"},
		},
		Held:  []string{"bypassPermissions", "dontAsk"},
		Unset: "the account's mode", Merge: MergeOverride,
		Live: map[string]Live{TransportTmux: LiveNextStart, TransportStream: LiveNow},
	},
	{
		Key: "remoteControl", Label: "Remote Control", Kind: KindBool,
		Levels: []Level{LevelContour, LevelProject},
		Help:   "the session is reachable from the Claude app and claude.ai",
		Unset:  "Off", Merge: MergeOverride,
		Live: map[string]Live{TransportTmux: LiveNow, TransportStream: LiveNow},
	},
	{
		Key: "intent", Label: "First message", Kind: KindText,
		Levels: []Level{LevelContour, LevelProject},
		Help:   "sent right after the start — to a new session and to a resumed one alike; empty cancels the contour's",
		Unset:  "the conversation opens empty", Merge: MergeOverride, MaxLen: 500,
		Live: map[string]Live{TransportTmux: LiveNextStart, TransportStream: LiveNextStart},
	},
	{
		Key: "finalizeAt", Label: "Wrap up at", Kind: KindInt, Unit: "%", Min: 1, Max: 99,
		Levels: []Level{LevelContour, LevelProject},
		Help:   "past this share of the context the session finalizes and restarts itself; needs the context guard hook",
		Unset:  "Never", Merge: MergeOverride,
		Live: map[string]Live{TransportTmux: LiveOnMove, TransportStream: LiveOnMove},
	},
	{
		Key: "env", Label: "Environment", Kind: KindKV,
		Levels: []Level{LevelContour, LevelProject},
		Unset:  "the host's own environment", Merge: MergeByKey,
		Reserved: map[string]string{
			"CLAUDE_CONFIG_DIR":       accountRoute,
			"CLAUDE_CODE_OAUTH_TOKEN": accountRoute,
			"CLAUDE_PROFILE":          "the router's own variable",
			"AACP_FINALIZE_AT":        "set by Wrap up at",
		},
		Live: map[string]Live{TransportTmux: LiveOnMove, TransportStream: LiveOnMove},
	},
	{
		Key: "args", Label: "Extra arguments", Kind: KindTokens,
		Levels: []Level{LevelContour, LevelProject},
		Help:   "added to the end of the claude command, a word each — the project's list replaces the contour's",
		Unset:  "none", Merge: MergeOverride,
		Forbidden: map[string]string{
			"--model":                  ownFlag,
			"--effort":                 ownFlag,
			"--permission-mode":        ownFlag,
			"--remote-control":         ownFlag,
			"-n":                       ownFlag,
			"--name":                   ownFlag,
			"--resume":                 "the panel resumes a conversation itself",
			"--session-id":             "the panel names the conversation itself",
			"-p":                       "the panel decides where the session lives",
			"--print":                  "the panel decides where the session lives",
			"--input-format":           "the stream sets it",
			"--output-format":          "the stream sets it",
			"--permission-prompt-tool": "the stream sets it",
		},
		Live: map[string]Live{TransportTmux: LiveOnMove, TransportStream: LiveOnMove},
	},
}

var retired = []Retired{
	{Key: "room", Why: "sessions no longer go to rooms; the key reaches nothing"},
}

// Params returns the launch parameters in the order a page shows them.
func Params() []Param {
	return params
}

// RetiredKeys returns the keys that mean nothing any more.
func RetiredKeys() []Retired {
	return retired
}

// Find returns the parameter of a key.
func Find(key string) (Param, bool) {
	for _, p := range params {
		if p.Key == key {
			return p, true
		}
	}
	return Param{}, false
}

// Retire says why a key means nothing any more, or false for a live one.
func Retire(key string) (string, bool) {
	for _, r := range retired {
		if r.Key == key {
			return r.Why, true
		}
	}
	return "", false
}

// Offers says whether a value is among the ones offered or held.
func (p Param) Offers(value string) bool {
	return slices.ContainsFunc(p.Options, func(o Option) bool { return o.Value == value }) ||
		slices.Contains(p.Held, value)
}

// At says whether the parameter may be stored at a level.
func (p Param) At(level Level) bool {
	return slices.Contains(p.Levels, level)
}
