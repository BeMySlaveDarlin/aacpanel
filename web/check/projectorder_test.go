package check

import (
	"reflect"
	"testing"
)

type projectOrderShot struct {
	Order  []string `json:"order"`
	Roots  []string `json:"roots"`
	Folded []string `json:"folded"`
	Alone  []string `json:"alone"`
	Second []string `json:"second"`
	Error  string   `json:"error"`
}

// The phone keeps roots by name, with a run folded under its parent. Inside
// the project's own screen, its named session leads and the others follow
// by their last request, the latest on top.
func TestThePhoneKeepsProjectRowsByName(t *testing.T) {
	var got projectOrderShot
	runFixture(t, "projectorder.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke: %s", got.Error)
	}
	if want := []string{"evirma", "zd-lead", "zd-old", "zd-vm", "zd-run"}; !reflect.DeepEqual(got.Order, want) {
		t.Errorf("the block of the project lists %v, want %v", got.Order, want)
	}
	if want := []string{"evirma", "zd-lead", "zd-old", "zd-vm"}; !reflect.DeepEqual(got.Roots, want) ||
		!reflect.DeepEqual(got.Folded, []string{"zd-run"}) {
		t.Errorf("the block shows %v with %v folded under zd-lead, want %v with [zd-run]", got.Roots, got.Folded, want)
	}
	if want := []string{"zd-b", "zd-a"}; !reflect.DeepEqual(got.Alone, want) {
		t.Errorf("without the project's own session the block lists %v, want %v", got.Alone, want)
	}
	if want := []string{"evirma-2", "zd-a"}; !reflect.DeepEqual(got.Second, want) {
		t.Errorf("a second run of the project's own session stands at %v, want first: %v", got.Second, want)
	}
}
