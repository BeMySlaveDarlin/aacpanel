package action

import (
	"context"
	"testing"

	"aacpanel/internal/heavy"
)

type headroomExecutor struct{ ExecutorFunc }

func (headroomExecutor) Headroom(context.Context) (*Headroom, error) {
	return &Headroom{BootID: "host-boot", High: 100, Measure: heavy.Measure{Available: 90, Slice: 50, MemoryFull: 2}}, nil
}

func TestHeadroomIsAReadOnlyQuestion(t *testing.T) {
	s := NewServer("", headroomExecutor{}, 0)
	got := s.handle(t.Context(), Request{Ask: AskHeadroom})
	if !got.OK || got.Headroom == nil || got.Headroom.BootID != "host-boot" || got.Headroom.High != 100 || got.Headroom.Measure.Slice != 50 {
		t.Fatalf("host headroom: %+v", got)
	}
	for _, req := range []Request{{Ask: AskHeadroom, Target: "session"}, {Ask: AskHeadroom, Kind: SessionOpen}} {
		if err := req.Validate(); err == nil {
			t.Fatalf("a headroom read accepted an action or target: %+v", req)
		}
	}
	if resp := NewServer("", ExecutorFunc(nil), 0).handle(t.Context(), Request{Ask: AskHeadroom}); resp.OK {
		t.Fatal("an executor without host measurements claimed to answer")
	}
}

func TestSilentResumeAcceptsOnlyAnIdleClaudeStart(t *testing.T) {
	const id = "11111111-1111-4111-8111-111111111111"
	valid := Request{ID: "silent", Kind: SessionResume, Target: "lab", Resume: id, Silent: true}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Request){func(r *Request) { r.Kind = SessionRestart }, func(r *Request) { r.Agent = ResumeCodex; r.Contour = "work" }, func(r *Request) { r.Text = "work" }} {
		r := valid
		change(&r)
		if err := r.Validate(); err == nil {
			t.Fatalf("a silent start accepted %+v", r)
		}
	}
}
