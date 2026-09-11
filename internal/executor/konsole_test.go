package executor

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"aacpanel/internal/action"
)

const idleScreen = `s "  Got it, writing the fix.\n\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200 aacpanel \342\224\200\n\342\235\257 tell me when they are done\n\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\n  \342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\n   Opus 5 (1M context)"`

const heldScreen = `s "  Got it, writing the fix.\n\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200 aacpanel \342\224\200\n\342\235\257 [Image #1]here is what is on the screen\n\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\n  \342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\n   Opus 5 (1M context)"`

const echoedScreen = `s "\342\235\257 [Image #1]here is what is on the screen\n  Got it, writing the fix.\n\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200 aacpanel \342\224\200\n\342\235\257 tell me when they are done\n\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\n  \342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\n   Opus 5 (1M context)"`

const sentScreen = `s "\342\235\257 check the stack logs and say what crashed\n  Looking.\n\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200 aacpanel \342\224\200\n\342\235\257 tell me when they are done\n\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\n  \342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\342\224\200\n   Opus 5 (1M context)"`

func TestComposerReadFromRealScreen(t *testing.T) {
	sent := composerMark("here is what is on the screen\n/home/u/.local/share/aacpanel-exec/files/shot.png")

	cases := []struct {
		name                  string
		out                   string
		mark                  string
		held, onScreen, known bool
	}{
		{
			name: "the reply went out: the composer holds the hint, not the reply",
			out:  idleScreen, mark: sent, held: false, onScreen: false, known: true,
		},
		{
			name: "our own reply in the screen history is no longer the composer",
			out:  echoedScreen, mark: sent, held: false, onScreen: false, known: true,
		},
		{
			name: "the reply of a free session is visible in the conversation, not in the composer",
			out:  sentScreen, mark: composerMark("check the stack logs and say what crashed"),
			held: false, onScreen: true, known: true,
		},
		{
			name: "the reply stands in the composer as an attachment",
			out:  heldScreen, mark: sent, held: true, onScreen: true, known: true,
		},
		{
			name: "the bus answer did not parse — so we do not know",
			out:  "no such object", mark: sent, held: false, known: false,
		},
		{
			name: "a screen with no composer — we do not know either",
			out:  `s "just some command output\nwithout a single rule"`, mark: sent, held: false, known: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			screen, ok := busctlString(c.out)
			if !ok {
				if c.known {
					t.Fatalf("the bus answer did not parse: %q", c.out)
				}
				return
			}
			st, known := composerStateOf(screen, c.mark)
			if st.held != c.held || known != c.known {
				t.Errorf("held=%v known=%v, expected %v and %v", st.held, known, c.held, c.known)
			}
			if st.onScreen != c.onScreen {
				t.Errorf("on screen=%v, expected %v", st.onScreen, c.onScreen)
			}
		})
	}
}

func TestComposerMarkSurvivesWrapping(t *testing.T) {
	text := "check the stack logs and say what crashed there"
	wrapped := "❯ check the stack logs and say\n  what crashed there"
	if !strings.Contains(squeeze(wrapped), composerMark(text)) {
		t.Errorf("the mark was not found in the wrapped line: %q", composerMark(text))
	}
}

func TestLiveKonsolePaste(t *testing.T) {
	spec := os.Getenv("AACP_LIVE_TAB")
	if spec == "" {
		t.Skip("no live tab is named: AACP_LIVE_TAB=org.kde.konsole-<pid>:/Sessions/<N>")
	}
	service, path, ok := strings.Cut(spec, ":")
	if !ok {
		t.Fatalf("AACP_LIVE_TAB=%q, expected <service>:<path>", spec)
	}
	text := os.Getenv("AACP_LIVE_TEXT")
	if text == "" {
		t.Fatal("AACP_LIVE_TEXT is empty: there is nothing to send")
	}
	tab := konsoleTab{Service: service, Path: path, Bus: os.Getenv("DBUS_SESSION_BUS_ADDRESS")}

	start := time.Now()
	confirmed, err := pasteAndSend(t.Context(), tab, text, nil)
	t.Logf("pasteAndSend: confirmed=%v error=%v in %s", confirmed, err, time.Since(start).Round(time.Millisecond))
	if err != nil {
		t.Fatalf("the reply was not delivered: %v", err)
	}
	if !confirmed {
		t.Error("the send is unconfirmed — on a live konsole the screen has to be readable")
	}
}

func TestLiveSessionFile(t *testing.T) {
	name := os.Getenv("AACP_LIVE_SESSION")
	path := os.Getenv("AACP_LIVE_FILE")
	if name == "" || path == "" {
		t.Skip("no live session is named: AACP_LIVE_SESSION and AACP_LIVE_FILE")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the file was not read: %v", err)
	}
	sock := filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), "aacpanel-exec", "sock")
	client := action.NewClient(sock, 2*time.Minute)

	start := time.Now()
	resp, err := client.Do(t.Context(), action.Request{
		ID:     "live-" + strconv.FormatInt(time.Now().UnixNano(), 36),
		Kind:   action.SessionFile,
		Target: name,
		Device: "live check",
		Text:   os.Getenv("AACP_LIVE_TEXT"),
		Files:  []action.File{{Name: filepath.Base(path), Data: data}},
	})
	t.Logf("the executor answered in %s: ok=%v detail=%q err=%v",
		time.Since(start).Round(time.Millisecond), resp.OK, resp.Detail, err)
	if err != nil {
		t.Fatalf("the request did not go through: %v", err)
	}
	if !resp.OK {
		t.Fatalf("the executor refused: %s", resp.Error)
	}
	if strings.Contains(resp.Detail, "nothing to confirm") {
		t.Error("the send is unconfirmed — on a live konsole the screen has to be readable")
	}
}
