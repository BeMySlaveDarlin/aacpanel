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
