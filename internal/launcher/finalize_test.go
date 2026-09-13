package launcher

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseParamsReadsFinalizeAt(t *testing.T) {
	cases := []struct {
		name   string
		launch string
		want   int
		warned bool
	}{
		{name: "a percentage in range", launch: `{"finalizeAt":80}`, want: 80},
		{name: "the lower edge", launch: `{"finalizeAt":1}`, want: 1},
		{name: "the upper edge", launch: `{"finalizeAt":99}`, want: 99},
		{name: "zero is outside the range", launch: `{"finalizeAt":0}`, warned: true},
		{name: "a full window is outside the range", launch: `{"finalizeAt":100}`, warned: true},
		{name: "a negative number", launch: `{"finalizeAt":-5}`, warned: true},
		{name: "a string, even a numeric one", launch: `{"finalizeAt":"80"}`, warned: true},
		{name: "a fraction", launch: `{"finalizeAt":80.5}`, warned: true},
		{name: "not set at all", launch: `{"model":"opus"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, warns := parseParams(json.RawMessage(c.launch))
			if p.FinalizeAt != c.want {
				t.Errorf("finalizeAt read as %d, expected %d", p.FinalizeAt, c.want)
			}
			named := false
			for _, w := range warns {
				if strings.Contains(w, "finalizeAt") {
					named = true
				}
			}
			if named != c.warned {
				t.Errorf("warned about finalizeAt: %v, expected %v: %v", named, c.warned, warns)
			}
		})
	}

	p, _ := parseParams(json.RawMessage(`{"finalizeAt":150,"model":"opus"}`))
	if p.Model != "opus" {
		t.Errorf("a sound parameter was lost because of the threshold next to it: %+v", p)
	}
}

func TestChildEnvCarriesFinalizeAt(t *testing.T) {
	fakeProc(t)
	own := []string{"HOME=/home/u"}
	has := func(env []string, kv string) bool {
		for _, got := range env {
			if got == kv {
				return true
			}
		}
		return false
	}
	names := func(env []string) bool {
		for _, got := range env {
			if strings.HasPrefix(got, finalizeEnv+"=") {
				return true
			}
		}
		return false
	}
	// The substituted process table has no graphical session, and that
	// complaint is not the one this test is about.
	about := func(warns []string) []string {
		var out []string
		for _, w := range warns {
			if strings.Contains(w, finalizeEnv) {
				out = append(out, w)
			}
		}
		return out
	}

	env, warns := childEnv(own, Params{FinalizeAt: 80}, ":10", "", "")
	if !has(env, "AACP_FINALIZE_AT=80") {
		t.Errorf("the threshold did not reach the session environment: %v", env)
	}
	if len(about(warns)) != 0 {
		t.Errorf("a threshold on its own drew complaints: %v", warns)
	}

	env, _ = childEnv(own, Params{}, ":10", "", "")
	if names(env) {
		t.Errorf("no threshold was set, yet the variable is there: %v", env)
	}

	// The field is what the screen shows; a variable typed into the map with
	// another number loses to it, and the human is told so.
	env, warns = childEnv(own, Params{FinalizeAt: 80, Env: map[string]string{"AACP_FINALIZE_AT": "95"}}, ":10", "", "")
	if !has(env, "AACP_FINALIZE_AT=80") || has(env, "AACP_FINALIZE_AT=95") {
		t.Errorf("the variable from the map beat the threshold field: %v", env)
	}
	if said := about(warns); len(said) != 1 || !strings.Contains(said[0], "95") || !strings.Contains(said[0], "80") {
		t.Errorf("the losing variable was not named next to the winner: %v", warns)
	}

	env, warns = childEnv(own, Params{FinalizeAt: 80, Env: map[string]string{"AACP_FINALIZE_AT": "80"}}, ":10", "", "")
	if !has(env, "AACP_FINALIZE_AT=80") || len(about(warns)) != 0 {
		t.Errorf("the same number on both sides is not a conflict: %v %v", env, warns)
	}

	// Without the field the map alone still switches the guard on: that is
	// how a project set the threshold before the field existed.
	env, warns = childEnv(own, Params{Env: map[string]string{"AACP_FINALIZE_AT": "95"}}, ":10", "", "")
	if !has(env, "AACP_FINALIZE_AT=95") || len(about(warns)) != 0 {
		t.Errorf("the variable from the map did not pass through on its own: %v %v", env, warns)
	}
}
