package main

import (
	"context"
	"encoding/json"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/chat"
	"aacpanel/internal/notify"
	"aacpanel/internal/store"
)

const (
	watchEvery   = 20 * time.Second
	panelDidFor  = 5 * time.Minute
	watchTimeout = 15 * time.Second

	// callWait is how long a request for the calls of the sessions waits at
	// the collector for the next one before it is asked again.
	callWait = chat.MaxNoteWait
	// callRetry is the pause after the collector did not answer, and the wait
	// while a call waits for its session to reach the snapshot.
	callRetry = 2 * time.Second
)

type watcher struct {
	srv *Server

	// saying guards the tracker: the looks of the host and the calls of the
	// sessions reach it from two goroutines.
	saying  sync.Mutex
	tracker *notify.Tracker

	prev  notify.World
	calls notify.Calls

	mu      sync.Mutex
	did     map[string]time.Time
	stackOf map[string]string
	// stacks are the stacks of the last look, for the choice of pushes.
	stacks []string
}

func newWatcher(s *Server, j notify.Journal) *watcher {
	return &watcher{
		srv:     s,
		tracker: notify.NewTracker(j),
		did:     map[string]time.Time{},
		stackOf: map[string]string{},
	}
}

// Run collects the host view on a schedule, and carries the calls of the
// sessions as they are made.
func (w *watcher) Run(ctx context.Context) {
	go w.carryCalls(ctx)

	t := time.NewTicker(watchEvery)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		w.once(ctx)
	}
}

func (w *watcher) once(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, watchTimeout)
	defer cancel()

	cur := w.look(ctx)
	w.say(ctx, notify.Look(w.prev, cur))
	w.prev = cur

	stacks := make([]string, 0, len(cur.Stacks))
	for _, s := range cur.Stacks {
		stacks = append(stacks, s.Name)
	}
	w.mu.Lock()
	w.stacks = stacks
	w.mu.Unlock()
}

// say sends what a report is worth saying, as the person chose.
func (w *watcher) say(ctx context.Context, r notify.Report) {
	w.saying.Lock()
	defer w.saying.Unlock()

	prefs := w.srv.pushPrefs(ctx)
	for _, m := range w.tracker.Step(ctx, r) {
		if !prefs.Allows(m) {
			log.Printf("notify: quiet by choice: %s — %s", m.Title, m.Body)
			continue
		}
		log.Printf("notify: %s — %s", m.Title, m.Body)
		w.srv.push.Send(m)
	}
}

// carryCalls pushes the calls of the sessions as the collector takes them. A
// request waits at the collector until a call comes, so a call reaches the
// phone the moment it is made rather than at the next look of the host, which
// comes on a tick and reads a snapshot that lags behind it.
func (w *watcher) carryCalls(ctx context.Context) {
	if !w.srv.chat.Available() {
		return
	}
	var seq *int64
	wait := callWait
	down := false
	for ctx.Err() == nil {
		board, err := w.srv.chat.Notes(ctx, seq, wait)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if !down {
				log.Printf("notify: the calls of the sessions do not come: %v", err)
				down = true
			}
			seq = nil
			select {
			case <-ctx.Done():
				return
			case <-time.After(callRetry):
			}
			continue
		}
		if down {
			log.Print("notify: the calls of the sessions come again")
			down = false
		}
		seq = &board.Seq
		wait = callWait
		if w.carry(ctx, board.Notes) {
			wait = callRetry
		}
	}
}

// carry pushes the calls of the board not carried yet and says whether one
// waits for its session to reach the snapshot.
func (w *watcher) carry(ctx context.Context, board []chat.SessionNote) bool {
	if len(board) == 0 {
		return false
	}
	notes := make([]notify.Note, 0, len(board))
	for _, n := range board {
		notes = append(notes, notify.Note{Session: n.SessionID, Text: n.Text, At: n.At})
	}
	var cur notify.World
	w.readSnapshot(&cur)
	news, waiting := w.calls.Fresh(time.Now(), notes, cur.Sessions)
	if len(news) > 0 {
		w.say(ctx, notify.Report{Raise: news})
	}
	return waiting
}

// Stacks returns the stacks the last look saw.
func (w *watcher) Stacks() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.stacks...)
}

