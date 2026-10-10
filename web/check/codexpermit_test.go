package check

import (
	"os"
	"strings"
	"testing"
)

// The card of a codex request whose tool is a word of the panel's says in
// words what it asks: a grant is more access than the sandbox gives, and what
// an MCP server asks is said to be the server's. A page the server asks to
// open stands under the request as a link that opens in a tab of its own, and
// the request no longer carries it as an address to copy; an address on
// another scheme than the web's own is left as text, never a link to press.
func TestACodexRequestIsTitledInWordsAndItsPageIsALink(t *testing.T) {
	if _, err := os.Stat(webPath("dist/bundle.css")); err != nil {
		t.Skip("web/dist/bundle.css is not built — run make front first")
	}
	type link struct {
		Href   string `json:"href"`
		Target string `json:"target"`
		Rel    string `json:"rel"`
		Text   string `json:"text"`
	}
	type card struct {
		Title  string  `json:"title"`
		Action string  `json:"action"`
		Page   *string `json:"page"`
		Link   *link   `json:"link"`
	}
	var got struct {
		Grant  card `json:"grant"`
		Script card `json:"script"`
		Page   card `json:"page"`
	}
	runFixture(t, "codexpermit.html", &got)

	if got.Grant.Title != "More access than the sandbox gives" || got.Grant.Action != "write /srv/out" || got.Grant.Page != nil {
		t.Errorf("the card of a grant reads %+v, expected it titled in words over what it asks", got.Grant)
	}
	const url = "https://tracker.test/login?next=/reports/quarterly-summary"
	if got.Page.Title != "An MCP server asks" || got.Page.Action != "Sign in to the tracker" {
		t.Errorf("the card of a page reads %q over %q", got.Page.Title, got.Page.Action)
	}
	if got.Page.Link == nil || got.Page.Link.Href != url || got.Page.Link.Text != url || got.Page.Link.Target != "_blank" ||
		!strings.Contains(got.Page.Link.Rel, "noopener") {
		t.Errorf("the page under the request is %+v, expected a link to %s opening in a tab of its own", got.Page.Link, url)
	}
	if got.Script.Title != "An MCP server asks" || got.Script.Link != nil || got.Script.Page == nil ||
		*got.Script.Page != "javascript:alert(1)" {
		t.Errorf("an address on another scheme reads %+v, expected it as text and no link", got.Script)
	}
}
