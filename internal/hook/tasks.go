package hook

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"aacpanel/internal/hostcfg"
	"aacpanel/internal/taskreg"
)

func taskContext(_ string, payload []byte, _ time.Time) Reply {
	var in struct {
		CWD string `json:"cwd"`
	}
	if json.Unmarshal(payload, &in) != nil || in.CWD == "" {
		return Reply{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var out taskreg.Context
	if taskreg.Call(ctx, hostcfg.PanelURL(), taskreg.Request{Operation: "context", CWD: in.CWD}, &out) != nil || out.Project.Writer != taskreg.PanelWriter {
		return Reply{}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Project task registry: %s. Use task_list, task_show, task_create, task_update and handoff_read/write.\n", out.Project.Name)
	// A full handoff can consume the client's hook allowance and hide the queue.
	if h := out.Handoff; h != nil {
		fmt.Fprintf(&b, "\nLast handoff: %s\nRead its full markdown with handoff_read key=%s.\n", h.Title, h.Key)
		var groups map[string][]int64
		_ = json.Unmarshal(h.Tasks, &groups)
		names := map[int64]string{}
		for _, task := range out.HandoffTasks {
			names[task.Number] = task.Title
		}
		for _, group := range []string{"closed", "created", "touched"} {
			fmt.Fprintf(&b, "%s:", strings.ToUpper(group[:1])+group[1:])
			for _, n := range groups[group] {
				fmt.Fprintf(&b, " #%d [%s]", n, names[n])
			}
			b.WriteByte('\n')
		}
		if h.Next != "" {
			fmt.Fprintf(&b, "Next step: %s\n", h.Next)
		}
	}
	fmt.Fprintf(&b, "\nOpen tasks (%d total):\n", out.Queue.Total)
	for _, task := range out.Queue.Tasks {
		fmt.Fprintf(&b, "- %s #%d [%s]", task.Priority, task.Number, task.Title)
		if task.Order != nil {
			fmt.Fprintf(&b, " (order %g)", *task.Order)
		}
		if len(task.BlockedBy) > 0 {
			fmt.Fprintf(&b, " blocked by %v", task.BlockedBy)
		}
		b.WriteByte('\n')
	}
	return Reply{Context: strings.TrimSpace(b.String())}
}
