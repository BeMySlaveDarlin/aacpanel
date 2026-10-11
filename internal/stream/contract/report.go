// Package contract checks the stream protocol against the requests the panel uses.
// The live runner spends tokens; parsing and verdict tests read recorded events.
package contract

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const (
	OK   = "ok"
	Fail = "FAIL"
	Info = "info"
)

type Result struct {
	Check    string `json:"check"`
	Status   string `json:"status"`
	Detail   string `json:"detail"`
	Required bool   `json:"required"`
}

type Report []Result

func verdict(name string, ok bool, detail string) Result {
	status := Fail
	if ok {
		status = OK
	}
	return Result{name, status, detail, true}
}

func (r Report) ExitCode() int {
	for _, v := range r {
		if v.Required && v.Status == Fail {
			return 1
		}
	}
	return 0
}

func (r Report) Text() string {
	width := 10
	if len(r) > 0 {
		width = 0
	}
	for _, v := range r {
		width = max(width, len(v.Check))
	}
	var b strings.Builder
	for _, v := range r {
		info := ""
		if !v.Required {
			info = "(info) "
		}
		fmt.Fprintf(&b, "%*s  %-4s  %s%s\n", -width, v.Check, v.Status, info, v.Detail)
	}
	return b.String()
}

// A control response has an open body: its fields belong to the requested
// operation, and a contract must report an unfamiliar shape without panicking.
type object map[string]any

func (o object) text(key string) string { s, _ := o[key].(string); return s }
func (o object) obj(key string) object  { return asObject(o[key]) }
func (o object) list(key string) []any  { v, _ := o[key].([]any); return v }
func asObject(v any) object {
	switch m := v.(type) {
	case map[string]any:
		return object(m)
	case object:
		return m
	}
	return nil
}
func success(o object) bool { return o.text("subtype") == "success" }
func isAsk(o object) bool {
	return o.text("type") == "control_request" && o.obj("request").text("subtype") == "can_use_tool"
}
func toolOf(o object) string { return o.obj("request").text("tool_name") }
func isResult(o object) bool { return o.text("type") == "result" }
func system(o object, subtype string) bool {
	return o.text("type") == "system" && o.text("subtype") == subtype
}

func taskEnded(o object, id string) bool {
	if o.text("type") != "system" || o.text("task_id") != id {
		return false
	}
	return o.text("subtype") == "task_notification" || (o.text("subtype") == "task_updated" &&
		slices.Contains([]string{"completed", "failed", "killed", "stopped"}, o.obj("patch").text("status")))
}

func assistantText(events []object) string {
	var out []string
	for _, ev := range events {
		if ev.text("type") != "assistant" {
			continue
		}
		for _, part := range ev.obj("message").list("content") {
			p := asObject(part)
			if p.text("type") == "text" {
				out = append(out, p.text("text"))
			}
		}
	}
	return strings.Join(out, "\n")
}

func toolResults(events []object) string {
	var out []string
	for _, ev := range events {
		if ev.text("type") != "user" {
			continue
		}
		for _, part := range ev.obj("message").list("content") {
			p := asObject(part)
			if p.text("type") != "tool_result" {
				continue
			}
			if list, ok := p["content"].([]any); ok {
				for _, x := range list {
					out = append(out, asObject(x).text("text"))
				}
			} else {
				out = append(out, fmt.Sprint(p["content"]))
			}
		}
	}
	return strings.Join(out, " ")
}

func messageText(events []object, usersOnly bool) string {
	text := assistantText(events)
	for _, ev := range events {
		if !usersOnly || ev.text("type") == "user" {
			b, _ := json.Marshal(ev.obj("message")["content"])
			text += " " + string(b)
		}
	}
	return text
}

func keys(v any) string {
	m := asObject(v)
	if m == nil {
		return fmt.Sprintf("%T", v)
	}
	if len(m) == 1 {
		for name, value := range m {
			if items, ok := value.([]any); ok {
				fields := "nothing"
				if len(items) > 0 && len(asObject(items[0])) > 0 {
					fields = keys(items[0])
				}
				return fmt.Sprintf("%s: %d, each with %s", name, len(items), fields)
			}
		}
	}
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

func cut(s string, n int) string {
	r := []rune(s)
	return string(r[:min(n, len(r))])
}
