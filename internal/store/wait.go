package store

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/zlcwwhq/agora/internal/post"
)

// waits are the requests that wait for posts, as api/cli.md's agora wait
// says, and what wakes them.
type waits struct {
	mu sync.Mutex
	// changed is closed, and replaced, by a write that may have made posts
	// unread or edited for some reader.
	changed chan struct{}
	// carriers counts, by board, harness, and session, the waits of the
	// sessions that carry names.
	carriers map[string]int
}

// renewEvery is how often a waiting carrier's name has its last activity
// renewed.
const renewEvery = time.Minute

// changes returns what the next write that may make posts unread closes.
func (w *waits) changes() <-chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.changed == nil {
		w.changed = make(chan struct{})
	}
	return w.changed
}

// notify wakes the requests that wait.
func (w *waits) notify() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.changed != nil {
		close(w.changed)
		w.changed = nil
	}
}

func carrierKey(board, harness, session string) string {
	return board + "\x00" + harness + "\x00" + session
}

// hold counts a wait of a carrier, until the function that it returns.
func (w *waits) hold(key string) func() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.carriers == nil {
		w.carriers = map[string]int{}
	}
	w.carriers[key]++
	return func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		if w.carriers[key]--; w.carriers[key] == 0 {
			delete(w.carriers, key)
		}
	}
}

// waiting reports whether the session waits on board.
func (w *waits) waiting(board, harness, session string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.carriers[carrierKey(board, harness, session)] > 0
}

// wait is Unread with req.Wait: it returns what Unread would, marking
// nothing, once there is something, or nothing once req.Wait seconds are
// over or ctx is done. While it waits for a session that carries a name,
// the name is waiting, and its last activity is renewed.
func (s *Store) wait(ctx context.Context, who post.Identity, board string, req post.UnreadRequest) (post.Unread, error) {
	over := time.NewTimer(time.Duration(req.Wait) * time.Second)
	defer over.Stop()
	renew := time.NewTicker(renewEvery)
	defer renew.Stop()
	var carrier string
	release := func() {}
	defer func() { release() }()
	newReader := false
	for {
		changed := s.changes()
		u, a, err := s.unread(ctx, who, board, req, false)
		if err != nil {
			return u, err
		}
		newReader = newReader || u.NewReader
		u.NewReader = newReader
		if len(u.Posts)+len(u.Edited) > 0 {
			return u, nil
		}
		// The session that waits may have claimed, or lost, a name since.
		if key := carrierOf(board, a); key != carrier {
			release()
			release, carrier = func() {}, key
			if key != "" {
				release = s.hold(key)
			}
		}
		select {
		case <-changed:
		case <-renew.C:
			if err := s.renew(ctx, board, a); err != nil {
				return u, err
			}
		case <-over.C:
			return u, s.renew(ctx, board, a)
		case <-ctx.Done():
			return u, nil
		}
	}
}

// carrierOf returns the key of the actor's carrier, if it carries a name.
func carrierOf(board string, a post.Actor) string {
	if !a.Name || a.Carrier == "" {
		return ""
	}
	return carrierKey(board, a.Harness, a.Carrier)
}

// renew renews the last activity of the name that the actor's carrier
// carries, if it still does.
func (s *Store) renew(ctx context.Context, board string, a post.Actor) error {
	if !a.Name || a.Carrier == "" {
		return nil
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE claims SET last_active = ? WHERE board = ? AND harness = ? AND name = ? AND session = ? AND ended = ''",
			s.stamp(), board, a.Harness, a.Session, a.Carrier)
		return err
	})
}
