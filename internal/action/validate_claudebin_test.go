package action

import (
	"strings"
	"testing"
)

func TestProjectChecksClaudeBin(t *testing.T) {
	open := func(bin string) Request {
		return Request{ID: "1", Kind: SessionOpen, Target: "aacpanel",
			Project: &Project{Path: "/srv/proj/aacpanel", Session: "aacpanel", ClaudeBin: bin}}
	}

	if err := open("").Validate(); err != nil {
		t.Errorf("an empty path is rejected, and that is the default for every contour: %v", err)
	}

	for name, bin := range map[string]string{
		"relative":         "claude",
		"with a newline":   "/srv/claude\nrm -rf /",
		"over the limit":   "/srv/" + strings.Repeat("a", pathMax),
		"with a null byte": "/srv/claude\x00",
	} {
		if err := open(bin).Validate(); err == nil {
			t.Errorf("%s path accepted: %q", name, bin)
		}
	}

	if err := open("/srv/tools/claude-contours/bin/claude").Validate(); err != nil {
		t.Errorf("an honest path is rejected: %v", err)
	}
}
