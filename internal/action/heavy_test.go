package action

import (
	"context"
	"reflect"
	"testing"

	"aacpanel/internal/heavy"
)

type heavyExecutor struct {
	view *heavy.View
}

func (e heavyExecutor) Execute(context.Context, Request) (string, error) {
	panic("a queue read must not execute an action")
}

func (e heavyExecutor) Heavy(context.Context) (*heavy.View, error) { return e.view, nil }

func TestHeavyViewTravelsThroughTheActionSocket(t *testing.T) {
	want := &heavy.View{Slots: 3, Running: []heavy.RunningView{{Unit: "aacp-heavy-1.scope", Session: "lab", Name: "check", Ask: 1024, Taken: 512}},
		Waiting: []heavy.WaitingView{{Place: 1, Session: "other", Name: "build", Ask: 2048, Since: 123}}, Head: "a slot"}
	client := serve(t, heavyExecutor{view: want})
	got, err := client.Heavy(t.Context())
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("the service receives %+v, %v; want %+v", got, err, want)
	}
	if _, err := serve(t, okExecutor("done")).Heavy(t.Context()); err == nil {
		t.Error("an executor with no queue claims to have answered")
	}
	if _, err := serve(t, heavyExecutor{}).Heavy(t.Context()); err == nil {
		t.Error("an answer without a queue claims to have answered")
	}
	for _, req := range []Request{{Ask: AskHeavy, Target: "lab"}, {Ask: AskHeavy, Kind: SessionKill}} {
		if err := req.Validate(); err == nil {
			t.Errorf("a queue question accepts %+v", req)
		}
	}
}
