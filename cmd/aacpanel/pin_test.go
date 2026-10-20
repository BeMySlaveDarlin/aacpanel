package main

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"aacpanel/internal/action"
	"aacpanel/internal/auth"
	"aacpanel/internal/host"
)

func TestAPinGoesToTheLiveRootOfTheGroup(t *testing.T) {
	client, fake := startFakeExec(t, action.Response{OK: true, Detail: "ok"})
	srv := &Server{auth: &auth.Service{}, exec: client, host: host.NewReader(snapshotWith(t,
		`{"at":1,"sessions":[{"session":"lead"},{"session":"child","parent":{"session":"lead"}},`+
			`{"session":"grandchild","parent":{"session":"child"}},{"session":"orphan","parent":{"session":"gone"}},`+
			`{"session":"outside","outside":true},{"session":"loop","parent":{"session":"loop"}}]}`))}
	for _, target := range []string{"lead", "child", "grandchild", "orphan"} {
		for _, on := range []bool{true, false} {
			body := fmt.Sprintf(`{"kind":"session.pin","target":%q,"params":{"on":%v}}`, target, on)
			if w := post(t, srv, body); w.Code != http.StatusOK {
				t.Fatalf("pinning %s: %d %s", target, w.Code, w.Body.String())
			}
			want := "lead"
			if target == "orphan" {
				want = target
			}
			select {
			case got := <-fake.got:
				if got.Kind != action.SessionPin || got.Target != want || got.Pin == nil || *got.Pin != on {
					t.Fatalf("pinning %s reached the executor as %+v", target, got)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("the executor got no pin")
			}
		}
	}
	for _, body := range []string{
		`{"kind":"session.pin","target":"lead","params":{}}`,
		`{"kind":"session.pin","target":"lead","params":{"on":"true"}}`,
		`{"kind":"session.pin","target":"gone","params":{"on":true}}`,
		`{"kind":"session.pin","target":"outside","params":{"on":true}}`,
		`{"kind":"session.pin","target":"loop","params":{"on":true}}`,
	} {
		if w := post(t, srv, body); w.Code != http.StatusBadRequest {
			t.Errorf("invalid pin passed: %s: %d %s", body, w.Code, w.Body.String())
		}
	}
}
