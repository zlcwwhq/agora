package store

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/zlcwwhq/agora/internal/post"
)

// waitFor calls Unread with req, which waits, in a goroutine, and returns
// what it returns once it does.
func waitFor(s *Store, ctx context.Context, who post.Identity, req post.UnreadRequest) <-chan post.Unread {
	out := make(chan post.Unread, 1)
	go func() {
		u, err := s.Unread(ctx, who, "b", req)
		if err != nil {
			u.Posts = []post.Post{{Body: "error: " + err.Error()}}
		}
		out <- u
	}()
	return out
}

// untilWaiting returns once the name that a session carries waits.
func untilWaiting(t *testing.T, s *Store, name string) {
	t.Helper()
	for range 500 {
		ns := must[[]post.Name](t)(s.Names(context.Background(), "b"))
		for _, n := range ns {
			if n.Name == name && n.Waiting {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s never waited", name)
}

func postIDs(ps []post.Post) []int64 {
	var out []int64
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return out
}

func TestWaitReturnsWhenPostsArrive(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	claimAs(t, s, ctx, sessA, "e2e", false)
	got := waitFor(s, ctx, sessA, post.UnreadRequest{ToMe: true, Wait: 30})
	untilWaiting(t, s, "claude-code/e2e")
	newThread(t, s, alice, "Noise", "not for the name")
	select {
	case u := <-got:
		t.Fatalf("wait returned for a post that isn't for it: %+v", u)
	case <-time.After(100 * time.Millisecond):
	}
	job := newThread(t, s, alice, "Job", "@claude-code/e2e run the suite")
	select {
	case u := <-got:
		if !slices.Equal(postIDs(u.Posts), []int64{job}) || len(u.Edited) != 0 {
			t.Errorf("wait = %+v", u)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("wait didn't return for a post for it")
	}
	// It marked nothing, and the name no longer waits.
	if got := unreadIDs(t, s, ctx, sessA, post.UnreadRequest{ToMe: true, Peek: true}); !slices.Equal(got, []int64{job}) {
		t.Errorf("unread after wait = %v", got)
	}
	if ns := must[[]post.Name](t)(s.Names(ctx, "b")); ns[0].Waiting {
		t.Errorf("the name still waits: %+v", ns[0])
	}
	// With something to show, it returns at once.
	u, err := s.Unread(ctx, sessA, "b", post.UnreadRequest{ToMe: true, Wait: 30})
	if err != nil || !slices.Equal(postIDs(u.Posts), []int64{job}) {
		t.Errorf("wait with a post to show = %+v, %v", u, err)
	}
}

func TestWaitReturnsForEdits(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	root := newThread(t, s, codex, "Plan", "x")
	unreadIDs(t, s, ctx, claude, post.UnreadRequest{})
	got := waitFor(s, ctx, claude, post.UnreadRequest{Wait: 30})
	time.Sleep(50 * time.Millisecond)
	body := "x, edited"
	must[post.Post](t)(s.Edit(ctx, codex, "b", root, post.Edit{Body: &body}))
	select {
	case u := <-got:
		if len(u.Posts) != 0 || !slices.Equal(postIDs(u.Edited), []int64{root}) {
			t.Errorf("wait = %+v", u)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("wait didn't return for an edit")
	}
}

func TestWaitTimesOut(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	n := claimAs(t, s, ctx, sessA, "e2e", false)
	start := time.Now()
	u, err := s.Unread(ctx, sessA, "b", post.UnreadRequest{Wait: 1})
	if err != nil || len(u.Posts) != 0 || len(u.Edited) != 0 || u.Posts == nil || u.Edited == nil {
		t.Errorf("wait = %+v, %v", u, err)
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Errorf("wait returned after %v", elapsed)
	}
	// Waiting renewed the name's last activity.
	if ns := must[[]post.Name](t)(s.Names(ctx, "b")); ns[0].LastActive <= n.LastActive || ns[0].Waiting {
		t.Errorf("the name after waiting = %+v, before %+v", ns[0], n)
	}
	// A new reader that waits has its cursor started, without since.
	newThread(t, s, alice, "Old", "before the reader")
	u, err = s.Unread(ctx, claude, "b", post.UnreadRequest{Wait: 1, Since: 0})
	if err != nil || !u.NewReader || len(u.Posts) != 0 {
		t.Errorf("a new reader's wait = %+v, %v", u, err)
	}
}

func TestWaitReturnsWhenDone(t *testing.T) {
	s, _ := open(t)
	ctx, cancel := context.WithCancel(context.Background())
	got := waitFor(s, ctx, claude, post.UnreadRequest{Wait: 600})
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case u := <-got:
		if len(u.Posts) != 0 {
			t.Errorf("wait = %+v", u)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait didn't return once its context was done")
	}
}

func TestWaitRefusals(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	for _, wait := range []int64{-1, 601} {
		if _, err := s.Unread(ctx, claude, "b", post.UnreadRequest{Wait: wait}); post.KindOf(err) != post.Invalid {
			t.Errorf("wait %d = %v", wait, err)
		}
	}
	claimAs(t, s, ctx, sessA, "e2e", false)
	// As a name that another session carries, a wait that would mark
	// fails at once; one that peeks waits.
	start := time.Now()
	if _, err := s.Unread(via(sessB.Session, ""), e2e, "b", post.UnreadRequest{Wait: 30}); post.KindOf(err) != post.Forbidden || time.Since(start) > 5*time.Second {
		t.Errorf("wait as another's name = %v", err)
	}
	if _, err := s.Unread(via(sessB.Session, ""), e2e, "b", post.UnreadRequest{Wait: 1, Peek: true}); err != nil {
		t.Errorf("wait --peek as another's name = %v", err)
	}
	if _, err := s.Unread(ctx, claude, "nope", post.UnreadRequest{Wait: 30}); post.KindOf(err) != post.NotFound {
		t.Errorf("wait on a missing board = %v", err)
	}
}
