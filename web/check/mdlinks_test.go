package check

import (
	"strings"
	"testing"
)

// An address in an answer is a link wherever it stands: set in bold, in
// italics, or as a markdown link inside bold. Only code keeps it as text, to
// be copied. A markdown link goes where its address says, with or without a
// space before the address: a path opens its file as a path in code does, a
// web address opens in a tab, and any other scheme stays words.
func TestAnAddressIsALinkInBoldAndItalicsToo(t *testing.T) {
	type became struct {
		Text  string `json:"text"`
		Tag   string `json:"tag"`
		Href  string `json:"href"`
		Opens string `json:"opens"`
	}
	var got struct {
		Links      []string `json:"links"`
		InBold     int      `json:"inBold"`
		InItalic   int      `json:"inItalic"`
		CodeLinked bool     `json:"codeLinked"`
		Each       []became `json:"each"`
	}
	runFixture(t, "mdlinks.html", &got)
	want := "http://192.168.1.20:8093/board.html,https://b.example/y,https://c.example/z"
	if strings.Join(got.Links, ",") != want {
		t.Errorf("the answer links %v, expected %s", got.Links, want)
	}
	if got.InBold != 2 || got.InItalic != 1 {
		t.Errorf("%d links in bold and %d in italics — an address set in them is dead words", got.InBold, got.InItalic)
	}
	if got.CodeLinked {
		t.Error("an address in code became a link — code is there to be copied")
	}

	cases := []struct {
		name string
		want became
	}{
		{"[a]( /abs/x.md) — a path after a space opens the file", became{Text: "a", Tag: "code", Opens: "/abs/x.md"}},
		{"[a](/abs/x.md) — a path opens the file", became{Text: "a", Tag: "code", Opens: "/abs/x.md"}},
		{"[a](https://x) — a web address is a link", became{Text: "a", Tag: "a", Href: "https://x"}},
		{"[a](javascript:x) — another scheme stays words", became{Text: "a (javascript:x)"}},
	}
	if len(got.Each) != len(cases) {
		t.Fatalf("%d links drawn for %d written: %+v", len(got.Each), len(cases), got.Each)
	}
	for i, c := range cases {
		if got.Each[i] != c.want {
			t.Errorf("%s: drawn as %+v, expected %+v", c.name, got.Each[i], c.want)
		}
	}
}
