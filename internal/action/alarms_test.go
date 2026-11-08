package action

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type alarmExecutor struct{ ExecutorFunc }

func (alarmExecutor) SleepingAlarms(context.Context) (*SleepingAlarms, error) {
	return &SleepingAlarms{Alarms: []SleepingAlarm{{ID: "0123456789abcdef", Session: "lab",
		Conversation: "11111111-1111-4111-8111-111111111111", Next: time.Unix(100, 0)}}}, nil
}

func TestSleepingAlarmsAreAReadOnlyQuestion(t *testing.T) {
	s := NewServer("", alarmExecutor{}, 0)
	got := s.handle(t.Context(), Request{Ask: AskSleepingAlarms})
	if !got.OK || got.SleepingAlarms == nil || len(got.SleepingAlarms.Alarms) != 1 || got.SleepingAlarms.Alarms[0].ID != "0123456789abcdef" {
		t.Fatalf("sleeping alarms: %+v", got)
	}
	for _, req := range []Request{{Ask: AskSleepingAlarms, Target: "session"}, {Ask: AskSleepingAlarms, Kind: SessionClose}} {
		if err := req.Validate(); err == nil {
			t.Fatalf("a schedule read accepted an action or target: %+v", req)
		}
	}
	if resp := NewServer("", ExecutorFunc(nil), 0).handle(t.Context(), Request{Ask: AskSleepingAlarms}); resp.OK {
		t.Fatal("an executor without sleeping alarms claimed to answer")
	}
	raw, err := json.Marshal(Response{OK: true, SleepingAlarms: &SleepingAlarms{Alarms: []SleepingAlarm{}}})
	if err != nil || !strings.Contains(string(raw), `"alarms":[]`) {
		t.Fatalf("an empty schedule lost its shape: %s %v", raw, err)
	}
	client := serve(t, alarmExecutor{})
	alarms, err := client.SleepingAlarms(t.Context())
	if err != nil || len(alarms.Alarms) != 1 || !alarms.Alarms[0].Next.Equal(time.Unix(100, 0)) {
		t.Fatalf("the sleeping schedule did not cross the socket: %+v %v", alarms, err)
	}
}

func TestSleepMarksBindOnlyCloseAndAlarmCancellationToTheirConversation(t *testing.T) {
	const conversation = "11111111-1111-4111-8111-111111111111"
	for _, kind := range []Kind{SessionClose, AlarmCancel} {
		valid := Request{ID: "sleep", Kind: kind, Target: "lab", Conversation: conversation, Sleeping: true}
		if kind == AlarmCancel {
			valid.Work = &Work{ID: "0123456789abcdef"}
		}
		if err := valid.Validate(); err != nil {
			t.Fatal(err)
		}
		for _, change := range []func(*Request){
			func(r *Request) { r.Conversation = "" },
			func(r *Request) { r.Kind = SessionSend; r.Text = "hello" },
			func(r *Request) { r.Caller = conversation },
		} {
			r := valid
			change(&r)
			if err := r.Validate(); err == nil {
				t.Fatalf("an unbound sleep mark was accepted: %+v", r)
			}
		}
	}
}
