package taskreg

import (
	"slices"
	"strings"
	"time"
)

type Session struct {
	ID        string `json:"sessionId"`
	Name      string `json:"name"`
	ProjectID int    `json:"projectId"`
	Live      bool   `json:"live"`
	Status    string `json:"status"`
}

type Person struct {
	Name          string   `json:"name"`
	Conversations []string `json:"conversations"`
	Live          bool     `json:"live"`
	Status        string   `json:"status"`
	Known         bool     `json:"known"`
}

type Unfiled struct {
	External string   `json:"external"`
	People   []Person `json:"people"`
}

func SameConversation(id, full string) bool {
	return id != "" && (id == full || (len(id) >= 8 && strings.HasPrefix(full, id)))
}

func ActorIDs(tasks []Task) []string {
	ids := []string{}
	for _, task := range tasks {
		for _, id := range append(append([]string{}, task.TouchedBy...), task.CreatedBy, task.ClosedBy, task.Session, task.LastActor) {
			if id != "" {
				ids = append(ids, id)
			}
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

func People(ids []string, sessions []Session) []Person {
	groups := map[string]Person{}
	for _, id := range ids {
		matched := false
		for _, session := range sessions {
			if !SameConversation(id, session.ID) {
				continue
			}
			matched = true
			person := groups[session.Name]
			person.Name = session.Name
			person.Known = true
			if person.Conversations == nil {
				person.Conversations = []string{}
			}
			if !slices.Contains(person.Conversations, session.ID) {
				person.Conversations = append(person.Conversations, session.ID)
			}
			if session.Live {
				person.Live = true
				person.Status = session.Status
			}
			groups[session.Name] = person
		}
		if !matched && id != "" {
			groups[id] = Person{Name: id, Conversations: []string{id}}
		}
	}
	out := make([]Person, 0, len(groups))
	for _, person := range groups {
		slices.Sort(person.Conversations)
		out = append(out, person)
	}
	slices.SortFunc(out, func(a, b Person) int {
		if a.Live != b.Live {
			if a.Live {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}

// Enrich uses only explicit ownership or a shared issue key to associate a
// live conversation. Historical participants do not keep a task in work.
func Enrich(task *Task, sessions []Session, now time.Time) {
	people := ActorIDs([]Task{*task})
	refs := append([]string{}, task.ExternalRefs...)
	for _, session := range sessions {
		for _, id := range people {
			if SameConversation(id, session.ID) {
				refs = append(refs, IssueNumbers(session.Name)...)
				break
			}
		}
	}
	slices.Sort(refs)
	task.ExternalRefs = slices.Compact(refs)
	task.InWork = task.Status == "open" && task.DeletedAt == nil && task.LastEventAt != nil && !task.LastEventAt.Before(now.Add(-72*time.Hour)) && !task.LastEventAt.After(now)
	for _, session := range sessions {
		if !session.Live {
			continue
		}
		holds := SameConversation(task.Session, session.ID)
		for _, ref := range IssueNumbers(session.Name) {
			holds = holds || slices.Contains(task.ExternalRefs, ref)
		}
		if holds && task.Status == "open" && task.DeletedAt == nil {
			task.InWork = true
			people = append(people, session.ID)
		}
	}
	task.People = People(people, sessions)
	task.LastActorName = ActorName(task.LastActor, sessions)
	if task.LastActorName == "" {
		task.LastActorName = ActorName(task.Session, sessions)
	}
}

func ActorName(id string, sessions []Session) string {
	for _, session := range sessions {
		if SameConversation(id, session.ID) {
			return session.Name
		}
	}
	return id
}

func WithoutTasks(tasks []Task, sessions []Session) []Unfiled {
	filed := map[string]bool{}
	for _, task := range tasks {
		for _, ref := range task.ExternalRefs {
			filed[ref] = true
		}
	}
	ids := map[string][]string{}
	for _, session := range sessions {
		if session.Live {
			for _, ref := range IssueNumbers(session.Name) {
				if !filed[ref] {
					ids[ref] = append(ids[ref], session.ID)
				}
			}
		}
	}
	keys := make([]string, 0, len(ids))
	for key := range ids {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	out := []Unfiled{}
	for _, key := range keys {
		out = append(out, Unfiled{External: key, People: People(ids[key], sessions)})
	}
	return out
}
