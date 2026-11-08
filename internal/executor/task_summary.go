package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"aacpanel/internal/action"
	"aacpanel/internal/launcher"
)

func (e *Executor) taskSummary(ctx context.Context, req action.Request) (string, error) {
	if req.Project == nil || req.Contour == "" || strings.TrimSpace(req.Text) == "" || len(req.Text) > 256<<10 {
		return "", fmt.Errorf("a summary needs a mapped contour project and a bounded prompt")
	}
	p, err := chooseProject(req.Target, req.Project)
	if err != nil {
		return "", err
	}
	bin, err := launcherPath()
	if err != nil {
		return "", err
	}
	model := os.Getenv("AACP_TASK_SUMMARY_MODEL")
	if model == "" {
		model = "sonnet"
	}
	var report launcher.Summary
	ctx, cancel := context.WithTimeout(ctx, launcher.SummaryTimeout)
	defer cancel()
	err = e.runUnit(ctx, bin, summaryFlag, "summarizing tasks", e.memory.Session,
		launcher.SummarySpec{Dir: p.Path, ClaudeBin: p.ClaudeBin, ConfigDir: p.ConfigDir, Prompt: req.Text, Model: model}, &report)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(report.Text) == "" || len(report.Text) > 64<<10 || report.Model != model {
		return "", fmt.Errorf("the launcher returned an invalid summary")
	}
	raw, err := json.Marshal(report)
	return string(raw), err
}
