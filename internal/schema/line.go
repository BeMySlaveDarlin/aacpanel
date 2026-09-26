package schema

// Word is one word of the command a launch runs, with the parameter that put
// it there; the command's own words name none. Layer is where the parameter's
// value came from, filled by whoever knows the layers.
type Word struct {
	Text  string `json:"text"`
	Key   string `json:"key,omitempty"`
	Layer string `json:"layer,omitempty"`
}

// Line is the command a launch runs, word by word, built by the launcher's
// own code without starting anything; Then is what the holder of a session on
// the stream does once claude has answered, since that is not in the command.
type Line struct {
	Words    []Word   `json:"words"`
	Then     []Word   `json:"then,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}
