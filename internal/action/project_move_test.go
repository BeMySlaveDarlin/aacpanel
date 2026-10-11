package action

import "testing"

func TestProjectMoveChecksItsPathsAndTransferIdentity(t *testing.T) {
	valid := Request{ID: "a1-apply", Kind: ProjectMove, Target: "/srv/proj/shop", Move: &ProjectMoveSpec{ID: "a1", From: "/home/u/.claude", To: "/home/u/.claude-work"}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Request){
		func(r *Request) { r.Move = nil },
		func(r *Request) { r.Move.ID = "../other" },
		func(r *Request) { r.Move.From = "relative" },
		func(r *Request) { r.Move.To = r.Move.From },
		func(r *Request) { r.Move.To = "/home/u/../other" },
		func(r *Request) { r.Move.Finish = "delete" },
		func(r *Request) { r.Kind = ProjectCreate },
	} {
		r := valid
		move := *valid.Move
		r.Move = &move
		change(&r)
		if err := r.Validate(); err == nil {
			t.Fatalf("accepted invalid transfer: %+v %+v", r, r.Move)
		}
	}
}
