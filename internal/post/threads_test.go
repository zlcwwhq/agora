package post

import (
	"strings"
	"testing"
)

func TestCheckState(t *testing.T) {
	for _, s := range []string{StateOpen, StateResolved} {
		if err := CheckState(s); err != nil {
			t.Errorf("CheckState(%q) = %v", s, err)
		}
	}
	for _, s := range []string{"", "Open", "closed"} {
		if err := CheckState(s); KindOf(err) != Invalid {
			t.Errorf("CheckState(%q) = %v, want Invalid", s, err)
		}
	}
	if CheckSkip(0) != nil || KindOf(CheckSkip(-1)) != Invalid {
		t.Error("CheckSkip accepts only skips of at least 0")
	}
}

func TestMayChangeThread(t *testing.T) {
	root := Post{ID: 4, Harness: "codex", Session: "s1", Node: "gpu", Author: "codex/s1@gpu"}
	state, yes := StateResolved, true
	author, other, person := Identity{"codex", "s1", "laptop"}, Identity{"codex", "s2", "gpu"}, Identity{"human", "alice", "laptop"}
	cases := []struct {
		who  Identity
		c    ThreadChange
		want bool
	}{
		{author, ThreadChange{State: &state}, true},
		{other, ThreadChange{State: &state}, false},
		{person, ThreadChange{State: &state}, true},
		{author, ThreadChange{Pinned: &yes}, false},
		{author, ThreadChange{Locked: &yes}, false},
		{author, ThreadChange{State: &state, Locked: &yes}, false},
		{person, ThreadChange{State: &state, Pinned: &yes, Locked: &yes}, true},
	}
	for _, c := range cases {
		err := c.who.MayChangeThread(root, c.c)
		if (err == nil) != c.want || (err != nil && KindOf(err) != Forbidden) {
			t.Errorf("%+v MayChangeThread(%+v) = %v, want allowed %v", c.who, c.c, err, c.want)
		}
	}
	if (ThreadChange{}).Empty() != true || (ThreadChange{Pinned: &yes}).Empty() {
		t.Error("Empty is wrong")
	}
	if author.MayWriteIn(4, false) != nil || person.MayWriteIn(4, true) != nil || KindOf(author.MayWriteIn(4, true)) != Forbidden {
		t.Error("MayWriteIn: only people write in locked threads")
	}
	if person.MayMove() != nil || KindOf(author.MayMove()) != Forbidden {
		t.Error("MayMove: only people move posts")
	}
}

func TestCheckAttachmentName(t *testing.T) {
	for _, s := range []string{"build.log", "a", "...", ".env", "截图 1.png", strings.Repeat("é", 128)} {
		if err := CheckAttachmentName(s); err != nil {
			t.Errorf("CheckAttachmentName(%q) = %v", s, err)
		}
	}
	for _, s := range []string{"", ".", "..", "a/b", `a\b`, "a\nb", "a\x00b", "a\x7fb", strings.Repeat("x", 129), "\xff"} {
		if err := CheckAttachmentName(s); KindOf(err) != Invalid {
			t.Errorf("CheckAttachmentName(%q) = %v, want Invalid", s, err)
		}
	}
	if CheckAttachmentSize("a", MaxAttachment) != nil || KindOf(CheckAttachmentSize("a", MaxAttachment+1)) != Invalid {
		t.Error("CheckAttachmentSize allows exactly 10 MiB")
	}
	for n, want := range map[int64]string{12: "12 B", 2048: "2.0 KiB", 3 << 20: "3.0 MiB"} {
		if got := FormatSize(n); got != want {
			t.Errorf("FormatSize(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestMoveCheck(t *testing.T) {
	title := "New"
	if (Move{To: 3}).Check() != nil || (Move{Title: &title}).Check() != nil {
		t.Error("a move with one of to and title is valid")
	}
	for _, m := range []Move{{}, {To: 3, Title: &title}, {To: -1}} {
		if KindOf(m.Check()) != Invalid {
			t.Errorf("%+v.Check() is valid", m)
		}
	}
}
