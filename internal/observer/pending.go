package observer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Pending reports whether a conversation has an observer still running or
// an alarm with a ring left. It reads the keeper's records without starting
// a keeper or touching its units. Missing directories are empty: the keeper
// creates them on first use. Other read errors leave the answer unknown,
// which lets a Stop hook pass rather than accuse a session on missing facts.
func Pending(dir, conversation string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !idPattern.MatchString(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		rec, err := ReadRecord(path)
		if err != nil {
			return false, err
		}
		if rec.ID != entry.Name() || rec.Owner.Conversation == "" {
			return false, fmt.Errorf("the observer %s has no matching identity", entry.Name())
		}
		if rec.Owner.Conversation != conversation || rec.Ended != nil {
			continue
		}
		// The unit writes its end before the keeper records delivery of the
		// wake; the collector counts that end as soon as it appears too.
		if _, err := os.Stat(filepath.Join(path, endFile)); errors.Is(err, os.ErrNotExist) {
			return true, nil
		} else if err != nil {
			return false, err
		}
		if end, ok := readEnding(path); !ok || end.At.IsZero() {
			return false, fmt.Errorf("the end of observer %s cannot be read", entry.Name())
		}
	}
	entries, err = os.ReadDir(alarmDir(dir))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	for _, entry := range entries {
		id, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok || entry.IsDir() || !idPattern.MatchString(id) {
			continue
		}
		alarm, err := readAlarm(filepath.Join(alarmDir(dir), entry.Name()))
		if err != nil {
			return false, err
		}
		if alarm.ID != id || alarm.Owner.Conversation == "" || alarm.Next.IsZero() {
			return false, fmt.Errorf("the alarm %s has no matching identity or next ring", id)
		}
		// A late alarm still has its ring ahead: the keeper removes the
		// record only after delivery, or moves Next on for a repeat.
		if alarm.Owner.Conversation == conversation {
			return true, nil
		}
	}
	return false, nil
}
