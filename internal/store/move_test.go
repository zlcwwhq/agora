package store

import (
	"context"
	"slices"
	"testing"

	"github.com/zlcwwhq/agora/internal/post"
)

// threadPosts returns the ids of the posts of the thread that contains id,
// in display order, and the post that each replies to, 0 for none.
func threadPosts(t *testing.T, s *Store, id int64) (ids, replyTo []int64) {
	t.Helper()
	for _, p := range must[post.Show](t)(s.Show(context.Background(), "b", id)).Posts {
		ids = append(ids, p.ID)
		to := int64(0)
		if p.ReplyTo != nil {
			to = *p.ReplyTo
		}
		replyTo = append(replyTo, to)
	}
	return ids, replyTo
}

func TestMoveReplies(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	a := newThread(t, s, codex, "Alpha", "first")    // 1
	r2 := reply(t, s, claude, a, "two")              // 2
	r3 := reply(t, s, codex, r2, "three")            // 3
	r4 := reply(t, s, claude, a, "four")             // 4
	reply(t, s, codex, r3, "five")                   // 5
	b := newThread(t, s, claude, "Beta", "second")   // 6
	r7 := reply(t, s, codex, b, "seven", "tag-of-7") // 7

	if _, err := s.Move(ctx, codex, "b", r2, post.Move{To: r7}); post.KindOf(err) != post.Forbidden {
		t.Errorf("an agent moves: %v", err)
	}
	// A reply moves with the replies to it, directly or not.
	moved := must[post.Moved](t)(s.Move(ctx, alice, "b", r2, post.Move{To: r7}))
	if moved.Thread != b || !slices.Equal(moved.Moved, []int64{2, 3, 5}) {
		t.Fatalf("Move = %+v", moved)
	}
	ids, to := threadPosts(t, s, b)
	if !slices.Equal(ids, []int64{2, 3, 5, 6, 7}) || !slices.Equal(to, []int64{7, 2, 3, 0, 6}) {
		t.Errorf("thread %d = %v replying to %v", b, ids, to)
	}
	ids, to = threadPosts(t, s, a)
	if !slices.Equal(ids, []int64{1, 4}) || !slices.Equal(to, []int64{0, 1}) {
		t.Errorf("thread %d = %v replying to %v", a, ids, to)
	}
	for _, p := range must[post.Show](t)(s.Show(ctx, "b", b)).Posts {
		if p.Edited != "" || p.Thread != b {
			t.Errorf("moved post %+v", p)
		}
	}

	// A reply becomes a new thread's root, open, followed by its replies.
	moved = must[post.Moved](t)(s.Move(ctx, alice, "b", r3, post.Move{Title: valuePtr("  Gamma  ")}))
	if moved.Thread != r3 || !slices.Equal(moved.Moved, []int64{3, 5}) {
		t.Fatalf("Move to a new thread = %+v", moved)
	}
	th := must[post.Show](t)(s.Show(ctx, "b", 5)).Thread
	if th.ID != r3 || th.Title != "Gamma" || th.Replies != 1 || th.State != post.StateOpen || th.Author != "codex/019a2b3c@gpu" {
		t.Errorf("new thread = %+v", th)
	}
	if ids, to := threadPosts(t, s, r3); !slices.Equal(ids, []int64{3, 5}) || !slices.Equal(to, []int64{0, 3}) {
		t.Errorf("thread %d = %v replying to %v", r3, ids, to)
	}
	if rs := must[[]post.Result](t)(s.Search(ctx, "b", post.Query{Terms: []string{"gamma"}, Field: post.FieldTitle})); len(rs) != 1 || rs[0].ID != r3 {
		t.Errorf("search of the new title = %+v", rs)
	}

	refusals := []struct {
		what string
		id   int64
		m    post.Move
		want post.Kind
	}{
		{"to itself", r4, post.Move{To: r4}, post.Invalid},
		{"to a post that moves with it", b, post.Move{To: r7}, post.Invalid},
		{"a root to a new thread", a, post.Move{Title: valuePtr("New")}, post.Invalid},
		{"to a new thread without a title", r4, post.Move{Title: valuePtr(" ")}, post.Invalid},
		{"with to and title", r4, post.Move{To: b, Title: valuePtr("New")}, post.Invalid},
		{"with neither", r4, post.Move{}, post.Invalid},
		{"a missing post", 99, post.Move{To: b}, post.NotFound},
		{"to a missing post", r4, post.Move{To: 99}, post.NotFound},
	}
	for _, r := range refusals {
		if _, err := s.Move(ctx, alice, "b", r.id, r.m); post.KindOf(err) != r.want {
			t.Errorf("moving %s: %v, want kind %d", r.what, err, r.want)
		}
	}
	if _, err := s.Move(ctx, alice, "nope", r4, post.Move{To: b}); post.KindOf(err) != post.NotFound {
		t.Errorf("moving on a missing board: %v", err)
	}
}

