package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"aacpanel/internal/action"
	"aacpanel/internal/hostcfg"
)

const (
	askStoreEnv     = "AACP_ASK_STORE"
	askStoreName    = "asked.json"
	keyPause        = 80 * time.Millisecond
	keyRight        = "\x1b[C"
	keyTab          = "\t"
	submitPick      = "1"
	fieldWait       = 3 * time.Second
	fieldPoll       = 90 * time.Millisecond
	dialogMax       = 9
	dialogMarkRunes = 10
)

type storedAsk struct {
	SessionID string           `json:"sessionId"`
	ToolUseID string           `json:"toolUseId"`
	Questions []storedQuestion `json:"questions"`
}

type storedQuestion struct {
	Multi   bool           `json:"multi"`
	Options []storedOption `json:"options"`
}

type storedOption struct {
	Label   string `json:"label"`
	Preview string `json:"preview"`
}

func (q storedQuestion) hasPreview() bool {
	for _, opt := range q.Options {
		if opt.Preview != "" {
			return true
		}
	}
	return false
}

func (e *Executor) sessionAnswer(ctx context.Context, target string, ans *action.Answer) (string, error) {
	if ans == nil {
		return "", fmt.Errorf("answer with no options picked")
	}
	s, ask, err := askingSession(target, ans.AskID)
	if err != nil {
		return "", err
	}

	steps, err := answerKeys(ask, ans.Picks, ans.Texts)
	if err != nil {
		return "", err
	}
	if err := playDialog(ctx, s, target, steps); err != nil {
		return "", err
	}
	return fmt.Sprintf("answer sent to %s: %s", s.Name, picked(ask, ans.Picks, ans.Texts)), nil
}

func (e *Executor) sessionDismiss(ctx context.Context, target string, ans *action.Answer) (string, error) {
	if ans == nil {
		return "", fmt.Errorf("it is not said which question to dismiss")
	}
	s, ask, err := askingSession(target, ans.AskID)
	if err != nil {
		return "", err
	}
	first := ask.Questions[0]
	if first.hasPreview() {
		return "", fmt.Errorf(
			"this question cannot be dismissed from the panel: the options carry a preview, and in that " +
				"layout the \"Chat about this\" item is drawn without a number — there is no digit to press")
	}
	item := len(first.Options) + 2
	if item > dialogMax {
		return "", fmt.Errorf(
			"this question cannot be dismissed from the panel: the \"Chat about this\" item got number %d, "+
				"and items are picked with a single digit", item)
	}
	if err := playDialog(ctx, s, target, []dialogStep{{keys: strconv.Itoa(item)}}); err != nil {
		return "", err
	}
	return fmt.Sprintf("question dismissed in %s: the session is waiting for an ordinary message", s.Name), nil
}

func askingSession(target, askID string) (liveSession, storedAsk, error) {
	found, known, err := liveSessionsByName(target)
	if err != nil {
		return liveSession{}, storedAsk{}, err
	}
	switch {
	case len(found) == 0:
		if len(known) == 0 {
			return liveSession{}, storedAsk{},
				fmt.Errorf("there is no live session %s: there are no live sessions at all right now", target)
		}
		return liveSession{}, storedAsk{},
			fmt.Errorf("there is no live session %s; live ones are: %s", target, strings.Join(known, ", "))
	case len(found) > 1:
		return liveSession{}, storedAsk{},
			fmt.Errorf("there are two sessions named %s right now — it is unclear whose question to close", target)
	}
	s := found[0]

	ask, err := askOf(s.SessionID)
	if err != nil {
		return liveSession{}, storedAsk{}, askMiss(s, err)
	}
	if ask.ToolUseID != askID {
		return liveSession{}, storedAsk{}, fmt.Errorf(
			"session %s is asking something else now: the previous question was answered outside the panel", target)
	}
	return s, ask, nil
}

func playDialog(ctx context.Context, s liveSession, target string, steps []dialogStep) error {
	t, err := termFor(ctx, s.PID)
	if err != nil {
		return fmt.Errorf("the question of session %s cannot be answered from the panel: %w", target, err)
	}
	for i, step := range steps {
		if i > 0 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("the answer was interrupted at step %d of %d — the dialog stayed open", i+1, len(steps))
			case <-time.After(keyPause):
			}
		}
		if !step.paste {
			if err := t.send(ctx, step.keys); err != nil {
				return fmt.Errorf("step %d of %d did not go through: %w", i+1, len(steps), err)
			}
			continue
		}
		if err := pasteIntoDialog(ctx, t, step.keys); err != nil {
			return fmt.Errorf("step %d of %d did not go through: %w", i+1, len(steps), err)
		}
	}
	return nil
}

func pasteIntoDialog(ctx context.Context, t term, text string) error {
	mark := dialogMark(text)
	was := 0
	if screen, known := t.screen(ctx); known {
		was = strings.Count(squeeze(screen), mark)
	}
	if err := t.send(ctx, pasteStart+text+pasteEnd); err != nil {
		return err
	}
	deadline := time.Now().Add(fieldWait)
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("the free-form answer was typed, but there was no time to confirm it")
		case <-time.After(fieldPoll):
		}
		screen, known := t.screen(ctx)
		if !known {
			return nil
		}
		if mark == "" || strings.Count(squeeze(screen), mark) > was {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf(
				"the input field did not open: the typed words are not on the session screen after %s, "+
					"and pressing Enter blindly would confirm someone else's item", fieldWait)
		}
	}
}

