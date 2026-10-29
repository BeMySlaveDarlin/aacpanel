package executor

import (
	"context"
	"fmt"

	"aacpanel/internal/action"
	"aacpanel/internal/codex"
	"aacpanel/internal/launcher"
	"aacpanel/internal/role"
	"aacpanel/internal/sessionintent"
	"aacpanel/internal/stream"
	"aacpanel/internal/toolset"
)

func (e *Executor) codexRestart(ctx context.Context, th codex.Thread, req action.Request) (string, error) {
	if req.Resume != "" {
		return "", fmt.Errorf("a Codex restart starts a fresh thread and takes no resume")
	}
	if !e.restarting.take(th.Name) {
		return "", fmt.Errorf("session %s is being restarted already", th.Name)
	}
	defer e.restarting.give(th.Name)
	if th.Link.Closing(th.ID) {
		return "", fmt.Errorf("session %s has a replacement already and closes after this turn", th.Name)
	}
	terminals, err := codexTerminals(ctx, []string{th.ID})
	if err != nil {
		return "", fmt.Errorf("the terminal of session %s was not checked: %w", th.Name, err)
	}
	if terminals[th.ID] != "" {
		return "", fmt.Errorf("session %s runs in tmux: restart from the Codex terminal", th.Name)
	}
	if req.Project == nil {
		return "", fmt.Errorf("session %s has no project on the map to restart from", th.Name)
	}
	begin, err := th.Link.RestartSettings(th.ID)
	if err != nil {
		return "", err
	}
	c, warns := launcher.ParseCodex(req.Project.Launch)
	if begin.Model == "" {
		begin.Model = c.Model
	}
	if begin.Effort == "" {
		begin.Effort = c.Effort
	}
	if begin.Sandbox == "" {
		begin.Sandbox = c.Sandbox
	}
	begin.Owner = stream.OwnerOf(th.ID)
	begin.Approval = c.Approval
	if begin.Owner.Conversation != "" {
		begin.Approval = "never"
		if begin.Sandbox == "" {
			begin.Sandbox = "workspace-write"
		}
	}
	begin.Workspace = begin.Sandbox == "workspace-write" && (begin.Owner.Conversation != "" || req.Project.Codex != nil)
	if req.Project.Codex != nil {
		if err := req.Project.Codex.Validate(); err != nil {
			return "", err
		}
		begin.WritableRoots = req.Project.Codex.WritableRoots
	}
	warns = append(warns, "the link does not retain writable roots; the replacement uses the launch's roots or the Codex home's defaults")
	begin.Message = c.Intent
	if req.Text != "" {
		begin.Message = req.Text
	} else if !req.Clean {
		values, err := sessionintent.Read(sessionintent.Dir(), codexPlace(th), th.Name)
		if err != nil {
			return "", fmt.Errorf("the messages of session %s were not read: %w", th.Name, err)
		}
		begin.Message = values.Pick(sessionintent.Restart, c.Intent)
	}
	if req.Clean {
		begin.Message = ""
	}
	// Naming materializes a thread with no first message, so it can be
	// resumed and receive follow-ups immediately after this action returns.
	// The replacement goes by the name the panel gave the old thread; one
	// without such a name is named after the old session.
	if begin.Name == "" {
		begin.Name = th.Name
	}
	if path, err := toolServer(); err == nil {
		begin.Tools, begin.Allowed = path, toolset.CodexAllowed()
	} else {
		warns = append(warns, "the replacement starts without the panel's tools: "+err.Error())
	}
	id, err := th.Link.Start(ctx, begin)
	if err != nil {
		if id != "" {
			if _, closeErr := th.Link.Close(ctx, id); closeErr != nil {
				return "", fmt.Errorf("session %s stays open: %w; incomplete replacement %s could not be closed: %v",
					th.Name, err, codex.SessionName(id), closeErr)
			}
		}
		return "", fmt.Errorf("session %s stays open: its replacement did not start: %w", th.Name, err)
	}
	name := codex.SessionName(id)
	if err := role.Move(role.Dir(), codexPlace(th), th.Name, name); err != nil {
		warns = append(warns, "the role stayed under the old name: "+err.Error())
	}
	if err := sessionintent.Move(sessionintent.Dir(), codexPlace(th), th.Name, name); err != nil {
		warns = append(warns, "the messages stayed under the old name: "+err.Error())
	}
	for _, child := range e.codex.Children(th.Name) {
		if err := child.Link.SetParent(child.ID, name); err != nil {
			warns = append(warns, fmt.Sprintf("child %s stays under %s: %v", child.Name, th.Name, err))
		}
	}
	warns = append(warns, e.moveClaudeChildren(th, name)...)
	if err := stream.PassOwners(th.ID, stream.SessionOwner{Name: name, Conversation: id}); err != nil {
		warns = append(warns, "the children's opening records were not all transferred: "+err.Error())
	}
	if err := th.Link.CloseAfterTurn(th.ID); err != nil {
		return "", fmt.Errorf("replacement %s started, but session %s could not be scheduled to close: %w", name, th.Name, err)
	}
	detail := fmt.Sprintf("session %s replaced by %s in %s; the old thread closes after this turn", th.Name, name, begin.CWD)
	for _, w := range warns {
		detail += "; WARNING: " + w
	}
	return detail, nil
}

func (e *Executor) moveClaudeChildren(from codex.Thread, to string) []string {
	sessions, err := allLiveSessions()
	if err != nil {
		return []string{"Claude children were not read: " + err.Error()}
	}
	var warns []string
	for _, s := range sessions {
		if !claudeChildOf(s.PID, from) {
			continue
		}
		if _, err := e.sessionParent(s.Name, to); err != nil {
			warns = append(warns, fmt.Sprintf("Claude child %s stays under %s: %v", s.Name, from.Name, err))
		}
	}
	return warns
}

// claudeChildOf says whether a live claude session stands under a thread. The
// sandbox of the executor does not let it read the environment the session
// was given its parent in, so a child is the session the thread opened, by
// the opening record of its process, unless it was stood elsewhere since; or
// one stood under the thread later.
func claudeChildOf(pid int, th codex.Thread) bool {
	key, err := processOwnerKey(pid)
	if err != nil {
		return false
	}
	opened := ""
	if stream.OwnerOf(key).Conversation == th.ID {
		opened = th.Name
	}
	return keptParent(pid, opened) == th.Name
}
