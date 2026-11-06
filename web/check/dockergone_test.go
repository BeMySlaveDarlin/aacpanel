package check

import (
	"strings"
	"testing"
)

type goneTree struct {
	Chip    string `json:"chip"`
	Tone    string `json:"tone"`
	Dot     string `json:"dot"`
	Maps    int    `json:"maps"`
	Trees   int    `json:"trees"`
	Trouble string `json:"trouble"`
}

func runDockerGone(t *testing.T) (proxy, panel goneTree) {
	t.Helper()
	var got struct {
		Proxy goneTree `json:"proxy"`
		Panel goneTree `json:"panel"`
	}
	runFixture(t, "dockergone.html", &got)
	return got.Proxy, got.Panel
}

// A 502 without the name of the panel on it is a proxy in front of a panel it
// cannot reach — nginx on the domain answers so while the panel is down: the
// link is broken, whatever the agent snapshot says.
func TestATreeRefusedByAProxyIsNoConnection(t *testing.T) {
	proxy, _ := runDockerGone(t)
	if !strings.HasPrefix(proxy.Chip, "No connection") || proxy.Tone != "warn" || proxy.Dot != "off" {
		t.Errorf("a proxy refused the tree, and the chip reads %q, tone %q, dot %q; want No connection, warn, off",
			proxy.Chip, proxy.Tone, proxy.Dot)
	}
}

// The panel itself refusing the tree is docker silent behind it: the link is
// up, so the chip does not send the person to look for a network fault, and
// the screen of the containers says what broke.
func TestDockerSilentBehindThePanelKeepsTheLinkLive(t *testing.T) {
	_, panel := runDockerGone(t)
	if strings.Contains(panel.Chip, "No connection") || panel.Dot != "on" || panel.Tone != "" {
		t.Errorf("docker is silent behind a panel that answers, and the chip reads %q, tone %q, dot %q",
			panel.Chip, panel.Tone, panel.Dot)
	}
	if panel.Trouble != "docker is unavailable" {
		t.Errorf("the containers say %q, not what the panel said of docker", panel.Trouble)
	}
}

// A panel that answers is routed as one that gives the tree: the map of
// addresses is asked for and the nearer leg that answers takes the requests,
// so the sheet of the connection is not left with no map at all.
func TestDockerSilentBehindThePanelStillMapsTheAddresses(t *testing.T) {
	_, panel := runDockerGone(t)
	if panel.Maps == 0 {
		t.Fatalf("docker is silent behind a panel that answers, and the map of addresses was never asked for (%d trees asked)", panel.Trees)
	}
	if panel.Chip != "LAN" {
		t.Errorf("the LAN leg answers, and the chip reads %q, not the leg the requests take", panel.Chip)
	}
}
