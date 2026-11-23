package server

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/zlcwwhq/agora/internal/client"
	"github.com/zlcwwhq/agora/internal/post"
	"github.com/zlcwwhq/agora/internal/store"
	"github.com/zlcwwhq/agora/internal/testdir"
)

func TestAPIWait(t *testing.T) {
	base, _ := start(t, post.Identity{})
	ctx := context.Background()
	cx, cl := client.New(base, "b", codex), client.New(base, "b", sessA)
	if _, err := cl.Unread(ctx, post.UnreadRequest{}); err != nil {
		t.Fatal(err)
	}
	type result struct {
		u   post.Unread
		err error
	}
	got := make(chan result, 1)
	go func() {
		u, err := cl.Unread(ctx, post.UnreadRequest{Wait: 30})
		got <- result{u, err}
	}()
	time.Sleep(100 * time.Millisecond)
	pub, err := cx.Post(ctx, post.NewThread{Title: "T", Body: "b"})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-got:
		if r.err != nil || len(r.u.Posts) != 1 || r.u.Posts[0].ID != pub.ID {
			t.Errorf("wait = %+v, %v", r.u, r.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the request that waits didn't answer")
	}
	// It marked nothing.
	if u, err := cl.Unread(ctx, post.UnreadRequest{Peek: true}); err != nil || len(u.Posts) != 1 {
		t.Errorf("unread after wait = %+v, %v", u, err)
	}
	for _, body := range []string{`{"wait": 601}`, `{"wait": -1}`} {
		if status, _, data := nameAPI(t, "POST", base+"/api/boards/b/unread", body, map[string]string{"Agora-Author": sessA.Header()}); status != 400 {
			t.Errorf("%s: status %d: %s", body, status, data)
		}
	}
}

func TestShutdownAnswersWaits(t *testing.T) {
	st, err := store.Open(filepath.Join(testdir.New(t), "board.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, _, err := st.CreateBoard(context.Background(), alice, post.NewBoard{ID: "b"}); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, ln, Options{Store: st, Version: "test", Logf: t.Logf}) }()
	cl := client.New("http://"+ln.Addr().String(), "b", codex)
	got := make(chan error, 1)
	go func() {
		_, err := cl.Unread(context.Background(), post.UnreadRequest{Wait: 600})
		got <- err
	}()
	time.Sleep(200 * time.Millisecond)
	stopped := time.Now()
	cancel()
	select {
	case err := <-done:
		if err != nil || time.Since(stopped) > 3*time.Second {
			t.Errorf("Run = %v after %v", err, time.Since(stopped))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the server kept running for a request that waits")
	}
	if err := <-got; err != nil {
		t.Errorf("the request that waited = %v", err)
	}
}
