package action

import (
	"strings"
	"testing"
)

func TestProjectChecksConfigDir(t *testing.T) {
	open := func(dir string) Request {
		return Request{ID: "1", Kind: SessionOpen, Target: "aacpanel",
			Project: &Project{Path: "/srv/proj/aacpanel", Session: "aacpanel", ConfigDir: dir}}
	}

	if err := open("").Validate(); err != nil {
		t.Errorf("an empty directory is rejected, and that is the default for every install: %v", err)
	}

	for name, dir := range map[string]string{
		"relative":         ".claude",
		"with a newline":   "/home/u/.claude\nrm -rf /",
		"over the limit":   "/home/" + strings.Repeat("a", pathMax),
		"with a null byte": "/home/u/.claude\x00",
	} {
		if err := open(dir).Validate(); err == nil {
			t.Errorf("%s directory accepted: %q", name, dir)
		}
	}

	if err := open("/home/u/.claude-contours/work").Validate(); err != nil {
		t.Errorf("an honest directory is rejected: %v", err)
	}
}
