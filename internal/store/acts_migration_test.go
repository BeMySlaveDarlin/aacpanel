package store

import (
	"fmt"
	"testing"

	"aacpanel/deploy/migrations"
	"aacpanel/internal/act"
)

func TestActsMigrationPreservesEveryOutstandingTurnPG(t *testing.T) {
	s := requestStore(t)
	pool, err := s.Pool()
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	// A private schema exercises the data migration against the old tables,
	// without rewinding the package database or disturbing other store tests.
	if _, err := tx.Exec(t.Context(), `CREATE SCHEMA acts_migration; SET LOCAL search_path=acts_migration; SET LOCAL timezone='UTC'`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"056_session_calls.sql", "057_session_requests.sql", "059_text_questions.sql", "061_text_question_what.sql", "062_text_question_answers.sql", "063_parent_questions.sql"} {
		body, err := migrations.FS.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(t.Context(), string(body)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := tx.Exec(t.Context(), `
 INSERT INTO session_requests(id,conversation,session,title,steps,blocking,created_at,answered_at,withdrawn_at,answer,note) VALUES
 (repeat('1',32),'requests','work','Open','Run',true,'2026-01-01',NULL,NULL,'',''),
 (repeat('2',32),'requests','work','Done','Run',false,'2026-01-01','2026-01-02',NULL,'done','Worked'),
 (repeat('3',32),'requests','work','Cannot','Run',false,'2026-01-01','2026-01-02',NULL,'cant','No access'),
 (repeat('4',32),'requests','work','Withdrawn','Run',false,'2026-01-01',NULL,'2026-01-02','','');
 INSERT INTO session_calls VALUES ('call-open','work','2026-01-01',NULL),('call-seen','work','2026-01-01','2026-01-02');
 INSERT INTO text_questions(conversation,message,verdict,what,closed,answered,started_at,answers) VALUES
 ('questions','open','yes','Publish?',false,false,'2026-01-01',ARRAY['Yes','No']),
 ('questions','answered','yes','Publish?',true,true,'2026-01-01','{}'),
 ('questions','closed','yes','Publish?',true,false,NULL,'{}'),
 ('questions','no','no','',false,false,NULL,'{}'),
 ('questions','pending','pending','',false,false,NULL,'{}'),
 ('questions','error','error','',false,false,NULL,'{}');`); err != nil {
		t.Fatal(err)
	}
	body, err := migrations.FS.ReadFile("073_acts.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(t.Context(), string(body)); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(t.Context(), `SELECT `+actColumns+` FROM acts ORDER BY kind,title`)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]act.Record{}
	for rows.Next() {
		row, err := readAct(rows)
		if err != nil {
			t.Fatal(err)
		}
		key := row.Kind + "/" + row.Title
		if row.Kind == "call" {
			key = row.Conversation
		}
		if row.Kind == "ask" {
			key = row.Body.Message
		}
		got[key] = row
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{"do/Open": "open", "do/Done": "answered", "do/Cannot": "declined", "do/Withdrawn": "withdrawn", "call-open": "open", "call-seen": "answered", "open": "open", "answered": "answered", "closed": "withdrawn"}
	if len(got) != len(expected) {
		t.Fatalf("migrated: %+v", got)
	}
	for key, state := range expected {
		row := got[key]
		if row.State != state || row.Open() != (row.Closed == nil) {
			t.Errorf("%s: %+v", key, row)
		}
		if row.Kind == "do" && row.Body.Steps != "Run" {
			t.Errorf("steps lost: %+v", row)
		}
		if row.Kind == "ask" && (row.Body.Source != "prose" || row.Body.What != "Publish?") {
			t.Errorf("question body lost: %+v", row)
		}
	}
	if row := got["do/Cannot"]; row.Answer == nil || row.Answer.Outcome != "cant" || row.Answer.Note != "No access" || row.AnsweredBy != "person" {
		t.Errorf("answer lost: %+v", row)
	}
	if row := got["call-seen"]; row.Closed.Format("2006-01-02") != "2026-01-02" {
		t.Errorf("seen time lost: %+v", row)
	}
	for _, row := range got {
		var expectedID string
		if row.Kind == "ask" {
			err = tx.QueryRow(t.Context(), `SELECT md5($1||$2)`, row.Conversation, row.Body.Message).Scan(&expectedID)
		} else if row.Kind == "call" {
			err = tx.QueryRow(t.Context(), `SELECT md5(conversation||called_at::text) FROM session_calls WHERE conversation=$1`, row.Conversation).Scan(&expectedID)
		} else {
			expectedID = row.ID
		}
		if err != nil || row.ID != expectedID {
			t.Errorf("id: %+v want %s (%v)", row, expectedID, err)
		}
	}
	for table, want := range map[string]int{"session_requests": 4, "session_calls": 2, "text_questions": 6} {
		var count int
		if err := tx.QueryRow(t.Context(), fmt.Sprintf("SELECT count(*) FROM %s", table)).Scan(&count); err != nil || count != want {
			t.Errorf("old table %s: %d %v", table, count, err)
		}
	}
}
