package check

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTerminalInputStopsAtGone(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not found: sending input is run by the engine, not by reading the source")
	}
	path, err := filepath.Abs(filepath.Join(webDir, "src", "screens", "chat", "input.js"))
	if err != nil {
		t.Fatal(err)
	}
	script := `
import { sender } from ` + jsString("file://"+path) + `;
const run = async (statuses) => {
    const sent = [];
    let step = 0;
    const f = async (url) => {
        sent.push(url);
        const status = statuses[Math.min(step++, statuses.length - 1)];
        if (status === "offline") throw new TypeError("Failed to fetch");
        return { status, ok: status === 204 };
    };
    const input = sender({ fetch: f });
    await input.send("before the bridge");
    input.open("bridge-1");
    for (let i = 0; i < 4; i += 1) await input.send("byte");
    return { sent, closed: input.closed };
};
const out = {
    live: await run([204]),
    gone: await run([410]),
    hiccup: await run([502]),
    dropped: await run(["offline"]),
    stopped: await (async () => {
        const sent = [];
        const input = sender({ fetch: async (url) => { sent.push(url); return { status: 204, ok: true }; } });
        input.open("bridge-1");
        await input.send("byte");
        input.stop();
        await input.send("after the stream ended");
        return { sent, closed: input.closed };
    })(),
};
process.stdout.write(JSON.stringify(out));
`
	cmd := exec.Command(node, "--input-type=module", "-e", script)
	raw, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("node: %v\n%s", err, stderr)
	}
	var got map[string]struct {
		Sent   []string `json:"sent"`
		Closed bool     `json:"closed"`
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("node output: %v: %s", err, raw)
	}
	if n := len(got["live"].Sent); n != 4 {
		t.Errorf("a live bridge took %d requests out of four bytes — before the id there is nowhere to send, after it there is", n)
	}
	for _, url := range got["live"].Sent {
		if url != "/api/term/input?id=bridge-1" {
			t.Errorf("input went to %q", url)
		}
	}
	if n := len(got["gone"].Sent); n != 1 {
		t.Errorf("after \"410 — the terminal is gone\" another %d requests went out: retrying into a closed bridge is pointless, and mouse movement sends them by the dozen", n-1)
	}
	if !got["gone"].Closed {
		t.Error("the bridge answered 410, yet the sender still thinks it is alive")
	}
	if n := len(got["hiccup"].Sent); n != 4 {
		t.Errorf("a passing failure buried the sender: %d out of four went out", n)
	}
	if n := len(got["dropped"].Sent); n != 4 {
		t.Errorf("a network failure buried the sender: %d out of four went out", n)
	}
	if n := len(got["stopped"].Sent); n != 1 {
		t.Errorf("the stream is over, yet input keeps going out: %d requests", n)
	}
}

func TestTerminalSendsThroughTheSender(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(webDir, "src", "screens", "chat", "term.js"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if !strings.Contains(body, "sender(") {
		t.Error("the terminal screen sends input itself, past the sender that knows about a closed bridge")
	}
	if strings.Contains(body, "fetch(`/api/term/input") || strings.Contains(body, `fetch("/api/term/input`) {
		t.Error("the terminal screen has an input request of its own: the rule about 410 lives in one place")
	}
}
