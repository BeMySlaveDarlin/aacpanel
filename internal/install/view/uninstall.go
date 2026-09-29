package view

import (
	"errors"
	"strings"

	"aacpanel/internal/install"
	"aacpanel/internal/install/ui"
)

// UninstallOptions shape ./install.sh uninstall.
type UninstallOptions struct {
	Lines
	// Removal is the removal the manifest makes; nil without a manifest,
	// and Traces then are what the machine holds of the panel.
	Removal *install.Removal
	Refusal error
	Traces  []string
	// Yes removes without the question and deletes the data Purge names
	// without the host name typed. DryRun shows the plan and the part as
	// root, and changes nothing.
	Yes, DryRun bool
	Purge       []string
	// Begin opens the run: the journal, the shell, the clone and the user.
	Begin func(*install.Removal) (*install.Run, error)
	// DryRoot runs root.sh remove with --dry-run and gives what it said.
	DryRoot func(args []string) (string, error)
}

// Uninstall takes the panel off the machine by its manifest, in U1–U9: the
// plan and the questions, the parts in their order, the report of what went
// and what stays, and the installer's directory last. It returns the exit
// status.
func Uninstall(o UninstallOptions) int {
	t, p := o.Theme, o.plain()
	stop := func(err error) int {
		p.Print(t.Entry(ui.Bad, "Uninstall", []string{said{install.Stop, err.Error()}.render(t)}, PlainWidth))
		return 1
	}
	rm := o.Removal
	if rm == nil {
		p.Print(t.Entry(ui.Bad, "Uninstall", []string{said{install.Stop, o.Refusal.Error()}.render(t)}, PlainWidth))
		p.Print(t.Entry(ui.Plain, "What is here", o.Traces, PlainWidth))
		return 1
	}
	var rows []ui.Row
	for _, r := range rm.Plan() {
		rows = append(rows, ui.Row{Text: r.Text, Tag: r.Tag, Head: r.Head})
	}
	p.Print("\n" + t.Frame(ui.Frame{Title: "Uninstall plan", Edge: t.PlanEdge, Rows: rows}, PlainWidth))
	if o.DryRun {
		if args, any := rm.RootArgs(); any && o.DryRoot != nil {
			out, err := o.DryRoot(args)
			lines := strings.Split(strings.TrimSpace(out), "\n")
			if err != nil {
				lines = append(lines, said{install.Stop, err.Error()}.render(t))
			}
			p.Print(t.Entry(ui.Plain, "Root part — what root.sh would run", lines, PlainWidth))
		}
		p.Print("\n" + strings.Join(ui.Indent("Nothing on this machine was changed: --dry-run only looks.", " ", " ", PlainWidth), "\n"))
		return 0
	}

	remove := o.Yes
	if !remove {
		if !o.Tty {
			return stop(errors.New(`stop: no terminal to ask "Remove the panel?"; pass --yes`))
		}
		vals, ok, err := askOnce(t, o.Out, "Uninstall", []install.Question{rm.RemoveQuestion()}, true)
		if err != nil {
			return stop(err)
		}
		remove = ok && vals[0] == "yes"
	}
	if !remove {
		p.Print(t.Entry(ui.Plain, "Uninstall", []string{"Nothing was removed."}, PlainWidth))
		return 0
	}
	purge, typed := o.Purge, ""
	if !o.Yes && o.Tty && len(purge) == 0 {
		vals, ok, err := askOnce(t, o.Out, "Data", []install.Question{rm.DataQuestion()}, true)
		if err != nil {
			return stop(err)
		}
		if ok && vals[0] != "" {
			purge = strings.Split(vals[0], ",")
		}
	}
	if len(purge) > 0 && !o.Yes {
		if !o.Tty {
			return stop(errors.New("stop: no terminal to type the host name before the data goes; pass --yes with the --purge flags"))
		}
		vals, ok, err := askOnce(t, o.Out, "Data", []install.Question{rm.HostQuestion()}, true)
		if err != nil {
			return stop(err)
		}
		if ok {
			typed = vals[0]
		}
	}
	if err := rm.Choose(purge, typed, o.Yes); err != nil {
		return stop(err)
	}
	r, err := o.Begin(rm)
	if err != nil {
		return stop(err)
	}
	if err := o.Run(r, rm.Steps()); err != nil {
		var stopped *install.Interrupted
		title := "Stopped"
		status := 1
		if errors.As(err, &stopped) {
			title, status = "Interrupted after "+stopped.After, 130
		}
		p.Print(t.Entry(ui.Bad, title, []string{"Fix it and run ./install.sh uninstall again: what went is not taken twice, and the manifest still holds the rest."}, PlainWidth))
		return status
	}
	p.Print(t.Entry(ui.Good, "Removed", []string{"The panel is off this machine."}, PlainWidth))
	p.Print(t.Entry(ui.Plain, "Left", rm.Left(), PlainWidth))
	if r.Journal != nil {
		_ = r.Journal.Close()
		r.Journal = nil
	}
	if err := o.Run(r, []*install.Step{rm.LastStep()}); err != nil {
		return 1
	}
	return 0
}
