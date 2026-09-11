package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"aacpanel/internal/chat"
	"aacpanel/internal/store"
)

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

var subagentRE = regexp.MustCompile(`^a[A-Za-z0-9-]{1,64}$`)

type chatTarget = chat.Target

func (s *Server) chatTarget(r *http.Request, name string) (chatTarget, error) {
	if name == "" {
		return chatTarget{}, errors.New("a session name is required")
	}
	if id := r.URL.Query().Get("id"); id != "" {
		sub := ""
		if at := strings.IndexByte(id, ':'); at >= 0 {
			id, sub = id[:at], id[at+1:]
			if !subagentRE.MatchString(sub) {
				return chatTarget{}, errors.New("the agent id does not look like an id")
			}
		}
		if !uuidRE.MatchString(id) {
			return chatTarget{}, errors.New("the conversation id does not look like an id")
		}
		return chatTarget{Session: id, Subagent: sub}, nil
	}
	if live, ok := s.host.LiveSession(name); ok {
		if live.SessionID != "" {
			return chatTarget{Session: live.SessionID}, nil
		}
		return chatTarget{}, fmt.Errorf("session %q has not said a word yet — it has no conversation so far", name)
	}
	return chatTarget{}, fmt.Errorf("there is no session %q right now — its conversation opens from the archive, by id", name)
}

func chatWindow(r *http.Request, target chatTarget) chat.Req {
	req := chat.Req{Session: target.Session, Subagent: target.Subagent,
		Limit: intParam(r, "limit", 40)}
	if v := r.URL.Query().Get("before"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			req.Before = &n
		}
	}
	if v := r.URL.Query().Get("after"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			req.After = &n
		}
	}
	req.State = req.Before == nil && req.Subagent == ""
	return req
}

