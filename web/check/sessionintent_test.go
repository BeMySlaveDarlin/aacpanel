package check

import (
	"slices"
	"strings"
	"testing"
)

func TestSessionMessageSheetOnPhoneAndDesktop(t *testing.T) {
	for _, wide := range []bool{false, true} {
		name := "phone"
		if wide {
			name = "desktop"
		}
		t.Run(name, func(t *testing.T) {
			var got struct {
				Labels, Inherited, Sent                                 []string
				First, Restart, Cleared, Restored, FirstKept, ReadError string
				SameOff, LongOff, AstralOff, ErrorOff, Confirmation     bool
			}
			if wide {
				runWideFixture(t, "sessionintent.html", &got)
			} else {
				runFixture(t, "sessionintent.html", &got)
			}
			at := slices.Index(got.Labels, "Session messages…")
			if at < 1 || got.Labels[at-1] != "Role…" {
				t.Errorf("the messages do not stand beside Role: %v", got.Labels)
			}
			if got.First != "" || got.Restart != "Continue" || !got.SameOff {
				t.Errorf("the sheet did not open with its overrides: %+v", got)
			}
			if strings.Join(got.Inherited, "|") != "From contour: Read the queue|From project: Summary" {
				t.Errorf("the sheet lost the inherited values or sources: %v", got.Inherited)
			}
			if !got.LongOff || got.AstralOff || !got.ErrorOff || got.ReadError != "The messages could not be read" {
				t.Errorf("the sheet did not guard the length or failed read: %+v", got)
			}
			if got.Cleared != "" || got.Restored != "From project: Summary" || got.FirstKept != "Start with the plan" {
				t.Errorf("clearing the restart message lost inheritance or the first message: %+v", got)
			}
			want := []string{
				`session.intent:shop:{"intent":"Start with the plan"}`,
				`session.intent:shop:{"restartIntent":""}`,
			}
			if !slices.Equal(got.Sent, want) || got.Confirmation {
				t.Errorf("sent %v, confirmation %v; want %v", got.Sent, got.Confirmation, want)
			}
		})
	}
}
