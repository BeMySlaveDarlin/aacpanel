package check

import (
	"os"
	"reflect"
	"testing"
)

// contourPlace is a page of the phone or a section of the desktop column: its
// contour, how many sessions its tab counts and the sessions it holds.
type contourPlace struct {
	Name     string   `json:"name"`
	Count    int      `json:"count"`
	Sessions []string `json:"sessions"`
}

// A session no contour of the map claims stands in the personal contour —
// the one the map marks default — on both lists, wherever the map puts it.
//
// The map lists the work contour Acme first and the personal one second. A
// session that names no contour and lives outside every project is on the
// page of Personal on the phone, counted on its tab, and in the section of
// Personal in the desktop column. Taken for the first contour instead, it
// would stand among the work of Acme. A map that marks no contour puts it in
// the first: nothing else says which one is personal.
func TestASessionNoContourClaimsStandsInThePersonalOne(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	marked := []contourPlace{
		{"Acme", 1, []string{"shop"}},
		{"Personal", 2, []string{"notes", "stray"}},
	}
	unmarked := []contourPlace{
		{"Acme", 2, []string{"shop", "stray"}},
		{"Personal", 1, []string{"notes"}},
	}
	for _, screen := range []struct {
		name string
		wide bool
		run  func(*testing.T, string, any)
	}{
		{"phone", false, runFixture},
		{"desktop", true, runWideFixture},
	} {
		t.Run(screen.name, func(t *testing.T) {
			var got struct {
				Wide  bool                      `json:"wide"`
				Cases map[string][]contourPlace `json:"cases"`
			}
			screen.run(t, "personalpage.html", &got)
			if got.Wide != screen.wide {
				t.Fatalf("the fixture drew the %s list on a screen it takes for wide=%v", screen.name, got.Wide)
			}
			for key, want := range map[string][]contourPlace{"marked": marked, "unmarked": unmarked} {
				if screen.wide {
					// The column counts no sessions in a heading.
					want = append([]contourPlace(nil), want...)
					for i := range want {
						want[i].Count = 0
					}
				}
				if !reflect.DeepEqual(got.Cases[key], want) {
					t.Errorf("the %s with the map %s lays the sessions out as\n%v\nexpected\n%v", screen.name, key, got.Cases[key], want)
				}
			}
		})
	}
}
