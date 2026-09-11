package check

import (
	"strings"
	"testing"
)

func TestChatRendersWithoutInnerHTML(t *testing.T) {
	forbidden := []string{"innerHTML", "outerHTML", "dangerouslySetInnerHTML",
		"insertAdjacentHTML", "document.write"}

	for path, body := range srcFiles(t) {
		code := stripComments(body)
		for _, bad := range forbidden {
			if strings.Contains(code, bad) {
				t.Errorf("%s contains %q: the markup of the model's answer has to be built from nodes, "+
					"otherwise the panel runs anything that comes in someone else's text", path, bad)
			}
		}
	}
}

func TestStripCommentsKeepsCodeItOnlyLooksLikeAComment(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"a line comment", "code(); // explanation\nmore();", "code(); \nmore();"},
		{"a block comment", "before /* inside */ after", "before  after"},
		{
			"an asterisk in a string literal does not open a comment",
			`accept: "image/*", multiple: true`,
			`accept: "image/*", multiple: true`,
		},
		{
			"slashes in a template string are markup, not a comment",
			"html`<div>${x}<//>`",
			"html`<div>${x}<//>`",
		},
		{
			"code after an imaginary comment stays where it is",
			"const a = \"image/*\";\nexport function f() {}",
			"const a = \"image/*\";\nexport function f() {}",
		},
	}
	for _, c := range cases {
		if got := stripComments(c.in); got != c.want {
			t.Errorf("%s: stripComments(%q) = %q, expected %q", c.name, c.in, got, c.want)
		}
	}
}

func stripComments(body string) string {
	var out strings.Builder
	var quote byte
	for i := 0; i < len(body); i++ {
		c := body[i]

		if quote != 0 {
			out.WriteByte(c)
			if c == '\\' && i+1 < len(body) {
				i++
				out.WriteByte(body[i])
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}

		if c == '\'' || c == '"' || c == '`' {
			quote = c
			out.WriteByte(c)
			continue
		}

		if c == '/' && i+1 < len(body) {
			if body[i+1] == '/' {
				for i < len(body) && body[i] != '\n' {
					i++
				}
				out.WriteByte('\n')
				continue
			}
			if body[i+1] == '*' {
				end := strings.Index(body[i+2:], "*/")
				if end < 0 {
					break
				}
				i += 2 + end + 1
				continue
			}
		}

		out.WriteByte(c)
	}
	return out.String()
}

func TestChatOpensOnlySafeLinks(t *testing.T) {
	md := srcFiles(t)["src/md.js"]
	if md == "" {
		t.Fatal("src/md.js not found")
	}
	if !strings.Contains(stripComments(md), "^https?:") {
		t.Error("the markdown does not check the link scheme: javascript: from the model's answer becomes clickable")
	}
	if !strings.Contains(md, `rel="noopener noreferrer"`) {
		t.Error("an external link opens without noopener: the opened page gets access to the panel window")
	}
}

func TestChatClosesOnBack(t *testing.T) {
	chat := screenSrc(t, "src/screens/chat.js")
	if chat == "" {
		t.Fatal("src/screens/chat.js not found")
	}
	if !strings.Contains(chat, "useBackClose") {
		t.Error("the chat screen does not catch the system back gesture: in a PWA that closes the whole app")
	}
}

func TestChatAsksByNameNotUUID(t *testing.T) {
	chat := screenSrc(t, "src/screens/chat.js")
	if strings.Contains(chat, "sessionId") {
		t.Error("the chat screen works with sessionId: the phone has to send the name, the id is the service's business")
	}
	if !strings.Contains(chat, "session=${encodeURIComponent(name)}") {
		t.Error("the session name goes into the request unescaped")
	}
}

func TestChatQuoteReplyIsWired(t *testing.T) {
	const chatFile = "src/screens/chat.js"
	screen := screenSrc(t, chatFile)
	if !strings.Contains(screen, `addEventListener("selectionchange"`) {
		t.Errorf("%s: nobody listens to a selection in the feed — the quote-reply bar has nothing to appear on", chatFile)
	}
	if strings.Count(screen, "<${QuoteBar}") < 2 {
		t.Errorf("%s: the quote bar belongs both above the composer and in the output sheet — people quote from both", chatFile)
	}
	composer := jsBlock(t, chatFile, screen, "export function Composer(")
	if !strings.Contains(composer, "insert.text") || !strings.Contains(composer, "withQuote(") {
		t.Errorf("%s: the composer does not take a quote — a tap on the bar inserts nothing", chatFile)
	}
	for _, line := range strings.Split(screen, "\n") {
		if strings.HasPrefix(line, "export const QUOTABLE") && strings.Contains(line, ".mtools") {
			t.Errorf("%s: call cards are quotable — the counter captions end up inside the quote", chatFile)
		}
	}
}

func TestChatShowsWakeupsAsSessionEvents(t *testing.T) {
	const chatFile = "src/screens/chat.js"
	screen := screenSrc(t, chatFile)
	if !strings.Contains(screen, `item.role === "wake"`) {
		t.Errorf("%s: the feed does not know the wake role — a wake-up shows as the owner's bubble", chatFile)
	}
	if !strings.Contains(screen, "wake: [") {
		t.Errorf("%s: the watch list does not know the wake kind — a wake-up is named a command", chatFile)
	}
}
