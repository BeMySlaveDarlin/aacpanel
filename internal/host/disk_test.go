package host

import "testing"

func TestDiskReadsRootsDepthAndDirs(t *testing.T) {
	r := readerWith(t, `{"at":1,"projects":{"at":9,"roots":["/srv/proj/"],"depth":4,"dirs":[
		{"path":"/srv/proj/Labs","kind":"folder"},
		{"path":"/srv/proj/Labs/shop","kind":"project","git":true},
		{"path":"/srv/proj/Demo/site","kind":"project","claude":true},
		{"path":"/srv/proj/Link","kind":"link"},
		{"path":"relative/path","kind":"project"},
		{"path":"/srv/proj/odd","kind":"something"}
	]}}`)

	got := r.Disk()
	if got.State != DiskOK {
		t.Fatalf("state %q, expected ok: %+v", got.State, got)
	}
	if got.At != 9 || got.Depth != 4 {
		t.Errorf("the time or the depth is lost: %+v", got)
	}
	if len(got.Roots) != 1 || got.Roots[0] != "/srv/proj" {
		t.Errorf("the root keeps its trailing slash: %v", got.Roots)
	}
	want := map[string]string{
		"/srv/proj/Labs":      DirFolder,
		"/srv/proj/Labs/shop": DirProject,
		"/srv/proj/Demo/site": DirProject,
		"/srv/proj/Link":      DirLink,
	}
	if len(got.Dirs) != len(want) {
		t.Fatalf("%d dirs, expected %d — a relative path and an unknown kind must both drop out: %+v",
			len(got.Dirs), len(want), got.Dirs)
	}
	for _, d := range got.Dirs {
		if want[d.Path] != d.Kind {
			t.Errorf("%s: kind %q, expected %q", d.Path, d.Kind, want[d.Path])
		}
	}
	for _, d := range got.Dirs {
		if d.Path == "/srv/proj/Labs/shop" && !d.Git {
			t.Errorf("the git flag is lost: %+v", d)
		}
		if d.Path == "/srv/proj/Demo/site" && !d.Claude {
			t.Errorf("the claude flag is lost: %+v", d)
		}
	}
}

func TestDiskUnknownOnAnythingOdd(t *testing.T) {
	cases := map[string]string{
		"no block at all — an old agent": `{"at":1,"sessions":[]}`,
		"the block is null":              `{"at":1,"projects":null}`,
		"no roots":                       `{"at":1,"projects":{"depth":4,"dirs":[]}}`,
		"no depth":                       `{"at":1,"projects":{"roots":["/srv/proj"],"dirs":[]}}`,
		"roots are not absolute":         `{"at":1,"projects":{"roots":["proj"],"depth":4,"dirs":[]}}`,
		"the snapshot is not json":       `{"at":1,"projects":`,
	}
	for name, body := range cases {
		got := readerWith(t, body).Disk()
		if got.State != DiskUnknown {
			t.Errorf("%s: state %q, expected unknown: %+v", name, got.State, got)
		}
		if len(got.Dirs) != 0 {
			t.Errorf("%s: a list came along with the «unknown» state: %+v", name, got.Dirs)
		}
	}
}

func TestDiskEmptyIsStillOK(t *testing.T) {
	got := readerWith(t, `{"at":1,"projects":{"at":2,"roots":["/srv/proj"],"depth":4,"dirs":[]}}`).Disk()
	if got.State != DiskOK {
		t.Fatalf("state %q, expected ok: %+v", got.State, got)
	}
}
