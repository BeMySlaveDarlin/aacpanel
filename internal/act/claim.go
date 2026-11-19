package act

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"aacpanel/internal/request"
)

type ClaimInput struct {
	Expects string `json:"expects,omitempty"`
	Due     *int   `json:"due,omitempty"`
	Subject string `json:"subject,omitempty"`
	Re      string `json:"re,omitempty"`
	Outcome string `json:"outcome,omitempty"`
}

func (i ClaimInput) Validate() error {
	if i.Expects != "" && i.Re != "" {
		return errors.New("expects and re cannot be used together")
	}
	if i.Expects != "" && i.Expects != "reply" && i.Expects != "done" {
		return errors.New("expects must be reply or done")
	}
	if i.Expects == "" && (i.Due != nil || i.Subject != "") {
		return errors.New("due and subject need expects")
	}
	if i.Due != nil && (*i.Due < 1 || *i.Due > 1440) {
		return errors.New("due must be 1 to 1440 minutes")
	}
	if utf8.RuneCountInString(i.Subject) > 120 || (i.Subject != "" && strings.TrimSpace(i.Subject) == "") {
		return errors.New("subject needs 1 to 120 characters")
	}
	if i.Re != "" && !request.ValidID(i.Re) {
		return errors.New("re must be a claim id of 32 hex characters")
	}
	if i.Outcome != "" && i.Re == "" {
		return errors.New("outcome needs re")
	}
	switch i.Outcome {
	case "", "reply", "done", "cant", "withdraw":
	default:
		return errors.New("outcome must be reply, done, cant or withdraw")
	}
	return nil
}

func (i ClaimInput) Result() string {
	if i.Outcome == "" {
		return "reply"
	}
	return i.Outcome
}

func (i ClaimInput) Title(text string) string {
	if i.Subject != "" {
		return i.Subject
	}
	line, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	runes := []rune(strings.TrimSpace(line))
	return string(runes[:min(len(runes), 120)])
}

func (r Record) ClaimState(outcome string) (string, error) {
	if r.Kind != "claim" {
		return "", errors.New("the act is not a claim")
	}
	if !r.Open() {
		return "", fmt.Errorf("claim %s is %s", r.ID, r.State)
	}
	switch outcome {
	case "reply":
		if r.Body.Expects == "done" {
			return "open", nil
		}
		return "answered", nil
	case "done":
		return "answered", nil
	case "cant":
		return "declined", nil
	case "withdraw":
		return "withdrawn", nil
	}
	return "", errors.New("the claim outcome is not valid")
}

func (r Record) ClaimTarget(conversation, session, to, outcome string) (string, error) {
	if _, err := r.ClaimState(outcome); err != nil {
		return "", err
	}
	mine, target := session == r.Peer || conversation == r.PeerConversation, r.Session
	if outcome == "withdraw" {
		mine, target = session == r.Session || conversation == r.Conversation, r.Peer
	}
	if !mine {
		return "", fmt.Errorf("the claim %s is not yours to answer", r.ID)
	}
	if to != "" && to != target {
		return "", fmt.Errorf("claim %s needs to=%s", r.ID, target)
	}
	return target, nil
}

func (r Record) Expectation() string {
	if r.Body.Expects == "done" {
		return "the work done"
	}
	return "a reply"
}

func ClaimMinutes(span time.Duration) int {
	return max(1, int(math.Ceil(span.Minutes())))
}

func (r Record) ClaimLetter(text string, now time.Time) string {
	due := "no due"
	if r.Due != nil {
		due = fmt.Sprintf("due in %d min", ClaimMinutes(r.Due.Sub(now)))
	}
	return fmt.Sprintf("Claim %s from %s: expects %s, %s. Answer with send_to_session re=%s (outcome done when the work is done, cant if you cannot do it).\n\n%s",
		r.ID, r.Session, r.Expectation(), due, r.ID, text)
}

func (r Record) ClaimReceipt(said, outcome string) string {
	if outcome == "" {
		due := "it has no due, so nobody is reminded"
		if r.Due != nil {
			due = "if none comes by " + r.Due.Local().Format("15:04") + ", the panel tells you"
		}
		return fmt.Sprintf("Sent as claim %s: %s. The answer comes as a letter with re=%s; %s.", r.ID, said, r.ID, due)
	}
	if r.Open() {
		return fmt.Sprintf("Sent: %s. Claim %s stays open: it expects the work done — send outcome done when it is, or cant.", said, r.ID)
	}
	state := r.State
	if outcome == "done" {
		state = "done"
	}
	return fmt.Sprintf("Sent: %s. Claim %s is %s.", said, r.ID, state)
}

func (r Record) ClaimMessageID(rung int, side string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("claim:%s:%d:%s", r.ID, rung, side)))
	return MessageID(hex.EncodeToString(sum[:16]))
}
