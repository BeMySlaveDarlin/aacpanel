package schema

import (
	"reflect"
	"testing"
)

func valueOf(t *testing.T, vs []Value, key string) Value {
	t.Helper()
	for _, v := range vs {
		if v.Key == key {
			return v
		}
	}
	t.Fatalf("no value for %s among %v", key, vs)
	return Value{}
}

// Every parameter gets one value with the layer that gave it: the project
// over the contour over the account, and the schema's outcome where nobody
// says anything. A false a project stores is a value, not a silence: it is
// how a project turns off what its contour turns on.
func TestEffectiveNamesTheLayerOfEveryValue(t *testing.T) {
	account := map[string]any{"model": "opus[1m]", "effort": "xhigh", "permissionMode": "auto"}
	contour := map[string]any{"remoteControl": true, "effort": "high", "env": map[string]any{"A": "1", "B": "2"}}
	project := map[string]any{"remoteControl": false, "intent": "", "env": map[string]any{"B": "3"}}

	got := Effective(account, contour, project)
	if len(got) != len(Params()) {
		t.Fatalf("%d values for %d parameters", len(got), len(Params()))
	}
	for i, p := range Params() {
		if got[i].Key != p.Key {
			t.Errorf("value %d is %s, the schema's %s — the page shows them in the schema's order", i, got[i].Key, p.Key)
		}
	}
	cases := []struct {
		key   string
		value any
		layer string
	}{
		{"model", "opus[1m]", LayerAccount},
		{"effort", "high", LayerContour},
		{"permissionMode", "auto", LayerAccount},
		{"remoteControl", false, LayerProject},
		{"intent", "", LayerProject},
		{"transport", nil, LayerClaude},
		{"contextCap", CapDefault, LayerPanel},
		{"autoRestart", false, LayerPanel},
		{"restartIntent", RestartIntentDefault, LayerPanel},
	}
	for _, c := range cases {
		v := valueOf(t, got, c.key)
		if v.Value != c.value || v.Layer != c.layer {
			t.Errorf("%s is %v from %s — meant %v from %s", c.key, v.Value, v.Layer, c.value, c.layer)
		}
	}
	env := valueOf(t, got, "env")
	if !reflect.DeepEqual(env.Value, map[string]any{"A": "1", "B": "3"}) ||
		!reflect.DeepEqual(env.From, map[string]string{"A": LayerContour, "B": LayerProject}) {
		t.Errorf("env is %v from %v — merged key by key, each key with its own layer", env.Value, env.From)
	}
}

// What the launcher is given carries the contour and the project only:
// claude reads the account's settings itself, and a flag made of them would
// pin the project to what the account said on the day. The panel's defaults
// are not written into it either — the schema says them.
func TestLaunchLeavesTheAccountToClaude(t *testing.T) {
	got := Launch(map[string]any{"effort": "high", "room": "x"}, map[string]any{"remoteControl": false, "autoRestart": true})
	want := map[string]any{"effort": "high", "remoteControl": false, "autoRestart": true}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("launch %v, meant %v", got, want)
	}
}
