package store

import (
	"context"
	"database/sql"

	"github.com/zlcwwhq/agora/internal/post"
)

// checkLocked fails, with an error of kind Forbidden, if the thread whose
// root is the post thread of board is locked and the identity isn't a
// person's.
func checkLocked(ctx context.Context, q querier, who post.Identity, board string, thread int64) error {
	if who.IsPerson() {
		return nil
	}
	var locked bool
	if err := q.QueryRowContext(ctx, "SELECT locked FROM posts WHERE board = ? AND n = ?", board, thread).Scan(&locked); err != nil {
		return err
	}
	return who.MayWriteIn(thread, locked)
}

// ChangeThread changes the state, the pin, and the lock of the thread that
// contains id, as c says, and returns the thread. It makes every change or,
// if the identity may not make one of them, none.
func (s *Store) ChangeThread(ctx context.Context, who post.Identity, board string, id int64, c post.ThreadChange) (post.Thread, error) {
	if c.Empty() {
		return post.Thread{}, post.Errorf(post.Invalid, "nothing to change: give a state, a pin, or a lock")
	}
	if c.State != nil {
		if err := post.CheckState(*c.State); err != nil {
			return post.Thread{}, err
		}
	}
	var out post.Thread
	err := s.tx(ctx, func(tx *sql.Tx) error {
		root, err := rootOf(ctx, tx, board, id)
		if err != nil {
			return err
		}
		rs, err := rows(ctx, tx, postSelect+"WHERE p.pk = ?", root)
		if err != nil {
			return err
		}
		a, err := actor(ctx, tx, board, who)
		if err != nil {
			return err
		}
		if err := a.MayAct(); err != nil {
			return err
		}
		if err := a.MayChangeThread(rs[0].Post, c); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE posts SET state = coalesce(?, state), pinned = coalesce(?, pinned), locked = coalesce(?, locked) WHERE pk = ?",
			orNull(c.State), orNull(c.Pinned), orNull(c.Locked), root); err != nil {
			return err
		}
		t, err := thread(ctx, tx, root)
		out = t.Thread
		return err
	})
	return out, err
}

// orNull returns *v, or nil, SQL's NULL, if v is nil.
func orNull[T any](v *T) any {
	if v == nil {
		return nil
	}
	return *v
}
