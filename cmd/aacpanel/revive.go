package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/heavy"
	"aacpanel/internal/hostcfg"
	"aacpanel/internal/notify"
	"aacpanel/internal/schema"
	"aacpanel/internal/store"
)

type reviveConfig struct {
	Reserve, Session  uint64
	SlicePercent      uint64
	PSI               float64
	Wait, Alive, Poll time.Duration
}

func readReviveConfig() (reviveConfig, error) {
	c := reviveConfig{SlicePercent: 90, PSI: 5, Wait: 5 * time.Minute, Alive: 2 * time.Minute, Poll: 2 * time.Second}
	for _, s := range []struct {
		key, def string
		to       *uint64
	}{
		{"AACP_REVIVE_RESERVE", "2G", &c.Reserve}, {"AACP_REVIVE_SESSION_MEMORY", "768M", &c.Session},
	} {
		v, ok := hostcfg.SizeBytes(env(s.key, s.def))
		if !ok || v == 0 {
			return c, fmt.Errorf("%s must be a positive memory size", s.key)
		}
		*s.to = v
	}
	var err error
	if c.SlicePercent, err = strconv.ParseUint(env("AACP_REVIVE_SLICE_PERCENT", "90"), 10, 64); err != nil || c.SlicePercent == 0 || c.SlicePercent > 100 {
		return c, fmt.Errorf("AACP_REVIVE_SLICE_PERCENT must be between 1 and 100")
	}
	if c.PSI, err = strconv.ParseFloat(env("AACP_REVIVE_MEMORY_PSI", "5"), 64); err != nil || math.IsNaN(c.PSI) || c.PSI <= 0 || c.PSI > 100 {
		return c, fmt.Errorf("AACP_REVIVE_MEMORY_PSI must be above zero and at most 100")
	}
	for _, s := range []struct {
		key string
		to  *time.Duration
	}{
		{"AACP_REVIVE_WAIT", &c.Wait}, {"AACP_REVIVE_ALIVE_WAIT", &c.Alive}, {"AACP_REVIVE_POLL", &c.Poll},
	} {
		if *s.to, err = envDuration(s.key, *s.to); err != nil {
			return c, err
		}
		if *s.to <= 0 {
			return c, fmt.Errorf("%s must be positive", s.key)
		}
	}
	return c, nil
}

type reviver struct {
	srv     *Server
	config  reviveConfig
	read    func(time.Time) (livingSnapshot, error)
	measure func(context.Context) (*action.Headroom, error)
	boot    string
	sleep   bool
}

