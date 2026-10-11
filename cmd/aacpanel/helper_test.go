package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"aacpanel/internal/action"
	"aacpanel/internal/auth"
)

// The opening conversation survives even when the map groups the new thread
// in another project, and a child close needs no collector round-trip.
func TestTheHelperChannelCrossesThePanelWithoutLosingItsCallerPG(t *testing.T) {
	srv, root := hostServer(t, `{"at":1}`)
	fillMap(t, srv, root)
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "started"})
	srv.exec, srv.auth = client, &auth.Service{}
	options := `"agent":"codex","openedBy":"` + closeThread + `","codex":{"model":"gpt-6.1-sol",` +
		`"effort":"high","sandbox":"workspace-write","writableRoots":["/srv/cache"]}`
	body := `{"kind":"session.open","params":{"path":"` + filepath.Join(root, "aacpanel") + `",` + options + `}}`
	if w := post(t, srv, body); w.Code != http.StatusOK {
		t.Fatalf("open: %d %s", w.Code, w.Body.String())
	}
	got := <-fake.got
	if got.Project.OpenedBy != closeThread || got.Project.Codex == nil || got.Project.Codex.Effort != "high" ||
		got.Project.Codex.Model != "gpt-6.1-sol" || len(got.Project.Codex.WritableRoots) != 1 {
		t.Fatalf("the executor lost the helper's caller or choices: %+v", got.Project)
	}
	var launch map[string]any
	if err := json.Unmarshal(got.Project.Launch, &launch); err != nil || launch["agent"] != "codex" {
		t.Fatalf("the map's launch lost its agent: %s", got.Project.Launch)
	}
	for _, old := range []string{`"high"`, `"workspace-write"`} {
		w := post(t, srv, strings.Replace(body, old, `"unknown"`, 1))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("an unknown helper choice was accepted: %d %s", w.Code, w.Body.String())
		}
	}
	for _, caller := range []string{"", "not-a-conversation"} {
		w := post(t, srv, `{"kind":"session.close","target":"codex-beef0001","params":{"caller":"`+caller+`"}}`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("an invalid caller was accepted: %d %s", w.Code, w.Body.String())
		}
	}
	if w := post(t, srv, `{"kind":"session.close","target":"codex-beef0001","params":{"caller":"`+closeThread+`"}}`); w.Code != http.StatusOK {
		t.Fatalf("close before a snapshot: %d %s", w.Code, w.Body.String())
	}
	got = <-fake.got
	if got.Caller != closeThread || got.Target != "codex-beef0001" || got.Conversation != closeThread {
		t.Fatalf("the child close lost its scope: %+v", got)
	}
}
