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

// In a project the session named after it comes first whatever it is doing,
// by the same rule that tells it is the project's own, and the rest follow by
// their last request, the latest on top.
func TestTheProjectsOwnSessionLeadsItsBlock(t *testing.T) {
	var got projectOrderShot
	runFixture(t, "projectorder.html", &got)
	if got.Error != "" {
		t.Fatalf("the fixture broke: %s", got.Error)
	}
	if want := []string{"evirma", "zd-run", "zd-vm", "zd-lead", "zd-old"}; !reflect.DeepEqual(got.Order, want) {
		t.Errorf("the block of the project lists %v, want %v", got.Order, want)
	}
	if want := []string{"evirma", "zd-vm", "zd-lead", "zd-old"}; !reflect.DeepEqual(got.Roots, want) ||
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
