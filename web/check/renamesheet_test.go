package check

import (
	"strings"
	"testing"
)

// /rename opens the panel's sheet; a name the host cannot take or one another
// live session answers to is not saved, a sound one is saved with the sheet's
// own button, and the open conversation follows the session to its new name.
func TestRenameSheetSavesASoundNameAndTheScreenFollows(t *testing.T) {
	var got struct {
		Title        string   `json:"title"`
		Value        string   `json:"value"`
		SameOff      bool     `json:"sameOff"`
		SpaceOff     bool     `json:"spaceOff"`
		SpaceWhy     string   `json:"spaceWhy"`
		TakenOff     bool     `json:"takenOff"`
		TakenWhy     string   `json:"takenWhy"`
		GoodOff      bool     `json:"goodOff"`
		Sent         []string `json:"sent"`
		Confirmation bool     `json:"confirmation"`
		Before       []string `json:"before"`
		After        []string `json:"after"`
		Error        string   `json:"error"`
	}
	runFixture(t, "renamesheet.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke: %s", got.Error)
	}
	if got.Title != "Rename the session" || got.Value != "atlas" || !got.SameOff {
		t.Errorf("the sheet opened as %q with %q (save off on the same name: %v)", got.Title, got.Value, got.SameOff)
	}
	if !got.SpaceOff || !strings.Contains(got.SpaceWhy, "latin") {
		t.Errorf("a name with a space can be saved (%v): %q", !got.SpaceOff, got.SpaceWhy)
	}
	if !got.TakenOff || !strings.Contains(got.TakenWhy, "already") {
		t.Errorf("a taken name can be saved (%v): %q", !got.TakenOff, got.TakenWhy)
	}
	if got.GoodOff || len(got.Sent) != 1 || got.Sent[0] != `session.rename:atlas:{"name":"atlas-pilot"}` {
		t.Errorf("a sound name went out as %v (save off: %v)", got.Sent, got.GoodOff)
	}
	if got.Confirmation {
		t.Error("the saved name was asked about a second time")
	}
	if strings.Join(got.Before, ",") != "atlas" || !strings.Contains(strings.Join(got.After, ","), "atlas-pilot") {
		t.Errorf("the conversation asked by %v, then by %v — it did not follow the renamed session", got.Before, got.After)
	}
}
