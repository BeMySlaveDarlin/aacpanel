package browser

import (
	"context"
	"os"
	"strings"

	"aacpanel/internal/browserlink"

	"golang.org/x/text/language"
)

// Chrome runs with an isolated environment. Read the holder's locale rather
// than its child's C locale, and keep the same language in headers and JS.
// CDP takes bare language tags: Chrome adds the HTTP quality weights itself.
func hostLanguage() string {
	locale := os.Getenv("LC_ALL")
	if locale == "" {
		locale = os.Getenv("LANG")
	}
	locale, _, _ = strings.Cut(locale, ".")
	locale, _, _ = strings.Cut(locale, "@")
	tag, err := language.Parse(strings.ReplaceAll(locale, "_", "-"))
	if err != nil || tag == language.Und {
		return "en-US,en"
	}
	base, _ := tag.Base()
	value := tag.String()
	if value != base.String() {
		value += "," + base.String()
	}
	if base.String() != "en" {
		value += ",en"
	}
	return value
}

// Metrics alone squeeze a desktop site into a phone. UA and client hints must
// agree in both the first request and navigator; changing only the UA string
// leaves Chrome's desktop hints in place. The engine version comes from CDP.
func (b *chrome) emulate(ctx context.Context, sid, kind string, size browserlink.Size) error {
	major, _, _ := strings.Cut(b.version, ".")
	platform, platformVersion, model := "Linux", "", ""
	architecture, navigatorPlatform := "x86", "Linux x86_64"
	osToken, mobileToken := "X11; Linux x86_64", ""
	if size.Mobile {
		platform, platformVersion, model = "Android", "13.0.0", "Pixel 7"
		architecture, navigatorPlatform = "arm", "Linux armv8l"
		osToken, mobileToken = "Linux; Android 13; Pixel 7", " Mobile"
	}
	brands := func(version string) []map[string]string {
		return []map[string]string{
			{"brand": "Chromium", "version": version},
			{"brand": "Google Chrome", "version": version},
		}
	}
	if err := b.call(ctx, sid, "Emulation.setUserAgentOverride", map[string]interface{}{
		"userAgent":      "Mozilla/5.0 (" + osToken + ") AppleWebKit/537.36 (KHTML, like Gecko) Chrome/" + b.version + mobileToken + " Safari/537.36",
		"acceptLanguage": b.language,
		"platform":       navigatorPlatform,
		"userAgentMetadata": map[string]interface{}{
			"brands": brands(major), "fullVersionList": brands(b.version),
			"platform": platform, "platformVersion": platformVersion,
			"architecture": architecture, "model": model, "mobile": size.Mobile,
			"bitness": "64", "wow64": false,
		},
	}, nil); err != nil {
		return err
	}
	// An out-of-process frame inherits its page's metrics; CDP refuses a
	// viewport override there. Before a viewer attaches only identity is known.
	if kind != "page" || size.Width == 0 {
		return nil
	}
	if err := b.call(ctx, sid, "Emulation.setDeviceMetricsOverride", map[string]interface{}{
		"width": size.Width, "height": size.Height,
		"deviceScaleFactor": size.DPR, "mobile": size.Mobile,
	}, nil); err != nil {
		return err
	}
	return b.call(ctx, sid, "Emulation.setTouchEmulationEnabled", map[string]interface{}{
		"enabled": size.Mobile, "maxTouchPoints": 5,
	}, nil)
}
