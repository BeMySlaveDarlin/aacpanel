package check

import (
	"strings"
	"testing"
)

type paperNode struct {
	Bg          string  `json:"bg"`
	BgImage     string  `json:"bgImage"`
	BorderTop   float64 `json:"borderTop"`
	BorderColor string  `json:"borderColor"`
	StandsOn    string  `json:"standsOn"`
}

// grounded is true while the node paints something of its own — a colour that
// is not fully transparent, or an image.
func (n *paperNode) grounded() bool {
	return n != nil && (n.BgImage != "none" || (n.Bg != "" && n.Bg != "rgba(0, 0, 0, 0)"))
}

type paperShot struct {
	Space paperSurvey `json:"space"`
	Sky   paperSurvey `json:"sky"`
}

type paperSurvey struct {
	Opts      *paperNode `json:"opts"`
	OptIdle   *paperNode `json:"optIdle"`
	OptPicked *paperNode `json:"optPicked"`
	OptRule   *paperNode `json:"optRule"`
	Capture   *paperNode `json:"capture"`
	Who       *paperNode `json:"who"`
	Button    *paperNode `json:"button"`
	Reply     *paperNode `json:"reply"`
}

// The paper of a brief is transparent: the document is drawn on the ground of
// the panel rather than on a sheet of its own. Every place that used to stand
// on the paper stands on something now — or is a deliberate hole, and then
// what shows through it has to be the ground and not a rule.
func TestTheControlsOfABriefStandOnSomething(t *testing.T) {
	var shot paperShot
	runFixture(t, "briefpaper.html", &shot)

	for _, theme := range []struct {
		name string
		it   paperSurvey
	}{{"space", shot.Space}, {"sky", shot.Sky}} {
		t.Run(theme.name, func(t *testing.T) {
			for _, c := range []struct {
				what string
				node *paperNode
			}{
				{"the box the answer is written into", theme.it.Capture},
				{"the picker of the session the answers go to", theme.it.Who},
				{"a button of the dock", theme.it.Button},
				{"the text that goes into the session", theme.it.Reply},
			} {
				if c.node == nil {
					t.Errorf("%s was not drawn at all", c.what)
					continue
				}
				if !c.node.grounded() {
					t.Errorf("%s has no ground of its own — it stands on %s and reads as a hole in the page",
						c.what, c.node.StandsOn)
				}
			}

			if theme.it.OptPicked != nil && !theme.it.OptPicked.grounded() {
				t.Error("the option that was picked is painted like the ones that were not")
			}
		})
	}
}

// The rules between the options are borders of their own. A list that paints
// itself the colour of a rule and leaves 1px gaps between opaque children
// draws the same lines — until the children are transparent, and then it is
// one slab with no telling one option from the next.
func TestTheRulesBetweenOptionsAreBorders(t *testing.T) {
	var shot paperShot
	runFixture(t, "briefpaper.html", &shot)

	for _, theme := range []struct {
		name string
		it   paperSurvey
	}{{"space", shot.Space}, {"sky", shot.Sky}} {
		t.Run(theme.name, func(t *testing.T) {
			if theme.it.Opts == nil || theme.it.OptIdle == nil || theme.it.OptRule == nil {
				t.Fatal("the options were not drawn")
			}
			if !theme.it.OptIdle.grounded() && theme.it.Opts.grounded() {
				t.Errorf("the option list paints itself %s%s under options that paint nothing — that is a slab, not a set of rules",
					theme.it.Opts.Bg, strings.TrimSuffix(theme.it.Opts.BgImage, "none"))
			}
			if theme.it.OptRule.BorderTop == 0 {
				t.Error("no rule between one option and the next")
			}
		})
	}
}
