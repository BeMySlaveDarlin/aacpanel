package store

import (
	"encoding/json"
	"strings"
	"testing"

	"aacpanel/internal/schema"
)

func TestLaunchMustBeObjectPG(t *testing.T) {
	s, root := profileStore(t)
	ctx := t.Context()

	for _, raw := range []string{`5`, `"opus"`, `[1,2]`, `null`, `{`} {
		e := edit("contour-"+raw, root)
		e.Launch = json.RawMessage(raw)
		if _, err := s.CreateProfile(ctx, e); err == nil {
			t.Errorf("the launch parameters %s were accepted as an object", raw)
		}
	}

	e := edit("normal", root)
	e.Launch = json.RawMessage(`{"model":"opus","env":{"FOO":"bar"}}`)
	if _, err := s.CreateProfile(ctx, e); err != nil {
		t.Fatalf("normal parameters were rejected: %v", err)
	}
}

func TestIntentIsCheckedAsOurOwnKey(t *testing.T) {
	intent, _ := schema.Find("intent")
	intentMax := intent.MaxLen
	bad := []struct {
		name   string
		launch string
	}{
		{"not a string", `{"intent":42}`},
		{"longer than the ceiling", `{"intent":"` + strings.Repeat("é", intentMax+1) + `"}`},
		{"a control character", `{"intent":"stop\u001bhere"}`},
	}
	for _, c := range bad {
		t.Run(c.name, func(t *testing.T) {
			if _, err := checkLaunch(json.RawMessage(c.launch), schema.LevelProject); err == nil {
				t.Errorf("such an intent was accepted silently: %s", c.launch)
			}
		})
	}

	good := []struct {
		name   string
		launch string
	}{
		{"an empty intent", `{"intent":""}`},
		{"multi-line", `{"intent":"the first line\nthe second"}`},
		{"exactly at the ceiling", `{"intent":"` + strings.Repeat("é", intentMax) + `"}`},
		{"no intent", `{"model":"opus"}`},
	}
	for _, c := range good {
		t.Run(c.name, func(t *testing.T) {
			if _, err := checkLaunch(json.RawMessage(c.launch), schema.LevelProject); err != nil {
				t.Errorf("a legitimate intent was rejected: %v", err)
			}
		})
	}
}
