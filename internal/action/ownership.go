package action

import "context"

// OwnerTransfer is the result of passing a restarted conversation's
// children, not a new relationship inferred from a reused session name.
type OwnerTransfer struct {
	From         string `json:"from"`
	Name         string `json:"name"`
	Conversation string `json:"conversation"`
}

func (p OwnerTransfer) Validate() error {
	if !safeUUID(p.From) || !safeUUID(p.Conversation) || p.From == p.Conversation {
		return badRequest("an ownership transfer needs distinct old and new conversations")
	}
	return ValidParent(p.Name)
}

// Restarter returns the ownership result that changed the host records to
// the service, which owns the records of sleeping children.
type Restarter interface {
	Restart(context.Context, Request) (string, *OwnerTransfer, error)
}
