package action

import "encoding/json"

// What a session is set up with, one read-only screen at a time.
const (
	SetupHooks  = "hooks"
	SetupMemory = "memory"
	SetupSkills = "skills"
	SetupAgents = "agents"
	SetupConfig = "config"
)

// SetupParts are the screens a session is asked for.
var SetupParts = map[string]bool{
	SetupHooks: true, SetupMemory: true, SetupSkills: true, SetupAgents: true, SetupConfig: true,
}

// Setup is what a session is set up with, as one of its read-only screens
// shows it. A session on the stream answers claude's own requests for the rows
// of those screens; a terminal draws them itself on screens driven by keys,
// and the answer only says where it lives. Only the part asked for is filled.
type Setup struct {
	Transport string        `json:"transport"`
	Hooks     *Hooks        `json:"hooks,omitempty"`
	Memory    *Memory       `json:"memory,omitempty"`
	Skills    []Skill       `json:"skills,omitempty"`
	Agents    []AgentType   `json:"agents,omitempty"`
	Config    []ConfigEntry `json:"config,omitempty"`
}

// Hooks are the hooks a session runs, by event, as the /hooks menu lists
// them, and why none of them may run.
type Hooks struct {
	Events []HookEvent `json:"events"`
	Hooks  []Hook      `json:"hooks"`
	// Blocked says why hooks do not run, when a policy stops them.
	Blocked string `json:"blocked,omitempty"`
}

// HookEvent is an event that has hooks.
type HookEvent struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Count   int    `json:"count"`
}

// Hook is one hook as the menu shows it.
type Hook struct {
	Event     string `json:"event"`
	Matcher   string `json:"matcher,omitempty"`
	Source    string `json:"source"`
	Plugin    string `json:"plugin,omitempty"`
	Type      string `json:"type"`
	Label     string `json:"label"`
	Text      string `json:"text"`
	TextLabel string `json:"textLabel,omitempty"`
	Condition string `json:"condition,omitempty"`
	Timeout   int    `json:"timeout,omitempty"`
	Disabled  bool   `json:"disabled,omitempty"`
}

// Memory is what the /memory dialog shows: the instruction files a session
// reads, the folders it keeps memories in, and the memories saved there.
type Memory struct {
	Auto     string        `json:"auto,omitempty"`
	Files    []MemoryFile  `json:"files"`
	Folders  []MemoryFile  `json:"folders"`
	Memories []SavedMemory `json:"memories"`
}

// MemoryFile is an instruction file or a memory folder.
type MemoryFile struct {
	Kind        string `json:"kind"`
	Path        string `json:"path"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Missing     bool   `json:"missing,omitempty"`
}

// SavedMemory is one memory in the auto-memory folder.
type SavedMemory struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Type        string `json:"type,omitempty"`
	Modified    int64  `json:"modified"`
}

// Skill is a row of the /skills menu.
type Skill struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source"`
	Tokens      int    `json:"tokens"`
	State       string `json:"state"`
	LockedBy    string `json:"lockedBy,omitempty"`
}

// AgentType is a kind of subagent the session can start.
type AgentType struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Model       string `json:"model,omitempty"`
}

// ConfigEntry is one merged setting with its value as claude holds it.
type ConfigEntry struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}
