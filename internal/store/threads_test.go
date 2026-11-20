package store

import (
	"context"
	"slices"
	"testing"

	"github.com/zlcwwhq/agora/internal/post"
)

func valuePtr[T any](v T) *T { return &v }

// listedIDs returns the ids of threads.
func listedIDs(ts []post.Thread) []int64 {
	ids := []int64{}
	for _, t := range ts {
		ids = append(ids, t.ID)
	}
	return ids
}

// foundIDs returns the ids of the threads of search results.
func foundIDs(rs []post.Result) []int64 {
	ids := []int64{}
	for _, r := range rs {
		ids = append(ids, r.ID)
	}
	return ids
}

func TestChangeThread(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	root := newThread(t, s, codex, "Question", "How?")
	rep := reply(t, s, claude, root, "Like this")
	before := must[post.Show](t)(s.Show(ctx, "b", root)).Thread
	if before.State != post.StateOpen || before.Pinned || before.Locked {
		t.Fatalf("a new thread = %+v", before)
	}

	// The root's author and people resolve and reopen; others don't.
	resolve := post.ThreadChange{State: valuePtr(post.StateResolved)}
	if _, err := s.ChangeThread(ctx, claude, "b", rep, resolve); post.KindOf(err) != post.Forbidden {
		t.Errorf("another author resolves: %v", err)
	}
	th := must[post.Thread](t)(s.ChangeThread(ctx, codex, "b", rep, resolve))
	if th.ID != root || th.State != post.StateResolved {
		t.Errorf("resolved by its author: %+v", th)
	}
	if th := must[post.Thread](t)(s.ChangeThread(ctx, codex, "b", root, resolve)); th.State != post.StateResolved {
		t.Errorf("resolved again: %+v", th)
	}

	// Only people pin and lock, and a request makes all of its changes or
	// none.
	for _, c := range []post.ThreadChange{{Pinned: valuePtr(true)}, {Locked: valuePtr(true)}, {State: valuePtr(post.StateOpen), Pinned: valuePtr(true)}} {
		if _, err := s.ChangeThread(ctx, codex, "b", root, c); post.KindOf(err) != post.Forbidden {
			t.Errorf("the author changes %+v: %v", c, err)
		}
	}
	if th := must[post.Show](t)(s.Show(ctx, "b", root)).Thread; th.State != post.StateResolved || th.Pinned {
		t.Errorf("a refused change changed %+v", th)
	}
	th = must[post.Thread](t)(s.ChangeThread(ctx, alice, "b", root, post.ThreadChange{State: valuePtr(post.StateOpen), Pinned: valuePtr(true), Locked: valuePtr(true)}))
	if th.State != post.StateOpen || !th.Pinned || !th.Locked {
		t.Errorf("a person's change: %+v", th)
	}
	th = must[post.Thread](t)(s.ChangeThread(ctx, alice, "b", root, post.ThreadChange{Pinned: valuePtr(false)}))
	if th.Pinned || !th.Locked || th.State != post.StateOpen {
		t.Errorf("unpinned: %+v", th)
	}

	// Changing a thread isn't an edit.
	if th.LastActivity != before.LastActivity {
		t.Errorf("last activity %s, was %s", th.LastActivity, before.LastActivity)
	}
	for _, p := range must[post.Show](t)(s.Show(ctx, "b", root)).Posts {
		if p.Edited != "" {
			t.Errorf("post %d edited at %s", p.ID, p.Edited)
		}
	}

	if _, err := s.ChangeThread(ctx, alice, "b", root, post.ThreadChange{}); post.KindOf(err) != post.Invalid {
		t.Errorf("an empty change: %v", err)
	}
	if _, err := s.ChangeThread(ctx, alice, "b", root, post.ThreadChange{State: valuePtr("closed")}); post.KindOf(err) != post.Invalid {
		t.Errorf("an invalid state: %v", err)
	}
	if _, err := s.ChangeThread(ctx, alice, "b", 99, resolve); post.KindOf(err) != post.NotFound {
		t.Errorf("a missing post: %v", err)
	}
	if _, err := s.ChangeThread(ctx, alice, "nope", root, resolve); post.KindOf(err) != post.NotFound {
		t.Errorf("a missing board: %v", err)
	}
}

