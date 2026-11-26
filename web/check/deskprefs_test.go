package check

import "testing"

type resizedColumn struct {
	Value     float64 `json:"value"`
	State     float64 `json:"state"`
	Selecting string  `json:"selecting"`
	Released  string  `json:"released"`
}

func checkColumnDrags(t *testing.T, got []resizedColumn, want []float64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("read %d drag states, expected %d: %+v", len(got), len(want), got)
	}
	for i, width := range want {
		if got[i].Value != width || got[i].State != width {
			t.Errorf("drag %d kept %.3frem in style and %.3frem in state, expected %.3frem", i, got[i].Value, got[i].State, width)
		}
		if got[i].Selecting != "none" || got[i].Released != "" {
			t.Errorf("drag %d left text selection as %q during the drag and %q after it", i, got[i].Selecting, got[i].Released)
		}
	}
}

func TestDesktopRailPreferencesAndSharedJournal(t *testing.T) {
	var got struct {
		Navigation struct {
			Labels      []bool   `json:"labels"`
			Journal     []bool   `json:"journal"`
			OnlyJournal []string `json:"onlyJournal"`
			Tips        []string `json:"tips"`
			Devices     string   `json:"devices"`
		} `json:"navigation"`
		Error string `json:"error"`
	}
	runWideFixture(t, "deskplace.html", &got)
	if got.Error != "" {
		t.Fatal(got.Error)
	}
	nav := got.Navigation
	if len(nav.Labels) != 4 {
		t.Fatalf("read compact states %v, expected the first visit, hide, reload and show after reload", nav.Labels)
	}
	for i, want := range []bool{false, true, true, false} {
		if nav.Labels[i] != want {
			t.Errorf("compact state %d is %v, expected %v", i, nav.Labels[i], want)
		}
	}
	if !equalStrings(nav.OnlyJournal, []string{"Action journal"}) {
		t.Errorf("a section without panels offers %v, expected only Journal", nav.OnlyJournal)
	}
	if len(nav.Journal) != 4 {
		t.Fatalf("read Journal states %v, expected open on Home, Containers, Home and closed", nav.Journal)
	}
	for i, ok := range nav.Journal {
		if !ok {
			t.Errorf("Journal state %d failed to open, follow the section or close", i)
		}
	}
	if nav.Devices != "Devices" {
		t.Errorf("You → Devices opened %q", nav.Devices)
	}
	for i, tip := range nav.Tips {
		if tip == "" {
			t.Errorf("navigation button %d has no tip naming what it opens", i)
		}
	}
}

func TestDesktopSectionColumnResizes(t *testing.T) {
	var got struct {
		Navigation struct {
			Widths       []resizedColumn `json:"widths"`
			Reload       float64         `json:"widthReload"`
			Reset        float64         `json:"widthReset"`
			SharedWidths []float64       `json:"sharedWidths"`
		} `json:"navigation"`
		Error string `json:"error"`
	}
	runWideFixture(t, "deskplace.html", &got)
	if got.Error != "" {
		t.Fatal(got.Error)
	}
	checkColumnDrags(t, got.Navigation.Widths, []float64{24, 32, 16, 23})
	if got.Navigation.Reload != 23 || got.Navigation.Reset != 19.5 {
		t.Errorf("the column reloaded at %.3frem and reset to %.3frem", got.Navigation.Reload, got.Navigation.Reset)
	}
	if len(got.Navigation.SharedWidths) != 3 {
		t.Fatalf("read shared widths %v, expected Containers, Machine and Sessions", got.Navigation.SharedWidths)
	}
	for i, width := range got.Navigation.SharedWidths {
		if width != 23 {
			t.Errorf("section %d forgot the width: %.3frem", i, width)
		}
	}
}

func TestDesktopFilesColumnResizes(t *testing.T) {
	var got struct {
		Files struct {
			Initial *float64        `json:"initial"`
			Widths  []resizedColumn `json:"widths"`
			Half    resizedColumn   `json:"half"`
			Reload  float64         `json:"reload"`
			Reset   *float64        `json:"reset"`
		} `json:"files"`
		Error string `json:"error"`
	}
	runWideFixture(t, "deskplace.html", &got)
	if got.Error != "" {
		t.Fatal(got.Error)
	}
	checkColumnDrags(t, got.Files.Widths, []float64{24, 36, 14})
	checkColumnDrags(t, []resizedColumn{got.Files.Half}, []float64{20})
	if got.Files.Initial != nil || got.Files.Reset != nil {
		t.Errorf("Files overrode its automatic width initially or after reset: %+v", got.Files)
	}
	if got.Files.Reload != 24 {
		t.Errorf("Files reloaded at %.3frem, expected the saved 24rem", got.Files.Reload)
	}
}
