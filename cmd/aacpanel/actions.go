package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/auth"
	"aacpanel/internal/store"
)

func (s *Server) apiActions(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, "the log is off: the database is not configured", http.StatusServiceUnavailable)
		return
	}
	list, err := s.db.Actions(r.Context(), store.ActionsReq{
		Limit:  intParam(r, "limit", 50),
		Before: int64(intParam(r, "before", 0)),
		Result: r.URL.Query().Get("result"),
	})
	if err != nil {
		historyError(w, err)
		return
	}
	writeJSON(w, map[string]any{"actions": list})
}

func (s *Server) apiRunAction(w http.ResponseWriter, r *http.Request) {
	if s.exec == nil {
		http.Error(w, "the executor is not configured: actions are unavailable", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Kind   string         `json:"kind"`
		Target string         `json:"target"`
		Params map[string]any `json:"params"`
	}
	counted := &countingReader{from: http.MaxBytesReader(w, r.Body, uploadBodyMax)}
	if err := json.NewDecoder(counted).Decode(&body); err != nil {
		http.Error(w, "the request was not parsed", http.StatusBadRequest)
		return
	}
	if body.Kind != string(action.SessionFile) && counted.read > actionBodyMax {
		http.Error(w, "the request without a file is longer than allowed", http.StatusRequestEntityTooLarge)
		return
	}

	req := action.Request{Kind: action.Kind(body.Kind), Target: body.Target}

	var cwd string
	if req.Kind == action.SessionResume {
		sessionID, dir, err := s.resumeTarget(r, body.Target, body.Params)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		cwd = dir
		req.Resume, req.Target = sessionID, filepath.Base(strings.TrimRight(dir, "/"))
	}

	params := body.Params
	if req.Kind == action.SessionAnswer {
		answer, err := answerFromParams(body.Params)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req.Answer = answer
		params = map[string]any{"ask": answer.AskID, "picks": answer.Picks}
		if lens := textLens(answer.Texts); len(lens) > 0 {
			params["chars"] = lens
		}
		if lens := textLens(answer.Notes); len(lens) > 0 {
			params["noteChars"] = lens
		}
	}
	if req.Kind == action.SessionDismiss {
		answer, err := dismissFromParams(body.Params)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req.Answer = answer
		params = map[string]any{"ask": answer.AskID}
	}
	if req.Kind == action.SessionPermit {
		permit, err := permitFromParams(body.Params)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req.Permit = permit
		params = map[string]any{"option": permit.Option, "dialog": permit.Fingerprint}
	}
	if req.Kind == action.TaskStop || req.Kind == action.AgentStop {
		work, err := workFromParams(body.Params)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		req.Work = work
		params = map[string]any{"work": work.ID}
		if work.Line != "" {
			params["line"] = work.Line
		}
	}
	if req.Kind == action.SessionSend {
		text, _ := body.Params["text"].(string)
		req.Text = text
		params = map[string]any{"chars": len([]rune(text))}
		if id, _ := body.Params["messageId"].(string); id != "" {
			req.MessageID = id
			params["message"] = id
		}
	}
	if req.Kind == action.SessionUnqueue {
		id, _ := body.Params["messageId"].(string)
		req.MessageID = id
		params = map[string]any{"message": id}
	}
	if req.Kind == action.SessionFile {
		files, err := filesFromParams(body.Params)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		caption, _ := body.Params["text"].(string)
		req.Files, req.Text = files, caption
		names := make([]string, 0, len(files))
		bytes := 0
		for _, f := range files {
			names = append(names, f.Name)
			bytes += len(f.Data)
		}
		params = map[string]any{"files": names, "bytes": bytes, "chars": len([]rune(caption))}
	}
	if req.Kind == action.SessionCommand {
		name, _ := body.Params["command"].(string)
		arg, _ := body.Params["arg"].(string)
		req.Command = &action.Command{Name: name, Arg: arg}
		params = map[string]any{"command": name}
		if arg != "" {
			params["arg"] = arg
		}
	}
	if req.Kind == action.SessionSet {
		set := &action.Setting{}
		set.Model, _ = body.Params["model"].(string)
		set.Effort, _ = body.Params["effort"].(string)
		set.Mode, _ = body.Params["mode"].(string)
		set.Scope, _ = body.Params["scope"].(string)
		req.Setting = set
		params = map[string]any{}
		for key, v := range map[string]string{"model": set.Model, "effort": set.Effort, "mode": set.Mode, "scope": set.Scope} {
			if v != "" {
				params[key] = v
			}
		}
	}
	if req.Kind == action.SessionRename {
		req.Rename, _ = body.Params["name"].(string)
		params = map[string]any{"name": req.Rename}
	}
	if req.Kind == action.SessionRemote {
		if on, ok := body.Params["on"].(bool); ok {
			req.Remote = &on
			params = map[string]any{"on": on}
		}
	}
	if req.Kind == action.SessionMcp {
		change := &action.McpChange{}
		change.Server, _ = body.Params["server"].(string)
		change.Do, _ = body.Params["do"].(string)
		req.Mcp = change
		params = map[string]any{"server": change.Server, "do": change.Do}
	}
	if req.Kind == action.SessionOpen || req.Kind == action.SessionResume {
		want, projectID, err := s.launchProject(r.Context(), body.Params, cwd, req.Target)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if want != nil {
			req.Project = want
			params = map[string]any{"project": projectID, "path": want.Path}
		}
	}

	if req.Kind == action.SessionSwitch {
		to, _ := body.Params["to"].(string)
		force, _ := body.Params["force"].(bool)
		window, _ := body.Params["window"].(bool)
		plan, err := s.switchPlan(r.Context(), body.Target)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if to != plan.To {
			http.Error(w, fmt.Sprintf("session %s cannot move to %q: %s", body.Target, to, plan.why()), http.StatusBadRequest)
			return
		}
		req.Switch, req.Project = &action.Switch{To: to, Force: force, Window: window}, plan.Project
		params = map[string]any{"to": to, "project": plan.ProjectID, "path": plan.Project.Path}
		if force {
			params["force"] = true
		}
		if window {
			params["window"] = true
		}
	}

	form := req
	form.ID = "form"
	if err := form.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	deviceID := s.auth.CurrentDevice(r)
	deviceName := s.passkey.DeviceName(r.Context(), deviceID)
	if deviceName == "" {
		deviceName = "unknown device"
	}

	var journalID int64
	var logged bool
	if s.db != nil {
		id, err := s.db.LogAttempt(r.Context(), store.Action{
			DeviceID:   deviceRef(deviceID),
			DeviceName: deviceName,
			Kind:       body.Kind,
			Target:     body.Target,
			Params:     params,
		})
		if err != nil {
			log.Printf("action log: %v", err)
		} else {
			journalID, logged = id, true
		}
	}

	req.ID = requestID(journalID)
	req.Device = deviceName

	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), execTimeout)
	defer cancel()

	resp, err := s.exec.Do(ctx, req)
	outcome := store.ActionOutcome{
		Result:   store.ActionOK,
		Detail:   resp.Detail,
		Duration: time.Duration(resp.DurationMs) * time.Millisecond,
	}
	switch {
	case errors.Is(err, action.ErrUnavailable):
		outcome.Result, outcome.Error = store.ActionRejected, "executor is unavailable"
	case err != nil:
		outcome.Result, outcome.Error = store.ActionFailed, err.Error()
	case !resp.OK:
		outcome.Result, outcome.Error = store.ActionFailed, resp.Error
	}

	if outcome.Result == store.ActionOK && s.watch != nil {
		s.watch.Expect(req.Kind, req.Target)
	}

	if logged {
		if err := s.db.LogResult(ctx, journalID, outcome); err != nil {
			log.Printf("action log, outcome: %v", err)
		}
	}

	log.Printf("%s: %s %s — %s%s", auth.ClientIP(r), body.Kind, body.Target, outcome.Result,
		note(outcome.Error))

	if outcome.Result != store.ActionOK {
		status := http.StatusBadGateway
		if outcome.Result == store.ActionRejected {
			status = http.StatusServiceUnavailable
		}
		writeStatusJSON(w, status, map[string]any{"error": outcome.Error, "logged": logged})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "detail": resp.Detail, "logged": logged})
}