func dialogMark(text string) string {
	var out []rune
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		out = append(out, r)
		if len(out) == dialogMarkRunes {
			break
		}
	}
	return string(out)
}

func askStorePath() string {
	if path := os.Getenv(askStoreEnv); path != "" {
		return path
	}
	return filepath.Join(hostcfg.Load().StateDir, askStoreName)
}

func askMiss(s liveSession, err error) error {
	if s.Status != "waiting" {
		return err
	}
	return fmt.Errorf("session %s is standing on a dialog, but its question is not in the agent store "+
		"(%w) — answer it at the machine: the panel does not press keys blind", s.Name, err)
}

func askOf(sessionID string) (storedAsk, error) {
	if sessionID == "" {
		return storedAsk{}, fmt.Errorf("the session has no conversation yet, and therefore no question")
	}
	raw, err := os.ReadFile(askStorePath())
	if err != nil {
		return storedAsk{}, fmt.Errorf("the question store was not read: %w", err)
	}
	var book map[string]storedAsk
	if err := json.Unmarshal(raw, &book); err != nil {
		return storedAsk{}, fmt.Errorf("the question store was not parsed: %w", err)
	}
	ask, ok := book[sessionID]
	if !ok || len(ask.Questions) == 0 {
		return storedAsk{}, fmt.Errorf("the session is not asking anything right now")
	}
	return ask, nil
}

func answerKeys(ask storedAsk, picks [][]int, texts []string) ([]dialogStep, error) {
	if len(picks) != len(ask.Questions) {
		return nil, fmt.Errorf("there are %d questions, but %d answers arrived", len(ask.Questions), len(picks))
	}
	if len(texts) > 0 && len(texts) != len(ask.Questions) {
		return nil, fmt.Errorf("there are %d questions, but %d free-form answers arrived", len(ask.Questions), len(texts))
	}
	single := len(ask.Questions) == 1 && !ask.Questions[0].Multi

	var keys []dialogStep
	needSubmit := false
	for i, q := range ask.Questions {
		chosen := picks[i]
		own := ""
		if i < len(texts) {
			own = texts[i]
		}
		if own != "" {
			open, err := ownWordsKey(q, i)
			if err != nil {
				return nil, err
			}
			if len(chosen) > 0 {
				return nil, fmt.Errorf(
					"question %d has both a picked option and a free-form answer — in the dialog these are different items", i+1)
			}
			keys = append(keys,
				dialogStep{keys: open},
				dialogStep{keys: own, paste: true},
				dialogStep{keys: enterKey},
			)
			if !single {
				needSubmit = true
			}
			continue
		}
		if !q.Multi && len(chosen) > 1 {
			return nil, fmt.Errorf("question %d takes a single choice, but %d options arrived", i+1, len(chosen))
		}
		for _, pick := range chosen {
			if pick > len(q.Options) {
				return nil, fmt.Errorf("question %d has only %d options, and %d was picked", i+1, len(q.Options), pick)
			}
			if pick > dialogMax {
				return nil, fmt.Errorf("option %d cannot be picked from the panel: items in the dialog are picked with a single digit", pick)
			}
			keys = append(keys, dialogStep{keys: strconv.Itoa(pick)})
		}
		switch {
		case single && len(chosen) == 0:
		case q.hasPreview() && len(chosen) > 0:
			keys = append(keys, dialogStep{keys: enterKey})
			needSubmit = needSubmit || !single
		case q.hasPreview():
			keys = append(keys, dialogStep{keys: keyTab})
			needSubmit = true
		case single:
		case q.Multi || len(chosen) == 0:
			keys = append(keys, dialogStep{keys: keyRight})
			needSubmit = true
		default:
			needSubmit = true
		}
	}
	if needSubmit {
		keys = append(keys, dialogStep{keys: submitPick})
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("there is nothing to press: no option was picked and not a word was written")
	}
	return keys, nil
}

func ownWordsKey(q storedQuestion, at int) (string, error) {
	if q.Multi {
		return "", fmt.Errorf(
			"question %d cannot be answered in your own words: it takes several choices, and the free-form "+
				"item in such a dialog is a checkbox, not a field. Pick options, or dismiss the question "+
				"and write in the composer", at+1)
	}
	if q.hasPreview() {
		return "", fmt.Errorf(
			"question %d cannot be answered in your own words: the options carry a preview, and in that "+
				"layout there is no free-form item at all — only a note to the picked option", at+1)
	}
	item := len(q.Options) + 1
	if item > dialogMax {
		return "", fmt.Errorf(
			"question %d cannot be answered in your own words: the item got number %d, "+
				"and items are picked with a single digit", at+1, item)
	}
	return strconv.Itoa(item), nil
}

func picked(ask storedAsk, picks [][]int, texts []string) string {
	var parts []string
	for i, q := range ask.Questions {
		if i >= len(picks) {
			break
		}
		if i < len(texts) && texts[i] != "" {
			parts = append(parts, fmt.Sprintf("in your own words, %d characters", len([]rune(texts[i]))))
			continue
		}
		var labels []string
		for _, pick := range picks[i] {
			if pick >= 1 && pick <= len(q.Options) {
				labels = append(labels, q.Options[pick-1].Label)
			}
		}
		if len(labels) == 0 {
			labels = append(labels, "skipped")
		}
		parts = append(parts, strings.Join(labels, ", "))
	}
	return strings.Join(parts, " · ")
}

type dialogStep struct {
	keys  string
	paste bool
}