// Expect records that the panel itself changed the state of the target.
func (w *watcher) Expect(kind action.Kind, target string) {
	if target == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	now := time.Now()
	switch kind {
	case action.ContainerStart, action.ContainerStop, action.ContainerRestart:
		w.did["container:"+target] = now
	case action.StackUp, action.StackDown:
		w.did["stack:"+target] = now
		for name, stack := range w.stackOf {
			if stack == target {
				w.did["container:"+name] = now
			}
		}
	case action.SessionClose, action.SessionKill, action.SessionRestart, action.SessionSwitch:
		w.did["session:"+target] = now
	}
}

func (w *watcher) panelDid(now time.Time) map[string]bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	out := make(map[string]bool, len(w.did))
	for key, at := range w.did {
		if now.Sub(at) > panelDidFor {
			delete(w.did, key)
			continue
		}
		out[key] = true
	}
	return out
}

func (w *watcher) look(ctx context.Context) notify.World {
	now := time.Now()
	cur := notify.World{Now: now, DBOff: w.srv.db == nil}

	w.readSnapshot(&cur)
	w.readDocker(ctx, &cur)
	w.readBriefs(ctx, &cur)
	w.readStore(ctx, &cur)
	cur.PanelDid = w.panelDid(now)
	return cur
}

type snapshot struct {
	AgeSec         int64 `json:"ageSec"`
	SessionsAgeSec int64 `json:"sessionsAgeSec"`
	Sessions       []struct {
		Name    string `json:"session"`
		ID      string `json:"sessionId"`
		CWD     string `json:"cwd"`
		Profile string `json:"profile"`
		// ConfigDir is the account's configuration directory: the key a
		// contour is named by in the person's choice of pushes.
		ConfigDir string `json:"configDir"`
		Status    string `json:"status"`
		StatusAt  int64  `json:"statusUpdatedAt"`
		Waiting   string `json:"waitingFor"`
		// How the session is kept: in tmux, or on the stream under a holder.
		Transport string `json:"transport"`
		Ask       *struct {
			Header string `json:"header"`
			Text   string `json:"text"`
			Count  int    `json:"count"`
			At     string `json:"at"`
		} `json:"ask"`
	} `json:"sessions"`
	Limits *limitsBlock `json:"limits"`
}

type limitsBlock struct {
	limitRow
	Contours []limitRow `json:"contours"`
}

type limitRow struct {
	Profile   string       `json:"profile"`
	ConfigDir string       `json:"configDir"`
	FiveHour  *limitWindow `json:"fiveHour"`
	SevenDay  *limitWindow `json:"sevenDay"`
}

type limitWindow struct {
	Pct      float64 `json:"pct"`
	ResetsAt int64   `json:"resetsAt"`
}

func (w *watcher) readSnapshot(cur *notify.World) {
	payload, err := w.srv.host.JSON()
	if err != nil {
		cur.AgentErr = shortReason(err.Error())
		return
	}
	var snap snapshot
	if err := json.Unmarshal(payload, &snap); err != nil {
		cur.AgentErr = "the snapshot cannot be parsed"
		return
	}

	cur.AgentAge = time.Duration(max(snap.AgeSec, snap.SessionsAgeSec)) * time.Second

	for _, s := range snap.Sessions {
		item := notify.Session{
			ID: s.ID, Name: s.Name, Profile: s.Profile, ConfigDir: s.ConfigDir, CWD: s.CWD,
			Status: s.Status, StatusAt: s.StatusAt, WaitingFor: s.Waiting, Transport: s.Transport,
		}
		if s.Ask != nil {
			item.Ask = &notify.Ask{Header: s.Ask.Header, Text: s.Ask.Text, Count: s.Ask.Count, At: s.Ask.At}
		}
		cur.Sessions = append(cur.Sessions, item)
	}

	if snap.Limits == nil {
		return
	}
	cur.LimitsSeen = true
	rows := snap.Limits.Contours
	if len(rows) == 0 && snap.Limits.Profile != "" {
		rows = append(rows, snap.Limits.limitRow)
	}
	for _, row := range rows {
		if row.FiveHour != nil {
			cur.Limits = append(cur.Limits, notify.Limit{
				Contour: row.Profile, ConfigDir: row.ConfigDir, Window: "5h",
				Pct: pctInt(row.FiveHour.Pct), ResetsAt: row.FiveHour.ResetsAt,
			})
		}
		if row.SevenDay != nil {
			cur.Limits = append(cur.Limits, notify.Limit{
				Contour: row.Profile, ConfigDir: row.ConfigDir, Window: "7d",
				Pct: pctInt(row.SevenDay.Pct), ResetsAt: row.SevenDay.ResetsAt,
			})
		}
	}
}