func TestMoveRoot(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	a := newThread(t, s, codex, "Alpha", "first", "alpha-tag") // 1
	r2 := reply(t, s, claude, a, "two")                        // 2
	r3 := reply(t, s, codex, r2, "three")                      // 3
	b := newThread(t, s, claude, "Beta", "second")             // 4
	must[post.Thread](t)(s.ChangeThread(ctx, alice, "b", a, post.ThreadChange{State: valuePtr(post.StateResolved), Pinned: valuePtr(true), Locked: valuePtr(true)}))
	// A reply whose post was deleted replies to nothing, and still moves
	// with its thread.
	must[post.Removed](t)(s.Delete(ctx, alice, "b", r2))

	moved := must[post.Moved](t)(s.Move(ctx, alice, "b", a, post.Move{To: b}))
	if moved.Thread != b || !slices.Equal(moved.Moved, []int64{a, r3}) {
		t.Fatalf("Move of a root = %+v", moved)
	}
	ts := must[[]post.Thread](t)(s.List(ctx, "b", post.Filter{}))
	if len(ts) != 1 || ts[0].ID != b || ts[0].Replies != 2 || !slices.Equal(ts[0].Tags, []string{"alpha-tag"}) ||
		ts[0].State != post.StateOpen || ts[0].Pinned || ts[0].Locked {
		t.Fatalf("threads = %+v", ts)
	}
	if ts[0].LastActivity != must[post.Post](t)(s.Get(ctx, "b", b)).Created {
		t.Errorf("last activity %s isn't that of the newest post, %d", ts[0].LastActivity, b)
	}
	p := must[post.Post](t)(s.Get(ctx, "b", a))
	if p.Title != "" || p.Thread != b || p.ReplyTo == nil || *p.ReplyTo != b || p.Edited != "" {
		t.Errorf("the old root = %+v", p)
	}
	if n := count(t, s, "SELECT count(*) FROM posts WHERE pk = (SELECT pk FROM posts WHERE n = ?) AND state = 'open' AND pinned = 0 AND locked = 0 AND title_fold = ''", a); n != 1 {
		t.Error("the old root kept its thread's title, state, pin, or lock")
	}
	if rs := must[[]post.Result](t)(s.Search(ctx, "b", post.Query{Terms: []string{"alpha"}, Field: post.FieldTitle})); len(rs) != 0 {
		t.Errorf("search finds the old title: %+v", rs)
	}

	// It can start a thread again, without what its thread had.
	moved = must[post.Moved](t)(s.Move(ctx, alice, "b", a, post.Move{Title: valuePtr("Alpha again")}))
	if moved.Thread != a || !slices.Equal(moved.Moved, []int64{a}) {
		t.Fatalf("Move back = %+v", moved)
	}
	th := must[post.Show](t)(s.Show(ctx, "b", a)).Thread
	if th.Title != "Alpha again" || th.State != post.StateOpen || th.Pinned || th.Locked || th.Replies != 0 {
		t.Errorf("thread again = %+v", th)
	}
}
