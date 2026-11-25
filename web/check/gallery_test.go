package check

import "testing"

func TestAttachmentsOpenAsAGalleryOnPhoneAndDesktop(t *testing.T) {
	for _, screen := range []struct {
		name string
		run  func(*testing.T, string, any)
	}{{"phone", runFixture}, {"desktop", runWideFixture}} {
		t.Run(screen.name, func(t *testing.T) {
			var got struct {
				Single, Cross, Escape, Backdrop, Back, Down bool
				Many, Arrows, Keys, Swipes                  bool
				ZoomKeepsPicture, Pinch, ZoomResets         bool
				File, FileImage, FileBack, Feed             bool
				TopLayer, Nested, Mixed                     bool
				Title, Dimensions, Save                     string
			}
			screen.run(t, "gallery.html", &got)
			if got.Title != "latency-1.svg" || got.Dimensions != "627 × 460" || got.Save != "latency-1.svg" {
				t.Errorf("attachment metadata and Save: %+v", got)
			}
			for name, ok := range map[string]bool{
				"single attachment": got.Single, "close button": got.Cross, "Escape": got.Escape,
				"backdrop": got.Backdrop, "system back": got.Back, "downward swipe": got.Down,
				"message gallery": got.Many, "arrows": got.Arrows, "keys": got.Keys, "swipes": got.Swipes,
				"zoom keeps the picture": got.ZoomKeepsPicture, "pinch": got.Pinch, "zoom resets": got.ZoomResets,
				"file": got.File, "file image": got.FileImage, "file back": got.FileBack, "feed remains": got.Feed,
				"browser modal layer": got.TopLayer, "sheet remains underneath": got.Nested,
				"pictures and files of one message": got.Mixed,
			} {
				if !ok {
					t.Errorf("%s: %+v", name, got)
				}
			}
		})
	}
}
