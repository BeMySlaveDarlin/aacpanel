package executor

import (
	"context"
	"fmt"

	"aacpanel/internal/action"
)

func (e *Executor) sessionCommand(ctx context.Context, target string, cmd *action.Command) (string, error) {
	if cmd == nil || cmd.Name == "" {
		return "", fmt.Errorf("no command arrived: there is nothing to send")
	}
	s, err := findOneLiveSession(target)
	if err != nil {
		return "", err
	}
	if onStream(s) {
		return streamCommand(ctx, s, cmd)
	}
	t, kerr := termFor(ctx, s.PID)
	if kerr != nil {
		return "", fmt.Errorf(
			"a slash command cannot be sent to session %s from the panel: %v — as a letter they do not "+
				"go through at all, claude turns off parsing them for anything that came from another "+
				"agent", target, kerr)
	}

	line := commandLine(cmd)
	set := newSetting(s, cmd)
	confirmed, err := pasteAndSendWith(ctx, t, typed(cmd), watchTranscript(s), set.watch())
	if err != nil {
		return "", fmt.Errorf("session %s did not accept the input: %w", target, err)
	}
	detail := fmt.Sprintf("%s typed into %s (%s)", line, s.Name, sessionWhere(s)) + set.said()
	if t.attempts() > 1 {
		detail += "; " + t.kind() + " answered on the second attempt"
	}
	if !confirmed {
		detail += "; there is nothing to confirm the send with — the " + t.kind() + " screen cannot be read"
	}
	return detail, nil
}

func commandLine(cmd *action.Command) string {
	if cmd.Arg != "" {
		return "/" + cmd.Name + " " + cmd.Arg
	}
	return "/" + cmd.Name
}

func typed(cmd *action.Command) string {
	if cmd.Arg != "" {
		return commandLine(cmd)
	}
	return commandLine(cmd) + " "
}
