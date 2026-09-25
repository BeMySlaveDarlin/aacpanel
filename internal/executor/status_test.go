package executor

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"aacpanel/internal/action"
)

// A session on the stream says who it works as, the way claude named it at the
// handshake, and the version of claude its file names.
func TestStatusCarriesTheAccountOfTheHandshake(t *testing.T) {
	f := onTheStream(t, false)
	sessionFiles(t, fakeSession{pid: 5001, name: "demo", start: "5555", sid: streamSID, version: "2.1.282"})
	f.mu.Lock()
	f.state.Init = json.RawMessage(`{"models":[],"account":{"email":"a@b.c","organization":"a@b.c's Organization",` +
		`"subscriptionType":"max","apiProvider":"firstParty","tokenSource":"oauth"}}`)
	f.mu.Unlock()
	e, _ := newTest(t, "")

	got, err := e.Status(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	want := &action.Status{Transport: action.SwitchStream, Version: "2.1.282", Account: &action.Account{
		Email: "a@b.c", Organization: "a@b.c's Organization", Plan: "max", Provider: "firstParty"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the status is %+v %+v, expected %+v %+v", got, got.Account, want, want.Account)
	}
}

// A terminal has no handshake to read: it says its version and nothing of its account.
func TestATerminalSaysItsVersionOnly(t *testing.T) {
	procFS(t, fakeProc{pid: 5002, comm: "claude", ppid: 1, cwd: "/opt/x", start: "5556",
		args: []string{"claude", "-n", "term"}})
	sessionFiles(t, fakeSession{pid: 5002, name: "term", start: "5556", sid: "s-5002", version: "2.1.280"})
	e, _ := newTest(t, "")
	got, err := e.Status(context.Background(), "term")
	if err != nil || got.Transport != action.SwitchConsole || got.Version != "2.1.280" || got.Account != nil {
		t.Fatalf("a terminal answered %+v, %v", got, err)
	}
}
