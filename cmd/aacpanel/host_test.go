package main

import (
	"testing"
)

// The token totals of a live session are the collector's figures: the service
// passes the row along as it came, and the screen reads them by name.
func TestHostSessionsKeepTheTokenTotals(t *testing.T) {
	srv := &Server{host: hostWith(t, `{"at":1,"sessions":[
		{"session":"shop","sessionId":"b2","cwd":"/opt/x","tokens":598897,
		 "tokensIn":48211034,"tokensOut":612903}
	]}`)}

	rows := hostSessions(t, srv)
	row, ok := rows[0].(map[string]any)
	if !ok {
		t.Fatalf("a session is not an object: %v", rows[0])
	}
	if got := row["tokensIn"]; got != float64(48211034) {
		t.Errorf("the input total is lost or changed on the way: %v", got)
	}
	if got := row["tokensOut"]; got != float64(612903) {
		t.Errorf("the output total is lost or changed on the way: %v", got)
	}
}
