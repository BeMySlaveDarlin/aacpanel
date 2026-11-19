package action

import (
	"strings"
	"testing"
)

func TestQuestionClassificationBoundsOpenQuestions(t *testing.T) {
	r := Request{Ask: AskQuestion, Target: "work", Conversation: "01234567-1234-4321-aaaa-123456789012", Text: "Publish?"}
	for range 12 {
		r.AlreadyAsked = append(r.AlreadyAsked, strings.Repeat("é", 200))
	}
	if err := r.Validate(); err != nil {
		t.Fatalf("bounded open questions: %v", err)
	}
	r.AlreadyAsked = append(r.AlreadyAsked, "One more?")
	if err := r.Validate(); err == nil {
		t.Fatal("more than twelve open questions were accepted")
	}
	r.AlreadyAsked = []string{strings.Repeat("é", 201)}
	if err := r.Validate(); err == nil {
		t.Fatal("an open question longer than two hundred characters was accepted")
	}
}
