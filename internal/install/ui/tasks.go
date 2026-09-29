package ui

import "strings"

// TaskState is where a task of the list stands.
type TaskState int

const (
	Waiting TaskState = iota
	Running
	Finished
	Failed
)

// Task is a line of the list of steps.
type Task struct {
	Title string
	State TaskState
}

// Tasks is the list of steps the way Claude Code keeps its todo list: ☒ done,
// ■ at work, ☐ ahead, and ✗ where the run stopped. It nests under whatever
// line stands above it, as a step's output does.
func (t Theme) Tasks(items []Task, width int) string {
	lines := make([]string, len(items))
	for i, it := range items {
		switch it.State {
		case Finished:
			lines[i] = t.Dim.Render("☒ " + it.Title)
		case Running:
			lines[i] = t.Strong.Render("■ " + it.Title)
		case Failed:
			lines[i] = t.Fail.Render("✗ " + it.Title)
		default:
			lines[i] = "☐ " + it.Title
		}
	}
	return t.Output(lines, width)
}

// TaskList is the list as an entry of the feed, under a heading of its own.
func (t Theme) TaskList(title string, items []Task, width int) string {
	return strings.Join([]string{"", t.Step(Plain, title, width), t.Tasks(items, width)}, "\n")
}