func (s *Server) apiSessionPermission(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "it is not said whose session to look at", http.StatusBadRequest)
		return
	}
	if s.exec == nil {
		writeJSON(w, map[string]any{"state": "unknown", "reason": "the executor is not configured"})
		return
	}
	perm, err := s.exec.Permission(r.Context(), name)
	if err != nil {
		writeJSON(w, map[string]any{"state": "unknown", "reason": err.Error()})
		return
	}
	if perm == nil {
		writeJSON(w, map[string]any{"state": "none"})
		return
	}
	writeJSON(w, map[string]any{"state": "ok", "permission": perm})
}

func (s *Server) apiSessionWindow(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "it is not said whose session window to look at", http.StatusBadRequest)
		return
	}
	if s.exec == nil {
		writeJSON(w, map[string]any{"state": "unknown", "reason": "the executor is not configured"})
		return
	}
	win, err := s.exec.Window(r.Context(), name)
	if err != nil {
		writeJSON(w, map[string]any{"state": "unknown", "reason": err.Error()})
		return
	}
	if win == nil {
		writeJSON(w, map[string]any{"state": "unknown", "reason": "the executor did not answer the question about the window"})
		return
	}
	if win.Open {
		writeJSON(w, map[string]any{"state": "open"})
		return
	}
	writeJSON(w, map[string]any{"state": "none"})
}

