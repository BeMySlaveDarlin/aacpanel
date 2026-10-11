package contract

import (
	"strings"
	"testing"
)

func TestCommandsAvoidTheRecordedSleepRefusal(t *testing.T) {
	blocked := toolResults(sample(t, "sleep-blocked.jsonl"))
	if !strings.Contains(blocked, "Blocked: sleep 40 followed by: echo woke") {
		t.Fatal("the sample must show the leading-sleep refusal", blocked)
	}
	for _, tc := range []struct {
		name, command string
		check         func(*session)
	}{
		{"interrupt", "python3 -c 'import time; time.sleep(40)' && echo woke", func(s *session) { checkInterrupt(s) }},
		{"queue", "python3 -c 'import time; time.sleep(12)' && echo woke", func(s *session) { checkQueue(s) }},
		{"background", "tail -f /dev/null", func(s *session) { checkBackground(s) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A closed recording lets us inspect the actual request without
			// starting its command or waiting through its lifetime.
			s, input := replaySession(t, "")
			tc.check(s)
			prompt := one(t, input.String()).obj("message").text("content")
			_, command, ok := strings.Cut(prompt, "run exactly: ")
			if !ok || !strings.HasPrefix(command, tc.command+" —") {
				t.Fatal("the probe changed its wait command", prompt)
			}
			if tc.name == "background" && !strings.Contains(prompt, "run_in_background set to true") {
				t.Fatal("the unbounded task must run in the background", prompt)
			}
		})
	}
}

func TestBackgroundExpiryClearsTheLastTaskList(t *testing.T) {
	recorded := sample(t, "background-expired.jsonl")
	// The live task was listed and outlived its first turn. Its completion
	// then emptied the list while the letter check was still waiting.
	want(t, backgroundVerdict("background-task", recorded[:3]), OK)
	want(t, backgroundVerdict("background-task", recorded), Fail)
	if got := backgroundVerdict("background-task", recorded).Detail; got != "listed false, ended before it was stopped true" {
		t.Fatal(got)
	}
	ack := one(t, `{"subtype":"success","response":{}}`)
	want(t, stopVerdict("background-task", ack, nil), Fail)
}
