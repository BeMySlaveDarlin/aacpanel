package request

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Tail bounds what crosses the service socket and reaches the phone or model.
func Tail(text string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) > 40 {
		lines = lines[len(lines)-40:]
	}
	runes := []rune(strings.Join(lines, "\n"))
	if len(runes) > 4000 {
		runes = runes[len(runes)-4000:]
	}
	return string(runes)
}

func Summary(id, title string, steps []Step, outputs []*Output, step, from int, state string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The chain “%s” (%s):\n", title, id)
	last := -1
	for i, out := range outputs {
		if out != nil && out.Tail != "" {
			last = i
		}
	}
	for i := from; i < len(steps); i++ {
		out := outputs[i]
		if out == nil || out.State == "running" || out.State == "pending" {
			continue
		}
		text := strings.Join(strings.Fields(steps[i].Text), " ")
		if utf8.RuneCountInString(text) > 100 {
			text = string([]rune(text)[:100]) + "…"
		}
		fmt.Fprintf(&b, "%d %s · %s · ", i+1, steps[i].Who, text)
		if out.State == "cant" {
			fmt.Fprintf(&b, "Can't: %s", out.Note)
		} else {
			if out.Exit != nil {
				fmt.Fprintf(&b, "exit %d", *out.Exit)
			} else {
				b.WriteString(out.State)
			}
			if out.Started != nil && out.Ended != nil {
				fmt.Fprintf(&b, " · %.0f s", out.Ended.Sub(*out.Started).Seconds())
			}
			if out.Note != "" {
				fmt.Fprintf(&b, " · %s", out.Note)
			}
		}
		b.WriteByte('\n')
		if out.Tail != "" && (i == last || out.State == "failed") {
			b.WriteString(Tail(out.Tail))
			b.WriteByte('\n')
		}
		if out.Log != "" {
			fmt.Fprintf(&b, "Full log: %s\n", out.Log)
		}
	}
	switch state {
	case "answered":
		b.WriteString("The chain is done.")
	case "failed":
		b.WriteString("It failed; reopen, withdraw or start another chain.")
	case "declined":
		b.WriteString("The person can't continue. The chain is declined.")
	case "cancelled":
		b.WriteString("The person cancelled the chain: stop waiting on it and report none of its steps.")
	default:
		fmt.Fprintf(&b, "Step %d is yours: %s Report it with request {id: %q, step: %d, outcome}.", step, Sentence(steps[step-1].Text), id, step)
	}
	return b.String()
}

// Sentence ends the text of a step as a sentence: a step written with its own
// full stop keeps it, and one written without gets one.
func Sentence(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasSuffix(text, ".") || strings.HasSuffix(text, "!") || strings.HasSuffix(text, "?") || strings.HasSuffix(text, "…") {
		return text
	}
	return text + "."
}
