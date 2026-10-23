package action

// Ready is a reply to the service's request to save this conversation. It
// changes no host state and is deliberately outside the executor's Kinds.
type Ready struct {
	Conversation string `json:"conversation"`
	Intent       string `json:"intent,omitempty"`
}

func (r Ready) Validate() error {
	if !safeUUID(r.Conversation) {
		return badRequest("readiness needs the calling conversation's uuid")
	}
	if r.Intent != "" {
		return RestartIntent(r.Intent)
	}
	return nil
}
