package codex

import "errors"

// RestartIdle uses the loaded statuses the normal poll already reads, including
// child threads. Holding sub keeps the panel's queue and send actions from
// starting a turn between this decision and the restart command.
func (l *Link) RestartIdle(restart func() error) (int, error) {
	l.sub.Lock()
	defer l.sub.Unlock()
	l.mu.Lock()
	busy, known := l.runningTurns()
	l.mu.Unlock()
	if !known {
		return busy, errors.New("restart pending: daemon activity is not known")
	}
	if busy > 0 {
		return busy, nil
	}
	return 0, restart()
}

// runningTurns is called with mu held. ours covers the interval between the
// panel's turn/start and the daemon's status becoming active.
func (l *Link) runningTurns() (int, bool) {
	known := l.conn != nil && !l.conn.down() && l.polled && l.activity != nil
	busy := 0
	for id, status := range l.activity {
		switch status {
		case "active":
			busy++
		case "idle", "systemError", "notLoaded":
			if t := l.threads[id]; t != nil && t.ours {
				busy++
			}
		default:
			known = false
		}
	}
	for id, t := range l.threads {
		if _, read := l.activity[id]; !read && t.ours {
			busy++
		}
	}
	return busy, known
}
