package store

import (
	"encoding/json"
	"fmt"
	"testing"

	"aacpanel/deploy/migrations"
)

func TestAskRetirementWithdrawsOnlyOpenAsksPG(t *testing.T) {
	s := requestStore(t)
	pool, _ := s.Pool()
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	if _, err := tx.Exec(t.Context(), `CREATE TEMP TABLE acts (LIKE acts INCLUDING ALL) ON COMMIT DROP`); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, kind := range []string{"ask", "do", "secret", "call", "claim"} {
		for _, state := range []string{"open", "failed", "answered", "declined", "withdrawn"} {
			n++
			if _, err := tx.Exec(t.Context(), `INSERT INTO acts(id,conversation,session,kind,title,state,closed_at,peer,body,answer,answered_by)
 VALUES ($1,'migration','work',$2,$3,$4,CASE WHEN $4 IN ('open','failed') THEN NULL ELSE '2026-01-01'::timestamptz END,
 CASE WHEN $2='claim' THEN 'peer' END,'{"source":"prose","questions":[{"question":"Publish?"}]}','{"note":"kept"}','lead')`, fmt.Sprintf("%032x", n), kind, kind+"/"+state, state); err != nil {
				t.Fatal(err)
			}
		}
	}
	read := func() map[string]string {
		t.Helper()
		rows, err := tx.Query(t.Context(), `SELECT title,to_jsonb(acts)::text FROM acts ORDER BY id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[string]string{}
		for rows.Next() {
			var title, body string
			if err := rows.Scan(&title, &body); err != nil {
				t.Fatal(err)
			}
			out[title] = body
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if _, err := tx.Exec(t.Context(), `INSERT INTO acts(id,conversation,session,kind,title,state,body)
 VALUES (repeat('f',32),'migration','work','ask','ask/tool','open','{"source":"tool","questions":[{"question":"Publish?"}]}')`); err != nil {
		t.Fatal(err)
	}
	before := read()
	migration, err := migrations.FS.ReadFile("077_withdraw_asks.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(t.Context(), string(migration)); err != nil {
		t.Fatal(err)
	}
	after := read()
	if len(after) != len(before) {
		t.Fatalf("migration lost acts: %d, want %d", len(after), len(before))
	}
	for title, body := range after {
		if title != "ask/open" && title != "ask/failed" && title != "ask/tool" {
			if body != before[title] {
				t.Errorf("%s changed: %s", title, body)
			}
			continue
		}
		var got, want map[string]any
		if err := json.Unmarshal([]byte(body), &got); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(before[title]), &want); err != nil {
			t.Fatal(err)
		}
		if got["state"] != "withdrawn" || got["closed_at"] == nil {
			t.Errorf("open ask survived: %s", body)
		}
		got["state"], got["closed_at"] = want["state"], want["closed_at"]
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(want)
		if string(a) != string(b) {
			t.Errorf("ask history changed: %s", body)
		}
	}
}