func pctInt(v float64) int { return int(math.Round(v)) }

func (w *watcher) readDocker(ctx context.Context, cur *notify.World) {
	tree, err := w.srv.docker.Tree(ctx, nil)
	if err != nil {
		cur.DockerErr = shortReason(err.Error())
		return
	}
	stackOf := make(map[string]string, tree.Total)
	for _, stack := range tree.Stacks {
		cur.Stacks = append(cur.Stacks, notify.Stack{
			Name: stack.Name, Running: stack.Running, Total: stack.Total,
		})
		for _, c := range stack.Containers {
			cur.Containers = append(cur.Containers, notify.Container{
				Name: c.Name, Stack: stack.Name, State: c.State, Status: c.Status, Health: c.Health,
			})
			stackOf[c.Name] = stack.Name
		}
	}
	w.mu.Lock()
	w.stackOf = stackOf
	w.mu.Unlock()
}

// readBriefs reads the shelf of the host. A collector that does not know
// briefs, or one that cannot be reached, leaves the shelf unseen rather than
// empty: an empty shelf and an unreadable one look the same from here, and one
// of them would announce every standing document as new the moment it answers.
func (w *watcher) readBriefs(ctx context.Context, cur *notify.World) {
	cards, err := w.srv.chat.Briefs(ctx, "")
	if err != nil {
		return
	}
	cur.BriefsSeen = true
	live := w.sessionNames()
	for _, card := range cards {
		cur.Briefs = append(cur.Briefs, notify.Brief{
			ID: card.ID, Title: card.Title, At: card.At,
			Questions: card.Questions, Session: live[card.SessionID],
		})
	}
}

// sessionNames maps a session by its id to the name it goes by right now.
func (w *watcher) sessionNames() map[string]string {
	out := map[string]string{}
	payload, err := w.srv.host.JSON()
	if err != nil {
		return out
	}
	var snap struct {
		Sessions []struct {
			Name string `json:"session"`
			ID   string `json:"sessionId"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(payload, &snap); err != nil {
		return out
	}
	for _, s := range snap.Sessions {
		out[s.ID] = s.Name
	}
	return out
}

func (w *watcher) readStore(ctx context.Context, cur *notify.World) {
	if w.srv.db == nil {
		return
	}

	list, err := w.srv.db.Alerts(ctx, store.AlertsReq{Limit: 200})
	if err != nil {
		cur.DBErr = shortReason(err.Error())
		return
	}
	for _, a := range list {
		if a.ClosedAt != nil {
			continue
		}
		cur.Alerts = append(cur.Alerts, alertEvent(a))
	}

	probes, err := w.srv.db.Probes(ctx)
	if err != nil {
		cur.DBErr = shortReason(err.Error())
		cur.Alerts = nil
		return
	}
	for _, p := range probes {
		if !p.Enabled || p.Last == nil {
			continue
		}
		row := notify.Probe{
			ID: p.ID, Name: p.Name, Target: p.Target,
			OK: p.Last.OK, Outcome: p.Last.Outcome, Streak: p.FailStreak,
		}
		if p.Last.Error != nil {
			row.Error = shortReason(*p.Last.Error)
		}
		cur.Probes = append(cur.Probes, row)
	}
}

func alertEvent(a store.Alert) notify.Alert {
	out := notify.Alert{
		ID: a.ID, RuleID: a.RuleID, RuleKey: a.RuleKey, Rule: a.Rule, Subject: a.Subject, Severity: a.Severity,
		Value: a.Value,
	}
	var payload struct {
		Threshold float64 `json:"threshold"`
		Op        string  `json:"op"`
		Unit      string  `json:"unit"`
		ForSec    int     `json:"forSec"`
	}
	if len(a.Payload) > 0 {
		if err := json.Unmarshal(a.Payload, &payload); err == nil {
			out.Threshold, out.Op, out.Unit, out.ForSec = payload.Threshold, payload.Op, payload.Unit, payload.ForSec
		}
	}
	return out
}

func shortReason(reason string) string {
	reason = strings.TrimSpace(strings.ReplaceAll(reason, "\n", " "))
	const max = 160
	if len([]rune(reason)) <= max {
		return reason
	}
	return string([]rune(reason)[:max-1]) + "…"
}
