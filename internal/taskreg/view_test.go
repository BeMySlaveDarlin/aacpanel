package taskreg

import (
	"reflect"
	"testing"
	"time"
)

func TestIssueNumbersShareOneRuleAcrossSources(t *testing.T) {
	got := IssueNumbers("SER-47, ser-47, LMS-13692", "evirma-ser1063", "algo-lms13692", "work SER_42", "PROJ-2", "c22ed268")
	want := []string{"LMS-13692", "PROJ-2", "SER-1063", "SER-42", "SER-47"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("issue keys: %v, want %v", got, want)
	}
	task, err := ParseTask(`{"id":1,"title":"Work","status":"open","tags":["ser-47","SER-549"],"refs":{"jira":"LMS-42"},"external":{"url":"https://issues.example/SER-1"},"body":"Related to SER-99."}`)
	if err != nil || !reflect.DeepEqual(task.ExternalRefs, []string{"LMS-42", "SER-1", "SER-47", "SER-549"}) {
		t.Fatalf("task sources: %+v, %v", task, err)
	}
}

func TestInWorkUsesRecentEventsOrLiveOwnership(t *testing.T) {
	now := time.Date(2025, 1, 8, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-72 * time.Hour)
	old := recent.Add(-time.Second)
	future := now.Add(time.Hour)
	sessions := []Session{{ID: "11111111-full", Name: "worker", Live: true, Status: "busy"}, {ID: "22222222-full", Name: "evirma-ser1063", Live: true, Status: "busy"}}
	for _, tc := range []struct {
		name string
		task Task
		want bool
	}{
		{"recent boundary", Task{Status: "open", LastEventAt: &recent}, true},
		{"old", Task{Status: "open", LastEventAt: &old}, false},
		{"future", Task{Status: "open", LastEventAt: &future}, false},
		{"held", Task{Status: "open", LastEventAt: &old, Session: "11111111"}, true},
		{"historic participant", Task{Status: "open", LastEventAt: &old, TouchedBy: []string{"11111111"}}, false},
		{"shared issue", Task{Status: "open", ExternalRefs: []string{"SER-1063"}}, true},
		{"deferred", Task{Status: "deferred", LastEventAt: &recent, Session: "11111111"}, false},
		{"done", Task{Status: "done", LastEventAt: &recent, Session: "11111111"}, false},
		{"deleted", Task{Status: "open", LastEventAt: &recent, DeletedAt: &old, Session: "11111111"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			Enrich(&tc.task, sessions, now)
			if tc.task.InWork != tc.want {
				t.Fatalf("in work=%v, want %v", tc.task.InWork, tc.want)
			}
		})
	}
}

func TestPeopleFoldConversationsAndUnfiledIssuesRemainVisible(t *testing.T) {
	sessions := []Session{{ID: "11111111-first", Name: "evirma-ser1063"}, {ID: "22222222-second", Name: "evirma-ser1063", Live: true, Status: "busy"}, {ID: "33333333-third", Name: "evirma-ser1064", Live: true, Status: "idle"}}
	task := Task{Status: "open", TouchedBy: []string{"11111111", "22222222"}, LastActor: "22222222"}
	Enrich(&task, sessions, time.Now())
	if !reflect.DeepEqual(task.ExternalRefs, []string{"SER-1063"}) || len(task.People) != 1 || len(task.People[0].Conversations) != 2 || !task.People[0].Live || task.LastActorName != "evirma-ser1063" {
		t.Fatalf("participants: %+v", task)
	}
	unfiled := WithoutTasks([]Task{task}, sessions)
	if len(unfiled) != 1 || unfiled[0].External != "SER-1064" || unfiled[0].People[0].Name != "evirma-ser1064" {
		t.Fatalf("unfiled issues: %+v", unfiled)
	}
	unknown := People([]string{"missing-id"}, sessions)
	if len(unknown) != 1 || unknown[0].Known || unknown[0].Name != "missing-id" {
		t.Fatalf("an unknown name was invented: %+v", unknown)
	}
}