func chatFail(w http.ResponseWriter, err error) {
	if errors.Is(err, chat.ErrUnavailable) {
		http.Error(w, "chat is unavailable: the session collector on the host does not answer. Is aacpanel-agent running?",
			http.StatusServiceUnavailable)
		return
	}
	if errors.Is(err, chat.ErrNoSubagents) {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	http.Error(w, err.Error(), http.StatusNotFound)
}

func (s *Server) apiChat(w http.ResponseWriter, r *http.Request) {
	if !s.chat.Available() {
		http.Error(w, "chat is unavailable: the collector socket is not mounted", http.StatusServiceUnavailable)
		return
	}
	name := r.URL.Query().Get("session")
	target, err := s.chatTarget(r, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	req := chatWindow(r, target)
	reply, err := s.chat.Feed(r.Context(), req)
	if err != nil {
		chatFail(w, err)
		return
	}
	body := map[string]any{
		"session":    name,
		"items":      reply.Items,
		"total":      reply.Total,
		"moreBefore": reply.MoreBefore,
		"first":      reply.First,
		"last":       reply.Last,
	}
	if reply.State != nil {
		body["state"] = reply.State
	}
	writeJSON(w, body)
}

func (s *Server) apiChatImage(w http.ResponseWriter, r *http.Request) {
	if !s.chat.Available() {
		http.Error(w, "chat is unavailable: the collector socket is not mounted", http.StatusServiceUnavailable)
		return
	}
	target, err := s.chatTarget(r, r.URL.Query().Get("session"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	pos, err := strconv.ParseInt(r.URL.Query().Get("pos"), 10, 64)
	if err != nil || pos < 0 {
		http.Error(w, "an attachment position is required", http.StatusBadRequest)
		return
	}
	index, err := strconv.Atoi(r.URL.Query().Get("i"))
	if err != nil || index < 0 {
		http.Error(w, "an attachment number is required", http.StatusBadRequest)
		return
	}

	media, body, err := s.chat.Image(r.Context(), target, pos, index)
	if err != nil {
		chatFail(w, err)
		return
	}
	w.Header().Set("Content-Type", media)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	_, _ = w.Write(body)
}

func (s *Server) apiChatCall(w http.ResponseWriter, r *http.Request) {
	if !s.chat.Available() {
		http.Error(w, "chat is unavailable: the collector socket is not mounted", http.StatusServiceUnavailable)
		return
	}
	target, err := s.chatTarget(r, r.URL.Query().Get("session"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	pos, err := strconv.ParseInt(r.URL.Query().Get("pos"), 10, 64)
	if err != nil || pos < 0 {
		http.Error(w, "a call position is required", http.StatusBadRequest)
		return
	}
	index, err := strconv.Atoi(r.URL.Query().Get("i"))
	if err != nil || index < 0 {
		http.Error(w, "a call number is required", http.StatusBadRequest)
		return
	}

	call, err := s.chat.CallOf(r.Context(), target, pos, index)
	if err != nil {
		chatFail(w, err)
		return
	}
	if !call.Pending {
		w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	}
	writeJSON(w, call)
}

func (s *Server) apiChatTask(w http.ResponseWriter, r *http.Request) {
	if !s.chat.Available() {
		http.Error(w, "chat is unavailable: the collector socket is not mounted", http.StatusServiceUnavailable)
		return
	}
	target, err := s.chatTarget(r, r.URL.Query().Get("session"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	task := r.URL.Query().Get("task")
	if task == "" {
		http.Error(w, "a task id is required", http.StatusBadRequest)
		return
	}
	reply, err := s.chat.TaskOutput(r.Context(), target, task)
	if err != nil {
		chatFail(w, err)
		return
	}
	writeJSON(w, map[string]any{"text": reply.Text, "cut": reply.Cut, "size": reply.Size})
}

func (s *Server) apiChatAgent(w http.ResponseWriter, r *http.Request) {
	if !s.chat.Available() {
		http.Error(w, "chat is unavailable: the collector socket is not mounted", http.StatusServiceUnavailable)
		return
	}
	target, err := s.chatTarget(r, r.URL.Query().Get("session"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "an agent name is required", http.StatusBadRequest)
		return
	}
	letters, err := s.chat.AgentMail(r.Context(), target.Session, name)
	if err != nil {
		chatFail(w, err)
		return
	}
	writeJSON(w, map[string]any{"letters": letters})
}

func (s *Server) apiChatFile(w http.ResponseWriter, r *http.Request) {
	if !s.chat.Available() {
		http.Error(w, "chat is unavailable: the collector socket is not mounted", http.StatusServiceUnavailable)
		return
	}
	target, err := s.chatTarget(r, r.URL.Query().Get("session"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "a file path is required", http.StatusBadRequest)
		return
	}
	offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
	size, _ := strconv.Atoi(r.URL.Query().Get("bytes"))
	reply, err := s.chat.FileOf(r.Context(), target, path, offset, size)
	if err != nil {
		chatFail(w, err)
		return
	}
	writeJSON(w, map[string]any{"text": reply.Text, "cut": reply.Cut,
		"size": reply.Size, "name": reply.Name, "binary": reply.Binary,
		"kind": reply.Kind, "offset": reply.Offset, "next": reply.Next,
		"tooBig": reply.TooBig, "media": reply.Media, "data": reply.Data,
		"mode": reply.Mode})
}

func (s *Server) apiSessionsArchive(w http.ResponseWriter, r *http.Request) {
	if !s.chat.Available() {
		http.Error(w, "the archive is unavailable: the collector socket is not mounted", http.StatusServiceUnavailable)
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	var skip []string
	if raw := q.Get("skip"); raw != "" {
		skip = strings.Split(raw, ",")
	}

	req := chat.ArchiveReq{
		Limit:   limit,
		Offset:  offset,
		Skip:    skip,
		Started: q.Get("started") == "1",
	}
	var picks []string
	for _, name := range q["profile"] {
		if name = strings.TrimSpace(name); name != "" {
			picks = append(picks, name)
		}
	}
	byID, err := s.contourPicks(r.Context(), q["contour"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	picks = append(picks, byID...)
	if len(picks) == 1 {
		req.Profile = picks[0]
	} else if len(picks) > 1 {
		req.Profiles = picks
	}

	page, err := s.chat.Archive(r.Context(), req)
	if err != nil {
		chatFail(w, err)
		return
	}
	s.placeArchive(r.Context(), page.Rows)
	writeJSON(w, page)
}

func (s *Server) placeArchive(ctx context.Context, rows []chat.ArchiveRow) {
	if s.db == nil || len(rows) == 0 {
		return
	}
	list, err := s.db.Profiles(ctx)
	if err != nil {
		log.Printf("session archive: the profile map is unavailable, the rows go without a project: %v", err)
		return
	}
	placeRows(rows, list)
}

func placeRows(rows []chat.ArchiveRow, list []store.Profile) {
	byPath := make(map[string]*chat.ArchiveProject)
	bySlug := make(map[string]*chat.ArchiveProject)
	for _, profile := range list {
		for _, group := range profile.Groups {
			for _, p := range group.Projects {
				dir := strings.TrimRight(p.Path, "/")
				if dir == "" {
					continue
				}
				found := &chat.ArchiveProject{ID: p.ID, Name: p.Name, Path: p.Path, Group: group.Name}
				if _, taken := byPath[dir]; !taken {
					byPath[dir] = found
				}
				if slug := archiveSlug(dir); slug != "" {
					if _, taken := bySlug[slug]; !taken {
						bySlug[slug] = found
					}
				}
			}
		}
	}

	for i := range rows {
		if dir := strings.TrimRight(rows[i].CWD, "/"); dir != "" {
			if found, ok := byPath[dir]; ok {
				rows[i].Project = found
				continue
			}
		}
		if found, ok := bySlug[rows[i].Slug]; ok && rows[i].Slug != "" {
			rows[i].Project = found
		}
	}
}

var archiveSlugRep = strings.NewReplacer("/", "-", "_", "-", ".", "-")

func archiveSlug(dir string) string {
	return archiveSlugRep.Replace(strings.TrimRight(dir, "/"))
}

const chatPoll = 1500 * time.Millisecond

func (s *Server) apiChatStream(w http.ResponseWriter, r *http.Request) {
	if !s.chat.Available() {
		http.Error(w, "chat is unavailable: the collector socket is not mounted", http.StatusServiceUnavailable)
		return
	}
	name := r.URL.Query().Get("session")
	target, err := s.chatTarget(r, name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	rc := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	rc.Flush()

	send := func(event string, payload any) bool {
		body, err := json.Marshal(payload)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, body); err != nil {
			return false
		}
		return rc.Flush() == nil
	}

	after := int64(-1)
	if v := r.URL.Query().Get("after"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			after = n
		}
	}

	ticker := time.NewTicker(chatPoll)
	defer ticker.Stop()
	lastState := ""
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}

		req := chat.Req{Session: target.Session, Subagent: target.Subagent, Limit: 40,
			State: target.Subagent == ""}
		if after >= 0 {
			pos := after
			req.After = &pos
		}
		reply, err := s.chat.Feed(r.Context(), req)
		if err != nil {
			if r.Context().Err() != nil {
				return
			}
			if !send("trouble", map[string]string{"error": err.Error()}) {
				return
			}
			continue
		}
		if reply.State != nil {
			if body, err := json.Marshal(reply.State); err == nil && string(body) != lastState {
				lastState = string(body)
				if !send("state", reply.State) {
					return
				}
			}
		}
		if len(reply.Items) == 0 {
			continue
		}
		if reply.Last != nil {
			after = *reply.Last
		}
		if !send("chat", map[string]any{"items": reply.Items, "total": reply.Total}) {
			return
		}
	}
}
