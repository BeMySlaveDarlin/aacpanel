package main

import (
	"testing"

	"aacpanel/internal/schema"
)

// A project row says what the project sets otherwise than its contour: a value
// it repeats, or one it inherits, is not its own.
func TestOwnValuesAreWhatDiffersFromTheContour(t *testing.T) {
	contour := schema.Effective(nil, map[string]any{"remoteControl": true, "effort": "high"}, nil)
	project := schema.Effective(nil, map[string]any{"remoteControl": true, "effort": "high"},
		map[string]any{"remoteControl": true, "effort": "max", "transport": "stream", "autoRestart": false})
	got := map[string]any{}
	for _, v := range ownValues(contour, project) {
		got[v.Key] = v.Value
	}
	if len(got) != 2 || got["effort"] != "max" || got["transport"] != "stream" {
		t.Errorf("own values %v — meant effort max and the stream; the repeated RC and the default-equal auto restart are not the project's own", got)
	}
}

// What the account already says is not the project's own either: a pin of the
// account's effort is a repeat, and the row stays quiet about it.
func TestOwnValuesSkipWhatTheAccountSays(t *testing.T) {
	account := map[string]any{"effort": "xhigh", "permissionMode": "auto"}
	contour := schema.Effective(account, nil, nil)
	project := schema.Effective(account, nil, map[string]any{"effort": "xhigh", "permissionMode": "plan"})
	got := ownValues(contour, project)
	if len(got) != 1 || got[0].Key != "permissionMode" {
		t.Errorf("own values %+v — only the mode differs from the account", got)
	}
}
