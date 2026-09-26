package stream

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// What a handshake says of the models is kept by resolved id and laid over
// what earlier handshakes said: an account that offers fewer models does not
// take the others' away, and a model it does offer is said as it says now.
func TestTraitsAreKeptAcrossHandshakes(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	first := json.RawMessage(`{"models":[
		{"value":"opus","resolvedModel":"claude-opus-5-5","supportsEffort":true,
		 "supportedEffortLevels":["low","medium","high","xhigh","max"],"supportsAutoMode":true},
		{"value":"haiku","resolvedModel":"claude-haiku-4-5-20251001"}]}`)
	if err := keepTraits(first, time.Unix(100, 0)); err != nil {
		t.Fatal(err)
	}
	second := json.RawMessage(`{"models":[
		{"value":"sonnet","resolvedModel":"claude-sonnet-4-6","supportsEffort":true,
		 "supportedEffortLevels":["low","medium","high"]}]}`)
	if err := keepTraits(second, time.Unix(200, 0)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(TraitsPath())
	if err != nil {
		t.Fatal(err)
	}
	var got Traits
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.At != 200 || len(got.Models) != 3 {
		t.Fatalf("the traits kept are %+v", got)
	}
	if o := got.Models["claude-opus-5-5"]; !o.Effort || len(o.Efforts) != 5 || !o.AutoMode {
		t.Errorf("opus is kept as %+v", o)
	}
	if h := got.Models["claude-haiku-4-5-20251001"]; h.Effort || h.AutoMode || len(h.Efforts) != 0 {
		t.Errorf("haiku, which takes no effort and no auto, is kept as %+v", h)
	}
	if s := got.Models["claude-sonnet-4-6"]; len(s.Efforts) != 3 || s.AutoMode {
		t.Errorf("sonnet 4.6, without xhigh and auto, is kept as %+v", s)
	}
	if err := keepTraits(json.RawMessage(`{"models":[]}`), time.Unix(300, 0)); err != nil {
		t.Fatal(err)
	}
	if raw2, _ := os.ReadFile(TraitsPath()); string(raw2) != string(raw) {
		t.Error("a handshake that named no models wiped what was known")
	}
}
