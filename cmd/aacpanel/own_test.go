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