// apiSessionModels says what a live session can be switched to: the models
// its claude lists and the mode and effort it runs with, beside the catalogue
// of the account — the models a terminal takes by id, and the older ones a
// list of aliases leaves out.
func (s *Server) apiSessionModels(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "it is not said whose models to list", http.StatusBadRequest)
		return
	}
	out := map[string]any{"catalog": s.modelCatalog()}
	if s.exec == nil {
		out["state"], out["reason"] = "unknown", "the executor is not configured"
		writeJSON(w, out)
		return
	}
	models, err := s.exec.Models(r.Context(), name)
	if err != nil {
		out["state"], out["reason"] = "unknown", err.Error()
		writeJSON(w, out)
		return
	}
	if models == nil {
		out["state"], out["reason"] = "unknown", "the executor did not answer the question about the models"
		writeJSON(w, out)
		return
	}
	out["state"], out["session"] = "ok", models
	writeJSON(w, out)
}

// apiSessionMcp says what a live session knows about its MCP servers.
func (s *Server) apiSessionMcp(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "it is not said whose MCP servers to list", http.StatusBadRequest)
		return
	}
	if s.exec == nil {
		writeJSON(w, map[string]any{"state": "unknown", "reason": "the executor is not configured"})
		return
	}
	mcp, err := s.exec.Mcp(r.Context(), name)
	if err != nil {
		writeJSON(w, map[string]any{"state": "unknown", "reason": err.Error()})
		return
	}
	if mcp == nil {
		writeJSON(w, map[string]any{"state": "unknown", "reason": "the executor did not answer the question about MCP"})
		return
	}
	// The list crosses the socket with omitempty and arrives as nothing when it
	// is empty; the screen is handed a list either way.
	servers := mcp.Servers
	if servers == nil {
		servers = []action.McpServer{}
	}
	writeJSON(w, map[string]any{"state": "ok", "transport": mcp.Transport, "servers": servers})
}

// apiSessionStatus says what a live session says about itself.
func (s *Server) apiSessionStatus(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "it is not said which session to ask", http.StatusBadRequest)
		return
	}
	if s.exec == nil {
		writeJSON(w, map[string]any{"state": "unknown", "reason": "the executor is not configured"})
		return
	}
	status, err := s.exec.Status(r.Context(), name)
	if err != nil {
		writeJSON(w, map[string]any{"state": "unknown", "reason": err.Error()})
		return
	}
	if status == nil {
		writeJSON(w, map[string]any{"state": "unknown", "reason": "the executor did not answer the question about the session"})
		return
	}
	writeJSON(w, map[string]any{"state": "ok", "transport": status.Transport, "version": status.Version,
		"account": status.Account})
}

