package browserlink

import "testing"

func TestAddressBarAcceptsOnlyWebAddresses(t *testing.T) {
	for _, url := range []string{"https://example.org/path?q=a", "http://example.org/", "about:blank"} {
		if err := Navigation(url); err != nil {
			t.Errorf("refused %s: %v", url, err)
		}
	}
	for _, url := range []string{
		"file:///etc/passwd",
		"javascript:alert(1)",
		"data:text/html,hello",
		"chrome://settings",
		"view-source:https://example.org",
		"//example.org",
		"https:",
		"about:config",
		"https://user:pass@example.org",
	} {
		if err := Navigation(url); err == nil {
			t.Errorf("allowed %s", url)
		}
	}
}

func TestInputCannotNameCDPMethods(t *testing.T) {
	for _, kind := range []string{"Runtime.evaluate", "DOM.setFileInputFiles", "Browser.setDownloadBehavior", ""} {
		if err := (Input{Kind: kind}).Check(); err == nil {
			t.Errorf("allowed %q", kind)
		}
	}
}
