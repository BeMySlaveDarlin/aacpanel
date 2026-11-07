package executor

import (
	"context"
	"fmt"

	"aacpanel/internal/action"
)

func (e *Executor) Restart(ctx context.Context, req action.Request) (string, *action.OwnerTransfer, error) {
	if req.Kind != action.SessionRestart {
		return "", nil, fmt.Errorf("only a session restart passes its children's ownership")
	}
	thread, err := e.codexSession(req.Target)
	if err != nil {
		return "", nil, err
	}
	if thread != nil {
		return e.codexRestart(ctx, *thread, req)
	}
	if !e.restarting.take(req.Target) {
		return "", nil, fmt.Errorf("session %s is being restarted already: a second restart would bring up a second session, so this one does nothing", req.Target)
	}
	defer e.restarting.give(req.Target)
	defer markRestarting(req.Target)()
	return e.sessionRestart(ctx, req)
}