// apiSessionSetup says what a live session is set up with, one read-only
// screen at a time. The part asked for goes out as a list or an object even
// when it is empty: the socket drops an empty one, and the screen reads it
// without guarding against its absence.
func (s *Server) apiSessionSetup(w http.ResponseWriter, r *http.Request) {
	name, part := r.URL.Query().Get("name"), r.URL.Query().Get("part")
	if name == "" {
		http.Error(w, "it is not said which session to ask", http.StatusBadRequest)
		return
	}
	if !action.SetupParts[part] {
		http.Error(w, fmt.Sprintf("there is no screen %q of what a session is set up with", part), http.StatusBadRequest)
		return
	}
	if s.exec == nil {
		writeJSON(w, map[string]any{"state": "unknown", "reason": "the executor is not configured"})
		return
	}
	setup, err := s.exec.Setup(r.Context(), name, part)
	if err != nil {
		writeJSON(w, map[string]any{"state": "unknown", "reason": err.Error()})
		return
	}
	if setup == nil {
		writeJSON(w, map[string]any{"state": "unknown", "reason": "the executor did not answer the question about the settings"})
		return
	}
	out := map[string]any{"state": "ok", "transport": setup.Transport}
	if setup.Transport == action.SwitchStream {
		out[part] = setupPart(setup, part)
	}
	writeJSON(w, out)
}

func setupPart(setup *action.Setup, part string) any {
	switch part {
	case action.SetupHooks:
		if setup.Hooks != nil {
			return setup.Hooks
		}
		return &action.Hooks{Events: []action.HookEvent{}, Hooks: []action.Hook{}}
	case action.SetupMemory:
		if setup.Memory != nil {
			return setup.Memory
		}
		return &action.Memory{Files: []action.MemoryFile{}, Folders: []action.MemoryFile{}, Memories: []action.SavedMemory{}}
	case action.SetupSkills:
		if setup.Skills != nil {
			return setup.Skills
		}
		return []action.Skill{}
	case action.SetupAgents:
		if setup.Agents != nil {
			return setup.Agents
		}
		return []action.AgentType{}
	}
	if setup.Config != nil {
		return setup.Config
	}
	return []action.ConfigEntry{}
}

// apiSessionCommands says which commands a live session takes, with what each
// does. A terminal lists none, and the composer keeps to the panel's own.
func (s *Server) apiSessionCommands(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "it is not said whose commands to list", http.StatusBadRequest)
		return
	}
	if s.exec == nil {
		writeJSON(w, map[string]any{"state": "unknown", "reason": "the executor is not configured"})
		return
	}
	commands, err := s.exec.Commands(r.Context(), name)
	if err != nil {
		writeJSON(w, map[string]any{"state": "unknown", "reason": err.Error()})
		return
	}
	if commands == nil {
		writeJSON(w, map[string]any{"state": "unknown", "reason": "the executor did not answer the question about commands"})
		return
	}
	list := commands.List
	if list == nil {
		list = []action.SlashCommand{}
	}
	writeJSON(w, map[string]any{"state": "ok", "transport": commands.Transport, "list": list})
}

// sideBodyMax bounds a question aside with its side chat: the executor holds
// them to their own limits, and this one keeps a body from being read whole
// before that.
const sideBodyMax = 4 << 20

