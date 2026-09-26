package host

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func authOf(list []ContourState) map[string]string {
	out := map[string]string{}
	for _, c := range list {
		if c.Auth != "" {
			out[c.Name] = c.Auth
		}
	}
	return out
}

func TestContourAuthReadsThreeStates(t *testing.T) {
	r := readerWith(t, `{"at":1,"profiles":[
		{"name":"personal","auth":"builtin"},
		{"name":"work","auth":"token"},
		{"name":"acme","auth":"missing"}
	]}`)

	got := authOf(r.Contours())
	want := map[string]string{"personal": AuthBuiltin, "work": AuthToken, "acme": AuthMissing}
	for name, state := range want {
		if got[name] != state {
			t.Errorf("contour %s: %q, expected %q", name, got[name], state)
		}
	}
	if len(got) != len(want) {
		t.Errorf("%d contours parsed, expected %d: %v", len(got), len(want), got)
	}
}

func TestContoursCarryConfigDir(t *testing.T) {
	r := readerWith(t, `{"at":1,"profiles":[
		{"name":"personal","configDir":"/home/u/.claude","auth":"builtin"},
		{"name":"work","configDir":"/home/u/.claude-contours/work","auth":"token","hooks":"diverged"}
	]}`)

	got := r.Contours()
	if len(got) != 2 {
		t.Fatalf("%d contours parsed, expected 2: %+v", len(got), got)
	}
	if got[0].ConfigDir != "/home/u/.claude" || got[1].ConfigDir != "/home/u/.claude-contours/work" {
		t.Errorf("the contour config dirs did not make it through: %+v", got)
	}
	if got[1].Hooks != HooksDiverged {
		t.Errorf("the hooks state did not make it through: %+v", got[1])
	}
	if got[0].Hooks != "" {
		t.Errorf("the personal contour grew a hooks state out of nowhere: %+v", got[0])
	}
}

func TestContourAuthStaysSilentOnAnythingOdd(t *testing.T) {
	cases := map[string]string{
		"no block at all":             `{"at":1,"sessions":[]}`,
		"the block is empty":          `{"at":1,"profiles":[]}`,
		"the block is not a list":     `{"at":1,"profiles":{"personal":"builtin"}}`,
		"the snapshot does not parse": `{"at":1,`,
		"an unknown state":            `{"at":1,"profiles":[{"name":"work","auth":"expired"}]}`,
		"no state":                    `{"at":1,"profiles":[{"name":"work"}]}`,
		"a profile without a name":    `{"at":1,"profiles":[{"name":"","auth":"token"}]}`,
	}
	for what, body := range cases {
		t.Run(what, func(t *testing.T) {
			if got := authOf(readerWith(t, body).Contours()); len(got) != 0 {
				t.Errorf("parsed %v, expected nothing", got)
			}
		})
	}

	t.Run("neither a name nor a config dir", func(t *testing.T) {
		r := readerWith(t, `{"at":1,"profiles":[{"name":"","configDir":"","auth":"token"}]}`)
		if got := r.Contours(); len(got) != 0 {
			t.Errorf("parsed %+v, expected nothing", got)
		}
	})

	t.Run("no file", func(t *testing.T) {
		r := NewReader(filepath.Join(t.TempDir(), "missing.json"))
		if got := r.Contours(); len(got) != 0 {
			t.Errorf("with no snapshot it parsed %+v, expected nothing", got)
		}
	})
}

func TestContourAuthKeepsGoodRowsBesideBad(t *testing.T) {
	r := readerWith(t, `{"at":1,"profiles":[
		{"name":"work","auth":"nonsense"},
		{"name":"personal","auth":"builtin"}
	]}`)
	got := authOf(r.Contours())
	if got["personal"] != AuthBuiltin {
		t.Errorf("a good row is lost together with the bad one: %v", got)
	}
	if _, bad := got["work"]; bad {
		t.Errorf("an unknown state reached the panel: %v", got)
	}
}

