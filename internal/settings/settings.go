// Package settings collects the panel settings into one list, with their source and the price of a change.
package settings

import (
	"os"
	"sort"

	"aacpanel/internal/hostcfg"
)

// Cost is what has to happen for a change to take effect.
type Cost string

const (
	// CostLive means the change takes effect at once.
	CostLive Cost = "live"
	// CostSession means the change takes effect from the next session started.
	CostSession Cost = "session"
	// CostExec means the change takes effect after the executor unit restarts.
	CostExec Cost = "exec"
	// CostAgent means the change takes effect after the collector unit restarts.
	CostAgent Cost = "agent"
	// CostService means the change takes effect after the panel container restarts.
	CostService Cost = "service"
	// CostRecreate means the change takes effect only when the container is recreated.
	CostRecreate Cost = "recreate"
	// CostNever means the value does not change by editing.
	CostNever Cost = "never"
)

// Source is where the effective value came from.
type Source string

const (
	SourceEnv     Source = "env"
	SourceFile    Source = "file"
	SourceDefault Source = "default"
)

// Item is one setting in the list.
type Item struct {
	Key    string `json:"key"`
	Value  string `json:"value,omitempty"`
	Secret bool   `json:"secret,omitempty"`
	Set    bool   `json:"set"`
	Source Source `json:"source"`
	Cost   Cost   `json:"cost"`
	Group  string `json:"group"`
	Note   string `json:"note,omitempty"`

	FileValue string `json:"fileValue,omitempty"`
}

type spec struct {
	group  string
	cost   Cost
	secret bool
	note   string
}

const (
	groupMachine = "machine"
	groupAccess  = "access"
	groupSession = "session"
	groupStore   = "store"
	groupSecrets = "secrets"
)

var specs = map[string]spec{
	hostcfg.HostEnv:         {groupMachine, CostService, false, "the machine name in the header and in the database"},
	hostcfg.RepoEnv:         {groupMachine, CostNever, false, "set by the install: the panel does not move the tree"},
	hostcfg.UnixUserEnv:     {groupMachine, CostNever, false, "set by the install: the panel does not reinstall units"},
	hostcfg.HomeSessionEnv:  {groupMachine, CostExec, false, "the name of the home session"},
	hostcfg.LangEnv:         {groupMachine, CostExec, false, "the locale of sessions; loses to the executor's own LANG when that one is set"},
	hostcfg.StateDirEnv:     {groupMachine, CostRecreate, false, "the state directory; it is also mounted into the container"},
	"DISPLAY":               {groupMachine, CostExec, false, "the display the session window opens on"},
	"AACP_TERMINAL":         {groupMachine, CostExec, false, "what opens the window; read past the machine description, so the file is no fallback"},
	"AACP_TERMINAL_AUTO":    {groupMachine, CostExec, false, "open the window with the session or only on the button"},
	"AACP_CLAUDE":           {groupMachine, CostSession, false, "what runs claude; the profile map wins over this"},
	"AACP_CLAUDE_HOME":      {groupMachine, CostExec, false, "the directories of claude contours"},
	"AACP_CLAUDE_REGISTRY":  {groupMachine, CostExec, false, "the registry of contours"},
	hostcfg.ProjectRootsEnv: {groupMachine, CostService, false, "where sessions may be opened; the service and the executor read this apart"},
	"AACP_PROJECT_SCAN":     {groupMachine, CostAgent, false, "what to walk when looking for projects"},
	"AACP_PROBE_PORTS":      {groupMachine, CostAgent, false, "the port checks the collector runs"},

	"AACP_RP_ID":      {groupAccess, CostRecreate, false, "the passkey domain; changing it voids every key already enrolled"},
	"AACP_RP_ORIGINS": {groupAccess, CostRecreate, false, "the origins a sign-in is accepted from"},
	"AACP_RP_NAME":    {groupAccess, CostRecreate, false, "the panel name in the passkey dialog"},
	"AACP_BIND":       {groupAccess, CostRecreate, false, "the address of the main listener"},
	"AACP_SECURE":     {groupAccess, CostRecreate, false, "cleared, the cookie travels over plain http"},
	"AACP_LAN_BIND":   {groupAccess, CostRecreate, false, "the address of the local network listener"},
	"AACP_LAN_PORT":   {groupAccess, CostRecreate, false, "the port of the local network listener"},
	"AACP_LAN_URL":    {groupAccess, CostRecreate, false, "the local network address in the client map"},
	"AACP_PUBLIC_URL": {groupAccess, CostRecreate, false, "the domain address in the client map"},
	"AACP_TS_URL":     {groupAccess, CostRecreate, false, "the tailnet node address in the client map"},
	"AACP_TAILSCALE":  {groupAccess, CostRecreate, false, "whether the tailnet node is raised"},
	"AACP_TS_HOSTNAME": {groupAccess, CostRecreate, false,
		"the node name in the tailnet; the passkey domain is built from it"},
	"AACP_TERM_PUBLIC": {groupAccess, CostRecreate, false,
		"the session terminal for whoever signs in from outside: input bypasses the confirmation gate"},

	"AACP_SESSION_IDLE": {groupSession, CostRecreate, false, "how long an idle session is kept"},
	"AACP_SESSION_MAX":  {groupSession, CostRecreate, false, "the hard lifetime of a session"},
	"AACP_EXEC_DIR":     {groupSession, CostRecreate, false, "the executor socket directory; it is also mounted into the container"},
	"AACP_UID":          {groupSession, CostRecreate, false, "who the service runs as; must match the socket owner"},
	"AACP_GID":          {groupSession, CostRecreate, false, "the group the service runs as"},

	"AACP_DB_NAME":  {groupStore, CostRecreate, false, "the database name"},
	"AACP_DB_USER":  {groupStore, CostRecreate, false, "the database owner"},
	"AACP_APP_ROLE": {groupStore, CostRecreate, false, "the role the service works under in production"},

	"AACP_SECRET":       {groupSecrets, CostRecreate, true, "the cookie signature; changing it signs out every device"},
	"AACP_TOKEN":        {groupSecrets, CostRecreate, true, "the second door next to passkey; empty means there is no such door"},
	"AACP_DB_PASSWORD":  {groupSecrets, CostRecreate, true, "the database owner password; applied only when the volume is created"},
	"AACP_APP_PASSWORD": {groupSecrets, CostRecreate, true, "the password of the application role"},
	"TS_AUTHKEY":        {groupSecrets, CostRecreate, true, "the one-time tailnet key; it burns on the first sign-in"},
}

// Collect builds the list of settings from the process environment and the machine description.
func Collect(hostEnvPath string) []Item {
	file := map[string]string{}
	if hostEnvPath == "" {
		hostEnvPath = hostcfg.Path()
	}
	file = hostcfg.ParseFile(hostEnvPath)

	out := make([]Item, 0, len(specs))
	for key, s := range specs {
		env, inEnv := os.LookupEnv(key)
		fromFile, inFile := file[key]

		item := Item{Key: key, Secret: s.secret, Group: s.group, Cost: s.cost, Note: s.note}
		switch {
		case inEnv && env != "":
			item.Value, item.Source, item.Set = env, SourceEnv, true
			if inFile && fromFile != env {
				item.FileValue = fromFile
			}
		case inFile && fromFile != "":
			item.Value, item.Source, item.Set = fromFile, SourceFile, true
		default:
			item.Source, item.Set = SourceDefault, false
		}
		if s.secret {
			item.Value, item.FileValue = "", ""
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Key < out[j].Key
	})
	return out
}
