package action

// PermitFingerprintMax is the length ceiling for a fingerprint.
const PermitFingerprintMax = 128

// Permit is a press in a permission prompt.
type Permit struct {
	Option      int    `json:"option"`
	Fingerprint string `json:"fingerprint"`
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

	// Raw holds the dialog lines as they stand on the screen, and only when the
	// parse failed: unmarked text still says what is being asked, silence sends the
	// human to the console. It reaches no further than the dialog itself — above it
	// runs an ordinary conversation, and there are secrets in it.
	Raw []string `json:"raw"`
}
