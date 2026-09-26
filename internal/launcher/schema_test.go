package launcher

import (
	"encoding/json"
	"reflect"
	"testing"

	"aacpanel/internal/schema"
)

// sample returns a value of the parameter's kind the schema accepts.
func sample(t *testing.T, p schema.Param) any {
	t.Helper()
	switch p.Kind {
	case schema.KindEnum:
		return p.Options[len(p.Options)-1].Value
	case schema.KindModel:
		return "opus"
	case schema.KindBool:
		return true
	case schema.KindInt:
		return p.Max
	case schema.KindText:
		return "read the queue"
	case schema.KindKV:
		return map[string]string{"FOO": "bar"}
	case schema.KindTokens:
		return []string{"--verbose"}
	}
	t.Fatalf("parameter %s has a kind the test does not know: %s", p.Key, p.Kind)
	return nil
}

// The schema is the list of what a launch can say, and the launcher reads it
// whole: every key of it lands in the parameters of the launch, a key the
// host reads and a retired one pass without a word, and a key outside it is
// named — so a parameter added to the screens without a launcher behind it
// fails here, not on the host.
func TestTheLauncherTakesEveryKeyOfTheSchema(t *testing.T) {
	for _, p := range schema.Params() {
		raw, err := json.Marshal(map[string]any{p.Key: sample(t, p)})
		if err != nil {
			t.Fatal(err)
		}
		got, warns := parseParams(raw)
		if len(warns) != 0 {
			t.Errorf("%s: the launcher complained about a value the schema accepts: %v", p.Key, warns)
		}
		switch empty := reflect.DeepEqual(got, Params{}); {
		case p.Host && !empty:
			t.Errorf("%s is the host's, and the launch took it: %+v", p.Key, got)
		case !p.Host && empty:
			t.Errorf("%s: the launcher read the key and kept nothing of it", p.Key)
		}
	}
	for _, r := range schema.RetiredKeys() {
		if _, warns := parseParams(json.RawMessage(`{"` + r.Key + `":"x"}`)); len(warns) != 0 {
			t.Errorf("retired %s drew complaints: %v", r.Key, warns)
		}
	}
	if _, warns := parseParams(json.RawMessage(`{"colour":"red"}`)); len(warns) == 0 {
		t.Error("a key outside the schema passed in silence")
	}
}