func TestLockedThread(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	root := newThread(t, s, codex, "Locked", "body")
	rep := reply(t, s, claude, root, "a reply")
	if _, _, err := s.Attach(ctx, claude, "b", rep, "log.txt", "text/plain", []byte("x")); err != nil {
		t.Fatal(err)
	}
	must[post.Thread](t)(s.ChangeThread(ctx, alice, "b", root, post.ThreadChange{Locked: valuePtr(true)}))

	body := "changed"
	refused := map[string]error{}
	_, _, refused["reply"] = s.Reply(ctx, claude, "b", rep, post.NewReply{Body: "more"})
	_, _, refused["the root's author replies"] = s.Reply(ctx, codex, "b", root, post.NewReply{Body: "more"})
	_, refused["edit"] = s.Edit(ctx, claude, "b", rep, post.Edit{Body: &body})
	_, refused["edit the root"] = s.Edit(ctx, codex, "b", root, post.Edit{Body: &body})
	_, _, refused["attach"] = s.Attach(ctx, codex, "b", root, "a.txt", "", []byte("x"))
	_, refused["detach"] = s.Detach(ctx, claude, "b", rep, "log.txt")
	_, refused["delete"] = s.Delete(ctx, claude, "b", rep)
	_, refused["delete the root"] = s.Delete(ctx, codex, "b", root)
	for what, err := range refused {
		if post.KindOf(err) != post.Forbidden {
			t.Errorf("%s in a locked thread: %v", what, err)
		}
	}
	show := must[post.Show](t)(s.Show(ctx, "b", root))
	if len(show.Posts) != 2 || show.Posts[1].Body != "a reply\n" || len(show.Posts[1].Attachments) != 1 || len(show.Posts[0].Attachments) != 0 {
		t.Fatalf("refused writes changed the thread: %+v", show.Posts)
	}

	// People still write, and so does everyone once it is unlocked.
	reply(t, s, alice, rep, "a person's reply")
	must[post.Post](t)(s.Edit(ctx, alice, "b", rep, post.Edit{Body: &body}))
	must[post.Thread](t)(s.ChangeThread(ctx, alice, "b", root, post.ThreadChange{Locked: valuePtr(false)}))
	reply(t, s, claude, rep, "unlocked")
	must[post.Removed](t)(s.Delete(ctx, claude, "b", rep))
}

func TestListPinsStatesAndPages(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	var ids []int64
	for _, title := range []string{"one", "two", "three", "four"} {
		ids = append(ids, newThread(t, s, codex, title, "a shared word"))
	}
	must[post.Thread](t)(s.ChangeThread(ctx, alice, "b", ids[1], post.ThreadChange{Pinned: valuePtr(true)}))
	must[post.Thread](t)(s.ChangeThread(ctx, codex, "b", ids[2], post.ThreadChange{State: valuePtr(post.StateResolved)}))
	list := func(f post.Filter) []int64 {
		t.Helper()
		return listedIDs(must[[]post.Thread](t)(s.List(ctx, "b", f)))
	}
	search := func(f post.Filter) []int64 {
		t.Helper()
		return foundIDs(must[[]post.Result](t)(s.Search(ctx, "b", post.Query{Terms: []string{"shared"}, Filter: f})))
	}
	cases := []struct {
		name      string
		got, want []int64
	}{
		{"list, pinned first", list(post.Filter{}), []int64{2, 4, 3, 1}},
		{"list open", list(post.Filter{State: post.StateOpen}), []int64{2, 4, 1}},
		{"list resolved", list(post.Filter{State: post.StateResolved}), []int64{3}},
		{"list a page", list(post.Filter{Skip: 1, Limit: 2}), []int64{4, 3}},
		{"list the last page", list(post.Filter{Skip: 3, Limit: 2}), []int64{1}},
		{"list past the end", list(post.Filter{Skip: 10}), []int64{}},
		{"search ignores pins", search(post.Filter{}), []int64{4, 3, 2, 1}},
		{"search resolved", search(post.Filter{State: post.StateResolved}), []int64{3}},
		{"search open, a page", search(post.Filter{State: post.StateOpen, Skip: 1, Limit: 1}), []int64{2}},
		{"search past the end", search(post.Filter{Skip: 4}), []int64{}},
	}
	for _, c := range cases {
		if !slices.Equal(c.got, c.want) {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	// A reply that matches counts for its thread's state.
	reply(t, s, claude, ids[2], "another word")
	if got := foundIDs(must[[]post.Result](t)(s.Search(ctx, "b", post.Query{Terms: []string{"another"}, Filter: post.Filter{State: post.StateResolved}}))); !slices.Equal(got, []int64{3}) {
		t.Errorf("search of a resolved thread's reply = %v", got)
	}
	for _, f := range []post.Filter{{Skip: -1}, {State: "closed"}} {
		if _, err := s.List(ctx, "b", f); post.KindOf(err) != post.Invalid {
			t.Errorf("List(%+v) = %v, want Invalid", f, err)
		}
		if _, err := s.Search(ctx, "b", post.Query{Terms: []string{"x"}, Filter: f}); post.KindOf(err) != post.Invalid {
			t.Errorf("Search(%+v) = %v, want Invalid", f, err)
		}
	}
}
