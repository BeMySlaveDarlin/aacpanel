package session

import (
	"net/http"
	"strings"
	"testing"
)

func TestTheHelperToolsCarryTheCallerAndOneStartOptions(t *testing.T) {
	p, url := startPanel(t, http.StatusOK, `{"ok":true,"detail":"done"}`)
	h := hostWith(t, "", url)
	said, failed := call(t, Open(h), boundAs("lead", mine), map[string]any{
		"dir": "/srv/proj", "agent": "codex", "model": "gpt-6.1-sol", "effort": "high",
		"sandbox": "workspace-write", "writableRoots": []string{"/srv/cache"},
	})
	if failed || !strings.Contains(said, "immediately") {
		t.Fatalf("open: %s", said)
	}
	params := p.calls()[0]["params"].(map[string]any)
	c := params["codex"].(map[string]any)
	if params["openedBy"] != mine || params["parent"] != "lead" || c["model"] != "gpt-6.1-sol" ||
		c["effort"] != "high" || c["sandbox"] != "workspace-write" || len(c["writableRoots"].([]any)) != 1 {
		t.Fatalf("open lost the caller or launch options: %v", params)
	}
	said, failed = call(t, Close(h), boundAs("lead", mine), map[string]any{"name": "codex-beef0001"})
	if failed || strings.Contains(said, "End the turn") {
		t.Fatalf("child close ended the parent: %s", said)
	}
	asked := p.calls()[1]
	params = asked["params"].(map[string]any)
	if asked["target"] != "codex-beef0001" || params["caller"] != mine || params["conversation"] != nil {
		t.Fatalf("child close lost the caller or closes itself: %v", asked)
	}
	for args, reason := range map[string]string{
		`{"dir":"/srv/proj","agent":"codex","effort":"mystery"}`:          "effort",
		`{"dir":"/srv/proj","agent":"codex","sandbox":"full"}`:            "sandbox",
		`{"dir":"/srv/proj","agent":"codex","writableRoots":["~/cache"]}`: "absolute",
		`{"dir":"/srv/proj","agent":"claude","model":"gpt-test"}`:         "Codex options",
		`{"dir":"/srv/proj","message":"a task"}`:                          "not the tool's",
	} {
		said, failed := Open(h).Call(t.Context(), boundAs("lead", mine), []byte(args))
		if !failed || !strings.Contains(said, reason) {
			t.Errorf("%s: %s", args, said)
		}
	}
	if len(p.calls()) != 2 {
		t.Fatal("invalid options reached the panel")
	}
}