func TestSnapshotKeepsContoursBlock(t *testing.T) {
	r := readerWith(t, `{"at":1,"profiles":[{"name":"work","auth":"token"}]}`)
	payload, err := r.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"profiles"`) || !strings.Contains(string(payload), `"work"`) {
		t.Errorf("the profiles block did not make it into the answer: %s", payload)
	}
}

func readerWith(t *testing.T, body string) *Reader {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return NewReader(path)
}

func TestModelCatalogIsCommonForHost(t *testing.T) {
	r := readerWith(t, `{"at":1,"models":{"at":10,"contours":[
		{"profile":"personal","state":"ok","at":9,"models":[
			{"id":"claude-opus-5","name":"Claude Opus 5","window":1000000,"output":128000},
			{"id":"claude-haiku-4-5-20251001","name":"Claude Haiku 4.5","window":200000}
		]},
		{"profile":"work","state":"unknown","error":"no-credentials"}
	]}}`)

	got := r.ModelCatalog()
	if got.State != CatalogOK || got.At != 9 {
		t.Fatalf("the state or the age of the catalog is read wrong: %#v", got)
	}
	if len(got.Models) != 2 {
		t.Fatalf("the models are read wrong: %#v", got.Models)
	}
	if got.Models[0].ID != "claude-opus-5" || got.Models[1].ID != "claude-haiku-4-5-20251001" {
		t.Errorf("the model order is shuffled: %#v", got.Models)
	}
	if got.Models[1].Window != 200_000 {
		t.Errorf("the Haiku window is read wrong: %d", got.Models[1].Window)
	}
}

func TestModelCatalogTakesFirstContourThatHasIt(t *testing.T) {
	r := readerWith(t, `{"at":1,"models":{"contours":[
		{"profile":"acme","state":"error","error":"http-401"},
		{"profile":"personal","state":"ok","at":9,"models":[
			{"id":"claude-opus-5","name":"Claude Opus 5","window":1000000}
		]},
		{"profile":"work","state":"unknown"}
	]}}`)

	got := r.ModelCatalog()
	if got.State != CatalogOK || len(got.Models) != 1 {
		t.Fatalf("the catalog is not taken from the contour that has read it: %#v", got)
	}
	if got.Error != "" {
		t.Errorf("a foreign failure reason stuck to the catalog that was read: %q", got.Error)
	}
}

func TestModelCatalogUnknownWhenNobodyHasIt(t *testing.T) {
	for name, body := range map[string]string{
		"nobody has credentials": `{"at":1,"models":{"contours":[
			{"profile":"personal","state":"unknown","error":"no-credentials"},
			{"profile":"work","state":"unknown","error":"no-credentials"}
		]}}`,
		"no state":                 `{"at":1,"models":{"contours":[{"profile":"work"}]}}`,
		"the state is unknown":     `{"at":1,"models":{"contours":[{"profile":"work","state":"fetching"}]}}`,
		"a contour without a name": `{"at":1,"models":{"contours":[{"profile":"","state":"ok","models":[{"id":"x"}]}]}}`,
		"an empty list under ok":   `{"at":1,"models":{"contours":[{"profile":"personal","state":"ok","models":[]}]}}`,
		"no block at all":          `{"at":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			got := readerWith(t, body).ModelCatalog()
			if got.State != CatalogUnknown || len(got.Models) != 0 {
				t.Errorf("the panel would show a made-up catalog: %#v", got)
			}
		})
	}
}

func TestModelCatalogKeepsReasonWhenNothingRead(t *testing.T) {
	r := readerWith(t, `{"at":1,"models":{"contours":[
		{"profile":"personal","state":"error","error":"http-401"}
	]}}`)
	if got := r.ModelCatalog(); got.Error != "http-401" {
		t.Errorf("the failure reason is lost: %#v", got)
	}
}

func TestCatalogErrorNeverCarriesToken(t *testing.T) {
	const token = "sk-ant-oat01-SECRET-subscription-token"

	for name, reason := range map[string]string{
		"the whole token":              token,
		"the token inside a message":   "http-401 Unauthorized: Bearer " + token,
		"latin only, still not a code": "sk-ant-oat01-aaaaaaaaaaaaaaaaaaaaaaaa",
	} {
		t.Run(name, func(t *testing.T) {
			r := readerWith(t, `{"at":1,"models":{"contours":[
				{"profile":"personal","state":"error","error":`+quote(reason)+`}
			]}}`)
			got := r.ModelCatalog()
			if got.Error != "" {
				t.Errorf("a reason that does not look like a code went out: %q", got.Error)
			}
			if strings.Contains(got.Error, "sk-ant") {
				t.Errorf("the subscription token went out in the endpoint answer: %q", got.Error)
			}
		})
	}

	r := readerWith(t, `{"at":1,"models":{"contours":[
		{"profile":"personal","state":"error","error":"http-401"}
	]}}`)
	if got := r.ModelCatalog(); got.Error != "http-401" {
		t.Errorf("the failure code is lost: %#v", got)
	}
}

func quote(s string) string {
	raw, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

func TestSnapshotKeepsWaitingReason(t *testing.T) {
	r := readerWith(t, `{"at":1,"sessions":[
		{"session":"shop","status":"waiting","waitingFor":"dialog open"}
	]}`)
	payload, err := r.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var snapshot struct {
		Sessions []struct {
			Name       string `json:"session"`
			Status     string `json:"status"`
			WaitingFor string `json:"waitingFor"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Sessions) != 1 {
		t.Fatalf("the sessions block did not make it through: %s", payload)
	}
	if got := snapshot.Sessions[0].WaitingFor; got != "dialog open" {
		t.Errorf("the waiting reason is lost or translated on the way: %q (%s)", got, payload)
	}
}

// What an account starts a session with and whether it runs the context guard
// come through as the collector read them, and stay unknown where it did not.
func TestContoursCarryTheAccountAndTheGuard(t *testing.T) {
	r := readerWith(t, `{"at":1,"profiles":[
		{"name":"personal","configDir":"/home/u/.claude","auth":"builtin",
		 "account":{"model":"opus[1m]","effort":"xhigh","permissionMode":"auto"},"contextGuard":true},
		{"name":"work","configDir":"/home/u/.claude-contours/work","auth":"token"}
	]}`)
	got := r.Contours()
	if len(got) != 2 {
		t.Fatalf("%d contours parsed: %+v", len(got), got)
	}
	if got[0].Account["model"] != "opus[1m]" || got[0].Account["permissionMode"] != "auto" ||
		got[0].ContextGuard == nil || !*got[0].ContextGuard {
		t.Errorf("the account or the guard of personal did not come through: %+v", got[0])
	}
	if got[1].Account != nil || got[1].ContextGuard != nil {
		t.Errorf("a contour whose settings were not read got an account or a guard: %+v", got[1])
	}
}
