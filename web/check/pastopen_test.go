package check

import "testing"

// A card of the archive opens a new session in the project it ran in, and the
// project it ran in is an entry of the map, not a word. Two contours may hold
// a directory of the same name — sent as a name the panel refuses to guess
// between them, and the button does nothing at all for the person pressing it.
func TestANewSessionFromTheArchiveNamesTheProjectOfTheMap(t *testing.T) {
	var got struct {
		Sent []struct {
			Kind   string         `json:"kind"`
			Target string         `json:"target"`
			Params map[string]any `json:"params"`
		} `json:"sent"`
		Trouble []string `json:"trouble"`
	}
	runFixture(t, "pastopen.html", &got)

	if len(got.Trouble) > 0 {
		t.Fatalf("the buttons of the card: %v", got.Trouble)
	}
	if len(got.Sent) != 2 {
		t.Fatalf("the two presses sent %d requests: %+v", len(got.Sent), got.Sent)
	}

	mapped := got.Sent[0]
	if mapped.Kind != "session.open" {
		t.Errorf("the card sent %q", mapped.Kind)
	}
	if id, ok := mapped.Params["project"].(float64); !ok || int(id) != 42 {
		t.Errorf("a row standing on the map opened by %+v — the id of its entry is the only address that "+
			"tells two projects of the same name apart", mapped.Params)
	}

	// A conversation that ran outside the map has no entry to name, and the
	// name is all there is: the executor looks the directory up itself.
	stray := got.Sent[1]
	if _, ok := stray.Params["project"]; ok {
		t.Errorf("a row the map does not know carried %+v", stray.Params)
	}
	if stray.Target != "lab" {
		t.Errorf("a row the map does not know went out as %q", stray.Target)
	}
}
