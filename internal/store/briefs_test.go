package store

import (
	"encoding/json"
	"testing"
)

func TestBriefDraftKeepsBothAnswerForms(t *testing.T) {
	for name, raw := range map[string]string{
		"questions":                `{"answers":{"order":{"picks":["A"],"note":"Window"}},"updatedAt":"2026-09-15T15:26:37Z"}`,
		"decisions":                `{"answers":{"decisions":{"order":{"picks":["A"]},"checks":{"note":"Thinking"}},"note":"Ready"},"updatedAt":"2026-09-15T15:26:37Z"}`,
		"question named decisions": `{"answers":{"decisions":{"picks":["A"],"note":"Window"}},"updatedAt":"2026-09-15T15:26:37Z"}`,
		"decision named picks":     `{"answers":{"decisions":{"picks":{"picks":["A"]}},"note":""},"updatedAt":"2026-09-15T15:26:37Z"}`,
	} {
		t.Run(name, func(t *testing.T) {
			var draft BriefDraft
			if err := json.Unmarshal([]byte(raw), &draft); err != nil {
				t.Fatal(err)
			}
			if draft.Decided() != 1 {
				t.Fatalf("progress: %d, %+v", draft.Decided(), draft)
			}
			back, err := json.Marshal(draft)
			if err != nil {
				t.Fatal(err)
			}
			var before, after any
			if err := json.Unmarshal([]byte(raw), &before); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(back, &after); err != nil {
				t.Fatal(err)
			}
			left, _ := json.Marshal(before)
			right, _ := json.Marshal(after)
			if string(left) != string(right) {
				t.Fatalf("draft changed form:\n%s\n%s", left, right)
			}
		})
	}
}