func (r *reviver) wait(ctx context.Context) error {
	t := time.NewTimer(r.config.Poll)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (s *Server) runRevival(ctx context.Context, c reviveConfig) {
	if s.exec == nil {
		return
	}
	r := &reviver{srv: s, config: c, read: s.livingSnapshot, measure: s.exec.Headroom}
	lastErr := ""
	for ctx.Err() == nil {
		snap, err := r.read(time.Now())
		var room *action.Headroom
		if err == nil {
			room, err = r.measure(ctx)
		}
		if err == nil && (room.BootID == "" || room.BootID != snap.BootID) {
			err = fmt.Errorf("the executor and the census do not name the same host boot")
		}
		if err == nil {
			err = s.keepLiving(ctx, time.Now())
		}
		if err != nil {
			if err.Error() != lastErr {
				log.Printf("reboot recovery waits: %v", err)
				lastErr = err.Error()
			}
			if r.wait(ctx) != nil {
				return
			}
			continue
		}
		hostID, err := s.db.HostID(ctx, s.hostName)
		if err != nil {
			if r.wait(ctx) != nil {
				return
			}
			continue
		}
		previous, rows, claimed, err := s.db.ClaimRevival(ctx, hostID, snap.BootID)
		if err != nil {
			log.Printf("reboot recovery claim: %v", err)
			if r.wait(ctx) != nil {
				return
			}
			continue
		}
		if !claimed || previous == "" {
			return
		}
		if local, err := os.ReadFile("/proc/sys/kernel/random/boot_id"); err == nil {
			log.Printf("reboot recovery: container boot identity matches the host: %t", strings.TrimSpace(string(local)) == snap.BootID)
		}
		r.boot = snap.BootID
		s.pauseMu.Lock()
		total, raised, failed := r.recover(ctx, rows)
		s.pauseMu.Unlock()
		message := notify.RevivalMessage(snap.BootID, total, raised, failed)
		log.Printf("reboot recovery: %s", message.Body)
		if s.pushPrefs(ctx).Allows(message) {
			s.push.Send(message)
		}
		return
	}
}

func liveConversation(s livingSnapshot, id string) (livingRow, bool) {
	for _, row := range s.Sessions {
		if row.SessionID == id {
			return row, true
		}
	}
	return livingRow{}, false
}

func (r *reviver) census() (livingSnapshot, error) {
	snap, err := r.read(time.Now())
	if err == nil && snap.BootID != r.boot {
		err = fmt.Errorf("the host boot changed during recovery")
	}
	return snap, err
}

func (r *reviver) candidates(ctx context.Context, rows []store.LivingSession) ([]store.LivingSession, error) {
	list, err := r.srv.db.Profiles(ctx)
	if err != nil {
		return nil, err
	}
	snap, err := r.census()
	if err != nil {
		return nil, err
	}
	out := []store.LivingSession{}
	pauses, err := r.srv.sessionPauses(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if p, ok := pauseConversation(pauses, row.Conversation); ok && p.Sleeping {
			continue
		}
		if row.Outside {
			continue
		}
		if _, alive := liveConversation(snap, row.Conversation); alive {
			continue
		}
		var contour, project map[string]any
		for _, profile := range list {
			if (row.ConfigDir != "" && row.ConfigDir == profile.ConfigDir) || (row.ConfigDir == "" && row.Contour != "" && row.Contour == profile.Name) {
				if err := json.Unmarshal(profile.Launch, &contour); err != nil {
					return nil, err
				}
				break
			}
		}
		if found, ok := projectByID(list, row.ProjectID); ok {
			if err := json.Unmarshal(found.profile.Launch, &contour); err != nil {
				return nil, err
			}
			if err := json.Unmarshal(found.project.Launch, &project); err != nil {
				return nil, err
			}
		}
		enabled := true
		for _, v := range schema.Effective(nil, contour, project) {
			if v.Key == "reviveAfterReboot" {
				enabled, _ = v.Value.(bool)
			}
		}
		if !enabled {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

// Capacity uses exactly the queue's admission calculation, including growth
// reserved for heavy runs. A newly resumed batch reserves one session's
// allowance per member until the next complete census sees all of them.
func (r *reviver) capacity(ctx context.Context, n int) (int, error) {
	wait, cancel := context.WithTimeout(ctx, r.config.Wait)
	defer cancel()
	why := "host headroom is not available"
	for {
		room, err := r.measure(wait)
		if err == nil && room.BootID != r.boot {
			return 0, fmt.Errorf("the host boot changed during recovery")
		}
		if err == nil {
			limits := heavy.Limits{Reserve: r.config.Reserve, Budget: room.High / 100 * r.config.SlicePercent, MemoryPressure: r.config.PSI}
			asks := make([]uint64, min(n, 5))
			for i := range asks {
				asks[i] = r.config.Session
			}
			goes, blocked := heavy.Admit(limits, room.Measure, asks)
			if len(goes) > 0 {
				return len(goes), nil
			}
			why = blocked
		} else {
			why = err.Error()
		}
		if r.wait(wait) != nil {
			return 0, fmt.Errorf("headroom did not recover: %s", why)
		}
	}
}

func (r *reviver) waitAlive(ctx context.Context, batch []store.LivingSession) (map[string]livingRow, error) {
	wait, cancel := context.WithTimeout(ctx, r.config.Alive)
	defer cancel()
	live := map[string]livingRow{}
	for {
		snap, err := r.census()
		if err == nil {
			live = map[string]livingRow{}
			for _, row := range batch {
				if found, ok := liveConversation(snap, row.Conversation); ok {
					live[row.Conversation] = found
				}
			}
			if len(live) == len(batch) {
				return live, nil
			}
		}
		if r.wait(wait) != nil {
			return live, fmt.Errorf("the previous batch did not appear in a fresh census")
		}
	}
}

func (r *reviver) recover(ctx context.Context, rows []store.LivingSession) (total, raised int, failed []string) {
	pending, err := r.candidates(ctx, rows)
	if err != nil {
		return len(rows), 0, []string{err.Error()}
	}
	total, raised, more := r.raise(ctx, pending, len(pending), r.restorePause)
	return total, raised, append(failed, more...)
}

// resumeParams resumes a conversation of claude by its id and name, and a
// thread of codex through the daemon of its home, named by its contour.
func resumeParams(row store.LivingSession) map[string]any {
	if row.Agent == schema.AgentCodex {
		return map[string]any{"session": row.Conversation, "agent": action.ResumeCodex, "contour": row.Contour}
	}
	return map[string]any{"session": row.Conversation, "name": row.Session}
}

// Sleep and reboot recovery share admission and census confirmation. A group
// wakes parents first; a child waking alone can leave its parent asleep.
func (r *reviver) raise(ctx context.Context, pending []store.LivingSession, total int, after func(context.Context, store.LivingSession, livingRow) error) (int, int, []string) {
	r.srv.raiseMu.Lock()
	defer r.srv.raiseMu.Unlock()
	raised := 0
	var failed []string
	for len(pending) > 0 {
		n, err := r.capacity(ctx, len(pending))
		if err != nil {
			for _, row := range pending {
				failed = append(failed, row.Session+": "+err.Error())
			}
			break
		}
		batch := []store.LivingSession{}
		next := []store.LivingSession{}
		for _, row := range pending {
			snap, err := r.census()
			if err == nil && row.ParentConversation != "" {
				if _, alive := liveConversation(snap, row.ParentConversation); !alive {
					waiting := false
					for _, parent := range pending {
						if parent.Conversation == row.ParentConversation {
							waiting = true
							break
						}
					}
					if waiting {
						next = append(next, row)
						continue
					}
				}
			}
			if len(batch) >= n {
				next = append(next, row)
				continue
			}
			if err == nil {
				if current, alive := liveConversation(snap, row.Conversation); alive {
					if err := after(ctx, row, current); err != nil {
						failed = append(failed, row.Session+": "+err.Error())
					} else {
						raised++
					}
					continue
				}
			}
			if err == nil {
				resumeCtx := ctx
				if r.sleep {
					resumeCtx = context.WithValue(ctx, sleepResumeKey{}, row)
					resumeCtx = context.WithValue(resumeCtx, automaticActionKey{}, "session wake")
				}
				err = r.srv.automaticAction(resumeCtx, string(action.SessionResume), row.Session, resumeParams(row))
			}
			if err != nil {
				failed = append(failed, row.Session+": "+err.Error())
				continue
			}
			batch = append(batch, row)
		}
		if len(next) == len(pending) {
			for _, row := range next {
				failed = append(failed, row.Session+": parent did not become live")
			}
			break
		}
		pending = next
		live, aliveErr := r.waitAlive(ctx, batch)
		for _, row := range batch {
			current, ok := live[row.Conversation]
			if !ok {
				failed = append(failed, row.Session+": did not become live")
				continue
			}
			if err := after(ctx, row, current); err != nil {
				failed = append(failed, row.Session+": pause: "+err.Error())
				continue
			}
			raised++
		}
		if aliveErr != nil {
			for _, row := range pending {
				failed = append(failed, row.Session+": "+aliveErr.Error())
			}
			break
		}
	}
	return total, raised, failed
}

func (r *reviver) restorePause(ctx context.Context, row store.LivingSession, current livingRow) error {
	if pauses, err := r.srv.sessionPauses(ctx); err != nil {
		return err
	} else if p, ok := pauseConversation(pauses, row.Conversation); ok {
		if p.Until == nil || p.Until.After(time.Now()) {
			return nil
		}
		result, err := r.srv.applyPause(automaticRequest(ctx), action.Pause{Project: row.ProjectID, Resume: true}, []pauseSession{{LiveSession: current.LiveSession}}, row.Session, map[string]any{"resume": true}, time.Time{})
		if err != nil {
			return err
		}
		if len(result.Failed) > 0 {
			return fmt.Errorf("%s", result.Failed[0]["error"])
		}
		return nil
	}
	if len(row.Pause) == 0 || string(row.Pause) == "null" {
		return nil
	}
	var alarm pauseAlarm
	if err := json.Unmarshal(row.Pause, &alarm); err != nil {
		return err
	}
	p := action.Pause{Project: row.ProjectID, Session: current.Name}
	var until time.Time
	if alarm.Name == action.PauseNoEndAlarm {
		p.NoEnd = true
	} else {
		var err error
		until, err = time.Parse(time.RFC3339, alarm.Next)
		if err != nil {
			return err
		}
		if !until.After(time.Now()) {
			p.Resume = true
			until = time.Time{}
		} else {
			p.Until = alarm.Next
		}
	}
	params := map[string]any{"project": p.Project, "session": p.Session, "resume": p.Resume, "noEnd": p.NoEnd, "until": p.Until}
	// Resolve the live scope as for a pause. Continue after a reboot has no
	// alarm yet, so it sends the same continuation without cancelling one.
	selecting := p
	selecting.Resume = false
	targets, label, err := r.srv.pauseTargets(ctx, selecting)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return fmt.Errorf("the restored session is no longer in its project")
	}
	result, err := r.srv.applyPause(automaticRequest(ctx), p, targets, label, params, until)
	if err != nil {
		return err
	}
	if len(result.Failed) > 0 {
		return fmt.Errorf("%s", result.Failed[0]["error"])
	}
	return nil
}
