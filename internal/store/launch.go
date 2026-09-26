package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"aacpanel/internal/schema"
)

// EffectiveLaunch merges the project's launch parameters on top of the profile's,
// the way the schema lays one level over another.
func EffectiveLaunch(profile, project json.RawMessage) (json.RawMessage, error) {
	base, err := launchObject(profile, "the profile")
	if err != nil {
		return nil, err
	}
	over, err := launchObject(project, "the project")
	if err != nil {
		return nil, err
	}
	return json.Marshal(schema.Launch(base, over))
}

func launchObject(raw json.RawMessage, whose string) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("the launch parameters of %s were not parsed: %w", whose, err)
	}
	return out, nil
}

// launchChange returns the launch parameters a change of some keys leaves:
// the stored row, read under a lock, with the named keys removed and the given
// ones laid over, checked as a whole. Two screens saving different keys a
// minute apart keep each other's work, which a whole object sent back would not.
func launchChange(ctx context.Context, tx pgx.Tx, table string, id int, set map[string]any, unset []string,
	level schema.Level) (json.RawMessage, error) {
	var cur json.RawMessage
	err := tx.QueryRow(ctx, `SELECT launch FROM `+table+` WHERE id = $1 FOR UPDATE`, id).Scan(&cur)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("%w: there is no %s %d", ErrNotFound, level, id)
	}
	if err != nil {
		return nil, err
	}
	obj, err := launchObject(cur, "the stored row")
	if err != nil {
		return nil, err
	}
	if obj == nil {
		obj = map[string]any{}
	}
	for _, key := range unset {
		delete(obj, key)
	}
	for key, value := range set {
		obj[key] = value
	}
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return checkLaunch(raw, level)
}

// launchOps says whether an edit changes some keys of the launch, and refuses
// one that also sends the launch whole: which of the two wins would be a guess.
func launchOps(launch json.RawMessage, set map[string]any, unset []string) (bool, error) {
	if len(set) == 0 && len(unset) == 0 {
		return false, nil
	}
	if len(launch) > 0 {
		return false, badRequest("the launch parameters are sent whole or changed key by key, not both at once")
	}
	for _, key := range unset {
		if _, ok := set[key]; ok {
			return false, badRequest("%s is both set and removed", key)
		}
	}
	return true, nil
}
