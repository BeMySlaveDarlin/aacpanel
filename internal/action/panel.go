package action

const (
	PanelContextRestart = "context restart"
	PanelSessionStart   = "session start"
	PanelSessionRestart = "session restart"
	PanelSessionNote    = "the session's note"
)

// PanelMessage keeps the instruction a user prompt so the session obeys it;
// the envelope tells the feed that the panel, rather than the person, wrote it.
func PanelMessage(from, text string) string {
	return "<panel-message from=\"" + from + "\">\n" + text + "\n</panel-message>"
}
