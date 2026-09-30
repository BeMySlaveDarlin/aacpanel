package check

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// A toast is a note about an action just taken. It goes away by itself, a
// newer one gets its full time, and the page put away or a screen left takes
// it down: a note that stays hangs over screens it has nothing to do with.
func TestToastGoesAwayByItself(t *testing.T) {
	files := srcFiles(t)
	host := files["src/ui/toasts.js"]
	view := files["src/ui/toast.js"]
	if host == "" || view == "" {
		t.Fatal("src/ui/toasts.js or src/ui/toast.js not found — the test is looking in the wrong place")
	}

	lifetime := regexp.MustCompile(`export const LIFETIME = (\d+);`).FindStringSubmatch(host)
	if lifetime == nil {
		t.Fatal("toasts.js does not export LIFETIME — the fixture cannot know how long to wait")
	}
	plain, _ := strconv.Atoi(lifetime[1])
	if plain < 2500 || plain > 4000 {
		t.Errorf("a toast lives %d ms — under two and a half seconds two lines go unread, over four it hangs over the screen", plain)
	}
	withAct := regexp.MustCompile(`export const ACT_LIFETIME = (\d+);`).FindStringSubmatch(host)
	if withAct == nil {
		t.Fatal("toasts.js does not export ACT_LIFETIME — a note with a way back goes at the plain note's time")
	}
	if ms, _ := strconv.Atoi(withAct[1]); ms <= plain || ms > 6000 {
		t.Errorf("a note with a way back lives %d ms against %d ms of a plain one — it has to outlast it, and over six seconds it hangs into the next screen", ms, plain)
	}

	show := funcBody(t, host, "const show = useCallback(")
	for _, want := range []struct{ code, harm string }{
		{"clearTimeout(timer.current)", "a second show leaves the first clock running, and the newer toast goes at the older one's time"},
		{"timer.current = setTimeout(hide, act ? ACT_LIFETIME : LIFETIME)", "the clock is not set where the toast is shown"},
	} {
		if !strings.Contains(show, want.code) {
			t.Errorf("show in toasts.js has no %q — %s", want.code, want.harm)
		}
	}
	if strings.Contains(view, "setTimeout") || strings.Contains(view, "useEffect") {
		t.Error("toast.js keeps a clock of its own — an effect is re-armed by whatever re-renders it, and a clock re-armed never rings")
	}
	if !strings.Contains(host, `document.addEventListener("visibilitychange", away)`) {
		t.Error("toasts.js does not watch the page being put away — whoever comes back finds a note about something long done")
	}

	for _, shell := range []struct{ file, deps string }{
		{"src/mobile/shell.js", "[tab, page, hideToast]"},
		{"src/desktop/shell.js", "[section, chat, hideToast]"},
	} {
		src := files[shell.file]
		if !strings.Contains(src, "const hideToast = useToastHide();") ||
			!strings.Contains(src, "useEffect(() => { hideToast(); }, "+shell.deps+");") {
			t.Errorf("%s does not take the toast down on %s — the note about one screen's action hangs over the next", shell.file, shell.deps)
		}
	}
}

// The host and the gate under a real engine: a screen re-rendering every
// hundred milliseconds under the toast, two shows, the note of a confirmed
// action, and the page put away.
func TestToastUnderChrome(t *testing.T) {
	var got struct {
		Lifetime int     `json:"lifetime"`
		Series   [][]any `json:"series"`
		Gate     struct {
			On    bool   `json:"on"`
			Text  string `json:"text"`
			Later bool   `json:"later"`
		} `json:"gate"`
		Hidden struct {
			Before bool `json:"before"`
			After  bool `json:"after"`
		} `json:"hidden"`
		Renders int `json:"renders"`
	}
	runFixture(t, "toast.html", &got)
	if got.Lifetime < 2500 || got.Lifetime > 4000 {
		t.Fatalf("LIFETIME is %d ms in the engine", got.Lifetime)
	}
	if got.Renders < 20 {
		t.Errorf("the screen under the toast re-rendered %d times — the fixture does not press the host the way the application does", got.Renders)
	}

	second := -1
	for _, s := range got.Series {
		if len(s) > 2 {
			second = int(s[0].(float64))
		}
	}
	if second < 0 {
		t.Fatal("the fixture never showed the second toast")
	}
	for _, s := range got.Series {
		at, on := int(s[0].(float64)), s[1].(bool)
		switch {
		case at < second+got.Lifetime-400 && !on:
			t.Errorf("at %d ms the toast is off, though the second show at %d ms gave it %d ms more", at, second, got.Lifetime)
		case at > second+got.Lifetime+400 && on:
			t.Errorf("at %d ms the toast is still on, %d ms after the second show — it does not go away by itself", at, at-second)
		}
	}
	if !got.Gate.On || !strings.HasPrefix(got.Gate.Text, "Session infra-rework is up") {
		t.Errorf("the note of a confirmed action did not show: on=%v text=%q", got.Gate.On, got.Gate.Text)
	}
	if got.Gate.Later {
		t.Error("the note of the confirmed action is still on after its time")
	}
	if !got.Hidden.Before || got.Hidden.After {
		t.Errorf("the page put away: toast on before=%v after=%v — it should be down", got.Hidden.Before, got.Hidden.After)
	}
}

// On a phone a note stands over the head, clear of the composer at the
// bottom of a conversation, where the finger is busy while it shows; at a desk
// it keeps the bottom right corner, away from the head and from the composer.
func TestToastStandsClearOfTheComposer(t *testing.T) {
	type place struct {
		Top    float64 `json:"top"`
		Bottom float64 `json:"bottom"`
		Left   float64 `json:"left"`
		Right  float64 `json:"right"`
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	}
	var phone, desk place
	runFixture(t, "toastplace.html", &phone)
	if phone.Height == 0 || phone.Top > 24 || phone.Bottom > phone.Height/4 {
		t.Errorf("on a phone the note stands at %v..%v of %v px — it is not over the head, and at the bottom it covers the composer",
			phone.Top, phone.Bottom, phone.Height)
	}
	runWideFixture(t, "toastplace.html", &desk)
	if desk.Height == 0 || desk.Height-desk.Bottom > 24 || desk.Width-desk.Right > 24 || desk.Left < desk.Width/2 ||
		desk.Top < desk.Height/2 {
		t.Errorf("at a desk the note stands at %v..%v × %v..%v of %v×%v px — it left the bottom right corner",
			desk.Left, desk.Right, desk.Top, desk.Bottom, desk.Width, desk.Height)
	}
}
