package action

// PermitFingerprintMax is the length ceiling for a fingerprint.
const PermitFingerprintMax = 128

// Permit is a press in a permission prompt.
type Permit struct {
	Option      int    `json:"option"`
	Fingerprint string `json:"fingerprint"`

	// Tail is the end of the dialog as it was read, spaces dropped. It is what
	// a dialog taller than the console screen is recognised by: the head of a
	// long command scrolls away, and how much of it is off the screen changes
	// with the width of the window, so what the two readings share is an end.
	Tail string `json:"tail"`
}

// PermOption is one option of a prompt.
type PermOption struct {
	N       int    `json:"n"`
	Text    string `json:"text"`
	Lasting bool   `json:"lasting"`
}

// Permission is a parsed permission prompt as the panel will show it.
type Permission struct {
	Tool        string       `json:"tool"`
	Action      []string     `json:"action"`
	Options     []PermOption `json:"options"`
	Partial     bool         `json:"partial"`
	Unknown     bool         `json:"unknown"`
	Fingerprint string       `json:"fingerprint"`

	// Tail is the end of the dialog, spaces dropped, and it comes back with the
	// keypress: a dialog whose head is off the screen is recognised by an end
	// the two readings share.
	Tail string `json:"tail"`

	// Note holds what the console adds under the command when the question is not
	// its own: a hook asked for the confirmation, or a permission rule or a classifier
	// did, and the lines say which and why. Without the note the human does not know
	// what they are confirming.
	Note []string `json:"note"`

	// Cut says the opening of the dialog is off the console screen: the dialog is
	// taller than the screen, and what was scrolled off is the head of the command
	// and the heading with the tool. Tool is then what the note names, or empty, and
	// Action is the tail as it stands.
	Cut bool `json:"cut"`

	// Raw holds the dialog lines as they stand on the screen, and only when the
	// parse failed: unmarked text still says what is being asked, silence sends the
	// human to the console. It reaches no further than the dialog itself — above it
	// runs an ordinary conversation, and there are secrets in it.
	Raw []string `json:"raw"`
}
