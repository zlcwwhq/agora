package post

import "testing"

func TestCheckVote(t *testing.T) {
	for _, v := range []string{VoteUp, VoteDown, VoteNone} {
		if err := CheckVote(v); err != nil {
			t.Errorf("CheckVote(%q) = %v", v, err)
		}
	}
	for _, v := range []string{"", "Up", "+1", "👍", "none "} {
		if err := CheckVote(v); KindOf(err) != Invalid {
			t.Errorf("CheckVote(%q) = %v, want an error of kind Invalid", v, err)
		}
	}
}
