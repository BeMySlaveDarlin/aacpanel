package check

import (
	"strings"
	"testing"
)

func TestThoughtWithTextIsReadAsTextNotAsACounter(t *testing.T) {
	src := srcFiles(t)["src/screens/chat/rows.js"]
	if src == "" {
		t.Fatal("src/screens/chat/rows.js not found")
	}

	at := strings.Index(src, `item.role === "mind"`)
	if at < 0 {
		t.Fatal("there is no branch for a thought with text at all — the thought falls into " +
			"the common reply rendering and stands there as an answer bubble")
	}
	row := src[at:]
	if end := strings.Index(row, "\n    if (item.role"); end > 0 {
		row = row[:end]
	}

	if !strings.Contains(row, "render(item.text)") {
		t.Error("the thought text is not rendered through markdown — yet it is the same prose " +
			"as the model's answer")
	}
	if !strings.Contains(row, "msg mmind") {
		t.Error("the thought row has lost its class: it has neither the muting nor the origin " +
			"mark, and is indistinguishable from the model's answer")
	}
	if !strings.Contains(row, "item.cut") {
		t.Error("a truncated thought says nothing about the truncation — the shortened text " +
			"lies about what the model was thinking")
	}

	mark := cssBlock(t, cssSrc(t), ".msg.mmind")
	if !strings.Contains(mark, "color:") {
		t.Error("the thought in the feed has lost its tone — next to the model's answer the " +
			"two can no longer be told apart")
	}
}
