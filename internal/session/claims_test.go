package session

import (
	"net/http"
	"strings"
	"testing"
)

func TestClaimToolValidatesAndPassesItsMetadata(t *testing.T) {
	id := strings.Repeat("a", 32)
	p, url := startPanel(t, http.StatusOK, `{"ok":true,"detail":"accepted","claim":{"id":"`+id+`","kind":"claim","state":"open"}}`)
	h := hostWith(t, machine, url)
	for _, args := range []map[string]any{
		{"to": "shop", "text": "Work", "expects": "reply", "re": id},
		{"to": "shop", "text": "Work", "due": 1},
		{"to": "shop", "text": "Work", "outcome": "done"},
		{"to": "shop", "text": "Work", "expects": "done", "due": 1441},
		{"to": "shop", "text": "Work", "unknown": true},
	} {
		if _, failed := call(t, Letter(h), bound(mine), args); !failed {
			t.Errorf("invalid claim accepted: %v", args)
		}
	}
	if len(p.calls()) != 0 {
		t.Fatal("invalid metadata reached the panel")
	}
	said, failed := call(t, Letter(h), bound(mine), map[string]any{"to": "shop", "text": "Work", "expects": "done", "due": 2, "subject": "Ship"})
	if failed || !strings.HasPrefix(said, "Sent as claim "+id+": accepted.") {
		t.Fatalf("claim receipt: %q %v", said, failed)
	}
	params := p.calls()[0]["params"].(map[string]any)
	if params["expects"] != "done" || params["due"] != float64(2) || params["subject"] != "Ship" {
		t.Fatalf("metadata lost: %v", params)
	}
	_, failed = call(t, CodexLetter(h), codexBound, map[string]any{"re": id, "outcome": "done", "text": "Ready"})
	if failed {
		t.Fatal("a claim reply without to was refused")
	}
	asked := p.calls()[1]
	params = asked["params"].(map[string]any)
	if asked["target"] != "" || params["re"] != id || params["outcome"] != "done" || params["fromCodex"] == nil {
		t.Fatalf("reply: %v", asked)
	}
	properties := letterSchema()["properties"].(map[string]any)
	for _, key := range []string{"expects", "due", "subject", "re", "outcome"} {
		if properties[key] == nil {
			t.Errorf("schema lacks %s", key)
		}
	}
}
