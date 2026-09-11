package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSplitLogTimeTakesDockerStampOnly(t *testing.T) {
	cases := []struct {
		name string
		in   string
		at   string
		text string
	}{
		{
			name: "the docker stamp is split off",
			in:   "2026-09-06T09:41:12.123456789Z brought the listener up on :8776",
			at:   "2026-09-06T09:41:12.123456789Z",
			text: "brought the listener up on :8776",
		},
		{
			name: "a stamp without fractions of a second",
			in:   "2026-09-06T09:41:12Z ready",
			at:   "2026-09-06T09:41:12Z",
			text: "ready",
		},
		{
			name: "lines without a stamp are left alone",
			in:   "GET /api/host 200 3ms",
			at:   "",
			text: "GET /api/host 200 3ms",
		},
		{
			name: "a word that looks like a date is not a stamp",
			in:   "2026-09-06 09:41:12 INFO start",
			at:   "",
			text: "2026-09-06 09:41:12 INFO start",
		},
		{
			name: "a stamp without a message",
			in:   "2026-09-06T09:41:12.000000000Z",
			at:   "2026-09-06T09:41:12.000000000Z",
			text: "",
		},
		{
			name: "an empty line",
			in:   "",
			at:   "",
			text: "",
		},
		{
			name: "the indent of the message is kept",
			in:   "2026-09-06T09:41:12.000000000Z     at foo.bar (x.js:12)",
			at:   "2026-09-06T09:41:12.000000000Z",
			text: "    at foo.bar (x.js:12)",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			at, text := splitLogTime(c.in)
			if at != c.at {
				t.Errorf("the stamp: got %q, expected %q", at, c.at)
			}
			if text != c.text {
				t.Errorf("the line: got %q, expected %q", text, c.text)
			}
		})
	}
}

func TestLogEventCarriesTimeToThePage(t *testing.T) {
	at, text := splitLogTime("2026-09-06T09:41:12.123456789Z brought the listener up")
	raw, err := json.Marshal(logEvent{Stream: "stdout", Line: text, At: at})
	if err != nil {
		t.Fatalf("assembling the event: %v", err)
	}
	body := string(raw)
	if !strings.Contains(body, `"at":"2026-09-06T09:41:12.123456789Z"`) {
		t.Errorf("the event has no full timestamp: %s", body)
	}
	if strings.Contains(body, `"at":"09:41:12"`) {
		t.Errorf("the server trimmed the stamp down to the clock — the timezone of the browser is lost with it: %s", body)
	}

	at, text = splitLogTime("GET /api/host 200 3ms")
	raw, err = json.Marshal(logEvent{Stream: "stdout", Line: text, At: at})
	if err != nil {
		t.Fatalf("assembling the event: %v", err)
	}
	if strings.Contains(string(raw), `"at"`) {
		t.Errorf("a line without a stamp still has at in its event: %s", raw)
	}
}
