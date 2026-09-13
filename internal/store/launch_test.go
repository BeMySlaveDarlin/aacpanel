package store

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEffectiveLaunchOverridesFieldByField(t *testing.T) {
	cases := []struct {
		what    string
		profile string
		project string
		want    string
	}{
		{
			what:    "the project refines some of the fields",
			profile: `{"model":"opus","effort":"high","permissionMode":"plan"}`,
			project: `{"model":"sonnet"}`,
			want:    `{"model":"sonnet","effort":"high","permissionMode":"plan"}`,
		},
		{
			what:    "a field is overridden by the project",
			profile: `{"permissionMode":"plan"}`,
			project: `{"permissionMode":"bypassPermissions"}`,
			want:    `{"permissionMode":"bypassPermissions"}`,
		},
		{
			what:    "env is merged key by key",
			profile: `{"env":{"CLAUDE_CONFIG_DIR":"/home/u/.claude","LANG":"ru_RU.UTF-8"}}`,
			project: `{"env":{"LANG":"C","AACP_DEBUG":"1"}}`,
			want: `{"env":{"CLAUDE_CONFIG_DIR":"/home/u/.claude",` +
				`"LANG":"C","AACP_DEBUG":"1"}}`,
		},
		{
			what:    "args are replaced wholesale",
			profile: `{"args":["--verbose","--no-color"]}`,
			project: `{"args":["--resume"]}`,
			want:    `{"args":["--resume"]}`,
		},
		{
			what:    "the project does not name args — the profile's remain",
			profile: `{"args":["--verbose"]}`,
			project: `{"model":"opus"}`,
			want:    `{"args":["--verbose"],"model":"opus"}`,
		},
		{
			what:    "empty on both sides",
			profile: ``,
			project: ``,
			want:    `{}`,
		},
		{
			what:    "empty objects on both sides",
			profile: `{}`,
			project: `{}`,
			want:    `{}`,
		},
		{
			what:    "the profile is silent — we take the project whole",
			profile: ``,
			project: `{"model":"opus","env":{"A":"1"}}`,
			want:    `{"model":"opus","env":{"A":"1"}}`,
		},
		{
			what:    "the project is silent — we take the profile whole",
			profile: `{"model":"opus","args":["--verbose"]}`,
			project: `{}`,
			want:    `{"model":"opus","args":["--verbose"]}`,
		},
		{
			what:    "env on the project only",
			profile: `{"model":"opus"}`,
			project: `{"env":{"A":"1"}}`,
			want:    `{"model":"opus","env":{"A":"1"}}`,
		},
		{
			what:    "env on the profile only",
			profile: `{"env":{"A":"1"}}`,
			project: `{"model":"opus"}`,
			want:    `{"env":{"A":"1"},"model":"opus"}`,
		},
	}

	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			got, err := EffectiveLaunch(json.RawMessage(c.profile), json.RawMessage(c.project))
			if err != nil {
				t.Fatalf("the merge refused: %v", err)
			}
			if !sameJSON(t, got, c.want) {
				t.Errorf("got %s, expected %s", got, c.want)
			}
		})
	}
}

func TestEffectiveLaunchIsStable(t *testing.T) {
	profile := json.RawMessage(`{"model":"opus","effort":"high","env":{"B":"2","A":"1"}}`)
	project := json.RawMessage(`{"effort":"high","env":{"C":"3"}}`)

	first, err := EffectiveLaunch(profile, project)
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		again, err := EffectiveLaunch(profile, project)
		if err != nil {
			t.Fatal(err)
		}
		if string(again) != string(first) {
			t.Fatalf("two merges of one input gave %s and %s", first, again)
		}
	}
}

func TestEffectiveLaunchRefusesBrokenSide(t *testing.T) {
	cases := map[string][2]string{
		"a broken profile":        {`{"model":`, `{}`},
		"a broken project":        {`{}`, `{"model":`},
		"the profile is a number": {`5`, `{}`},
		"the project is a list":   {`{}`, `[1,2]`},
	}
	for what, pair := range cases {
		t.Run(what, func(t *testing.T) {
			_, err := EffectiveLaunch(json.RawMessage(pair[0]), json.RawMessage(pair[1]))
			if err == nil {
				t.Fatal("broken parameters merged without an error")
			}
			if !strings.Contains(err.Error(), "the profile") && !strings.Contains(err.Error(), "the project") {
				t.Errorf("the error does not name the side: %v", err)
			}
		})
	}
}

func TestEffectiveLaunchTellsEmptyIntentFromNoIntent(t *testing.T) {
	cases := []struct {
		name    string
		profile string
		project string
		want    string
	}{
		{
			name:    "the project is silent — the intent is inherited from the profile",
			profile: `{"intent":"/rs"}`,
			project: `{"model":"opus"}`,
			want:    "/rs",
		},
		{
			name:    "the project names its own — and that is what wins",
			profile: `{"intent":"/rs"}`,
			project: `{"intent":"run the checks"}`,
			want:    "run the checks",
		},
		{
			name:    "the project says \"empty\" — there is no intent at all",
			profile: `{"intent":"/rs"}`,
			project: `{"intent":""}`,
			want:    "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw, err := EffectiveLaunch(json.RawMessage(c.profile), json.RawMessage(c.project))
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			value, ok := got["intent"]
			if !ok {
				t.Fatalf("the intent key disappeared during the merge: %s", raw)
			}
			if value != c.want {
				t.Errorf("the intent after the merge is %q, %q was expected", value, c.want)
			}
		})
	}
}

func TestEffectiveLaunchCarriesFinalizeAt(t *testing.T) {
	cases := []struct {
		name    string
		profile string
		project string
		want    any
	}{
		{
			name:    "the project is silent — the threshold comes from the profile",
			profile: `{"finalizeAt":80}`,
			project: `{"model":"opus"}`,
			want:    float64(80),
		},
		{
			name:    "the project names its own — and that is what wins",
			profile: `{"finalizeAt":80}`,
			project: `{"finalizeAt":60}`,
			want:    float64(60),
		},
		{
			name:    "neither side names it — there is no key",
			profile: `{"model":"opus"}`,
			project: `{"effort":"high"}`,
			want:    nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw, err := EffectiveLaunch(json.RawMessage(c.profile), json.RawMessage(c.project))
			if err != nil {
				t.Fatal(err)
			}
			var got map[string]any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			value, ok := got["finalizeAt"]
			if c.want == nil {
				if ok {
					t.Fatalf("a threshold appeared out of nowhere: %s", raw)
				}
				return
			}
			// A number that came back as a string would slip past the
			// launcher's check and switch nothing on.
			if !ok || value != c.want {
				t.Errorf("the threshold after the merge is %v (%T), %v was expected", value, value, c.want)
			}
		})
	}
}
