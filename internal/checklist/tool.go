package checklist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"aacpanel/internal/mcp"
)

// ToolName is the tool's name on the panel's server: the model calls it
// mcp__aacpanel__checklist.
const ToolName = "checklist"

// Instructions is the tool's line in the server's word to every session that
// has it.
const Instructions = "When the work has several steps, keep them with the checklist tool, which the person sees " +
	"in the panel: the whole list every time, sent when a step starts or ends. A short task needs none."

// Description is the tool's own word to the model.
const Description = "The checklist of the current work, shown to the person in the panel on their phone and desk; " +
	"the terminal does not show it. Send the whole list every time, in order, each step with its status: " +
	"pending, active (being worked on now; one at a time), done, or dropped (no longer needed, kept for the record). " +
	"Update it when a step starts or ends and when the checklist changes. Whether to keep a checklist and what makes a step " +
	"is yours to decide: a question or a one-step task needs none. An empty list clears the checklist; " +
	"a call without items changes nothing and returns the checklist as it stands. " +
	"The checklist belongs to the place the session works in and outlives a restart of the session. " +
	"note is an optional short line about the checklist as a whole, such as what it waits on. " +
	"The checklist belongs to the main conversation: a subagent does not call this."

// Tool is the checklist tool of the panel's server, keeping the checklists in
// dir with the times now gives. It is allowed: a list of steps that asked the
// person before every update would not be kept.
func Tool(dir string, now func() time.Time) mcp.Tool {
	return mcp.Tool{
		Name:         ToolName,
		Title:        "Checklist",
		Description:  Description,
		InputSchema:  InputSchema(),
		Instructions: Instructions,
		Standing:     func(b mcp.Binding) string { return standing(Adopt(dir, b.Place)) },
		Allowed:      true,
		Call: func(_ context.Context, bind mcp.Bind, args json.RawMessage) (string, bool) {
			return call(dir, now, bind, args)
		},
	}
}

// standingStep bounds the step the instructions quote: they stand in the
// system prompt of the whole session.
const standingStep = 60

// standing is what the instructions add when the place already has a checklist:
// a session started again here — afresh or going on with its conversation —
// learns of it before its first word, since the person sees it all along.
// It says how far the checklist got and the step it stands at, and how to read
// the rest; the length is bounded whatever the checklist holds.
func standing(p *Checklist) string {
	if p == nil || len(p.Items) == 0 {
		return ""
	}
	finished, at := progress(p)
	var b strings.Builder
	fmt.Fprintf(&b, "This place already has a checklist (last sent %s): %d of %d steps finished",
		p.At, finished, len(p.Items))
	switch {
	case at == nil:
		b.WriteString(", nothing left to do")
	case at.Status == Active:
		fmt.Fprintf(&b, ", the current step: “%s”", clip(at.Text, standingStep))
	default:
		fmt.Fprintf(&b, ", the next step: “%s”", clip(at.Text, standingStep))
	}
	b.WriteString(". The person sees it as it is. To go on with it, call the tool without items to read it " +
		"whole; if it no longer applies, clear it with an empty list.")
	return b.String()
}

// progress counts the steps finished — done or dropped — and finds the step
// the checklist stands at: the one at work, or else the first still to do.
func progress(p *Checklist) (int, *Item) {
	finished := 0
	var active, next *Item
	for i := range p.Items {
		switch p.Items[i].Status {
		case Done, Dropped:
			finished++
		case Active:
			if active == nil {
				active = &p.Items[i]
			}
		case Pending:
			if next == nil {
				next = &p.Items[i]
			}
		}
	}
	if active != nil {
		return finished, active
	}
	return finished, next
}

func clip(s string, runes int) string {
	if utf8.RuneCountInString(s) <= runes {
		return s
	}
	return string([]rune(s)[:runes-1]) + "…"
}

// InputSchema is what the tool takes, as JSON Schema.
func InputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"items": map[string]any{
				"type":        "array",
				"description": "Every step of the checklist, in order. Left out, the call reads the checklist and changes nothing.",
				"maxItems":    MaxItems,
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"text":   map[string]any{"type": "string", "minLength": 1, "maxLength": MaxText, "description": "The step, in a few words."},
						"status": map[string]any{"type": "string", "enum": Statuses},
					},
					"required":             []string{"text", "status"},
					"additionalProperties": false,
				},
			},
			"note": map[string]any{"type": "string", "maxLength": MaxNote, "description": "One short line about the checklist as a whole."},
		},
		"additionalProperties": false,
	}
}

// call runs the tool on the checklist of the place the claude works in, found
// anew on every call: a call without items reads the checklist, one with them
// keeps it.
func call(dir string, now func() time.Time, bind mcp.Bind, raw json.RawMessage) (string, bool) {
	var args struct {
		Items []Item `json:"items"`
		Note  string `json:"note"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "The checklist was not kept: the arguments are not the checklist's (" + err.Error() + ").", true
		}
	}
	b, err := bind()
	if err != nil {
		if args.Items == nil {
			return "The checklist was not read: " + err.Error(), true
		}
		return "The checklist was not kept: " + err.Error(), true
	}
	// A checklist filed under a conversation of the place is taken over before
	// anything else: a step sent again keeps the time it had there.
	current := Adopt(dir, b.Place)
	if args.Items == nil {
		return listing(current), false
	}
	kept, err := Keep(dir, b, args.Items, args.Note, now())
	if err != nil {
		var refused Refusal
		if errors.As(err, &refused) {
			return "The checklist was not kept: " + refused.Why + ".", true
		}
		return "The checklist was not kept: " + err.Error(), true
	}
	return summary(kept), false
}

// summary is what the model hears back: short, since it is read on every
// update of the checklist.
func summary(p *Checklist) string {
	if p == nil {
		return "The checklist is cleared."
	}
	finished, _ := progress(p)
	return fmt.Sprintf("The checklist is kept: %d of %d steps finished.", finished, len(p.Items))
}

// listing is the checklist read whole, for a call without items: a session that
// goes on with a checklist it did not send sends the list back with its own
// changes, and needs the steps as they stand to do it.
func listing(p *Checklist) string {
	if p == nil || len(p.Items) == 0 {
		return "There is no checklist in this place."
	}
	finished, _ := progress(p)
	var b strings.Builder
	fmt.Fprintf(&b, "The checklist, last sent %s: %d of %d steps finished.", p.At, finished, len(p.Items))
	for i, it := range p.Items {
		fmt.Fprintf(&b, "\n%d. [%s] %s", i+1, it.Status, it.Text)
	}
	if p.Note != "" {
		b.WriteString("\nNote: " + p.Note)
	}
	return b.String()
}