// apiSessionSide asks a live session a question aside. The answer goes back to
// the one who asked and nowhere else: it is not in the conversation, and the
// panel does not keep it either.
func (s *Server) apiSessionSide(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string            `json:"name"`
		Question string            `json:"question"`
		History  []action.SideTurn `json:"history"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, sideBodyMax)).Decode(&body); err != nil {
		http.Error(w, "the question was not read: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Name == "" || strings.TrimSpace(body.Question) == "" {
		http.Error(w, "a question aside needs the session and the question", http.StatusBadRequest)
		return
	}
	if s.exec == nil {
		writeJSON(w, map[string]any{"state": "unknown", "reason": "the executor is not configured"})
		return
	}
	side, err := s.exec.Side(r.Context(), body.Name, body.Question, body.History)
	if err != nil {
		writeJSON(w, map[string]any{"state": "failed", "reason": err.Error()})
		return
	}
	if side == nil {
		writeJSON(w, map[string]any{"state": "failed", "reason": "the executor did not answer the question aside"})
		return
	}
	writeJSON(w, map[string]any{"state": "ok", "answer": side.Answer})
}

// apiSessionSwitch says which way a live session can move between the console
// and the feed, so the conversation header offers only the way that works.
func (s *Server) apiSessionSwitch(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "it is not said which session to move", http.StatusBadRequest)
		return
	}
	plan, err := s.switchPlan(r.Context(), name)
	if err != nil {
		writeJSON(w, map[string]any{"to": "", "reason": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"to": plan.To})
}

// switchWay is where a live session can move, and what it is started with there.
type switchWay struct {
	To        string
	Project   *action.Project
	ProjectID int
}

func (p switchWay) why() string {
	if p.To == action.SwitchConsole {
		return "it is in the feed already"
	}
	return "it is in the console already"
}

// switchPlan finds where a live session can move. A session on the stream can
// always go to the console. A console goes to the feed only when its project
// is set to live there: which projects live in the feed is decided in the map,
// not by a button in one conversation.
func (s *Server) switchPlan(ctx context.Context, name string) (switchWay, error) {
	if s.host == nil {
		return switchWay{}, errors.New("the host snapshot is not configured")
	}
	live, ok := s.host.LiveSession(name)
	if !ok {
		return switchWay{}, fmt.Errorf("there is no live session %q", name)
	}
	if live.SessionID == "" || live.CWD == "" {
		return switchWay{}, fmt.Errorf("session %q has no conversation to carry over yet", name)
	}
	want, projectID, err := s.launchProject(ctx, nil, live.CWD, "")
	if err != nil {
		return switchWay{}, err
	}
	if want == nil {
		return switchWay{}, fmt.Errorf("session %q runs in %s, which is not a project from the map: "+
			"the panel does not know how to start it again", name, live.CWD)
	}
	want.Session = name
	if live.Transport == action.SwitchStream {
		return switchWay{To: action.SwitchConsole, Project: want, ProjectID: projectID}, nil
	}
	var launch struct {
		Transport string `json:"transport"`
	}
	_ = json.Unmarshal(want.Launch, &launch)
	if launch.Transport != action.SwitchStream {
		return switchWay{}, fmt.Errorf("the project of session %q lives in the console", name)
	}
	return switchWay{To: action.SwitchStream, Project: want, ProjectID: projectID}, nil
}

func (s *Server) apiExecStatus(w http.ResponseWriter, r *http.Request) {
	if s.exec == nil {
		writeJSON(w, map[string]any{"available": false, "reason": "the executor is not configured"})
		return
	}
	if err := s.exec.Reach(r.Context()); err != nil {
		writeJSON(w, map[string]any{"available": false, "reason": err.Error()})
		return
	}

	kinds, err := s.exec.Kinds(r.Context())
	if err != nil || len(kinds) == 0 {
		writeJSON(w, map[string]any{"available": true})
		return
	}
	writeJSON(w, map[string]any{"available": true, "kinds": kinds})
}

// resumeTarget finds the conversation a resume is about and where it ran. The
// screen knows which row was pressed and says so by its identifier; the name is
// the older way in and cannot tell two conversations apart when two contours
// hold a project of the same name.
func (s *Server) resumeTarget(r *http.Request, name string, params map[string]any) (sessionID, cwd string, err error) {
	if s.db == nil {
		return "", "", errors.New("the conversation cannot be resumed: the database is not configured")
	}
	hostID, err := s.db.HostID(r.Context(), s.hostName)
	if err != nil {
		return "", "", err
	}

	if id, _ := params["session"].(string); strings.TrimSpace(id) != "" {
		id = strings.TrimSpace(id)
		cwd, err = s.db.SessionResumeAt(r.Context(), hostID, id)
		if err != nil {
			return "", "", err
		}
		if cwd == "" {
			return "", "", fmt.Errorf("conversation %q is not in the archive of this machine", id)
		}
		return id, cwd, nil
	}

	sessionID, cwd, err = s.db.SessionResume(r.Context(), hostID, name)
	if err != nil {
		return "", "", err
	}
	if sessionID == "" {
		return "", "", fmt.Errorf("there is no conversation of session %q to resume", name)
	}
	if cwd == "" {
		return "", "", fmt.Errorf("the directory of session %q is not known — it cannot be resumed", name)
	}
	return sessionID, cwd, nil
}
