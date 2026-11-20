package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"

	"github.com/zlcwwhq/agora/internal/post"
)

// subtree returns the keys of the posts that move with the post r: a root's
// whole thread, or a reply and the posts of its thread that reply to it,
// directly or through other replies.
func subtree(ctx context.Context, q querier, r row) ([]int64, error) {
	if r.IsRoot() {
		return column[int64](ctx, q, "SELECT pk FROM posts WHERE pk = ? OR thread = ?", r.pk, r.pk)
	}
	return column[int64](ctx, q, `WITH RECURSIVE sub (pk) AS (
			SELECT ?
			UNION SELECT p.pk FROM posts p JOIN sub ON p.reply_to = sub.pk
			WHERE p.thread = (SELECT thread FROM posts WHERE pk = ?)
		) SELECT pk FROM sub`, r.pk, r.pk)
}

// Move moves post id, with the posts of its thread that reply to it,
// directly or through other replies, as agora move does: into the thread of
// m.To, where id then replies to m.To, or into a new thread titled m.Title,
// whose root id becomes. Only people may. Moving isn't an edit.
func (s *Store) Move(ctx context.Context, who post.Identity, board string, id int64, m post.Move) (post.Moved, error) {
	var out post.Moved
	if err := m.Check(); err != nil {
		return out, err
	}
	var title string
	if m.Title != nil {
		var err error
		if title, err = post.Title(*m.Title); err != nil {
			return out, err
		}
	}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		r, err := get(ctx, tx, board, id)
		if err != nil {
			return err
		}
		if err := who.MayMove(); err != nil {
			return err
		}
		moving, err := subtree(ctx, tx, r)
		if err != nil {
			return err
		}
		// The thread that the posts move to, and the post that id then
		// replies to.
		var thread, replyTo any
		if m.To != 0 {
			var to, root int64
			err := tx.QueryRowContext(ctx, `SELECT p.pk, coalesce(p.thread, p.pk), coalesce(r.n, p.n)
				FROM posts p LEFT JOIN posts r ON r.pk = p.thread WHERE p.board = ? AND p.n = ?`, board, m.To).Scan(&to, &root, &out.Thread)
			if errors.Is(err, sql.ErrNoRows) {
				return post.NoPost(board, m.To)
			}
			if err != nil {
				return err
			}
			if slices.Contains(moving, to) {
				return post.Errorf(post.Invalid, "post #%d moves with #%d: move it to a post that doesn't", m.To, id)
			}
			thread, replyTo = root, to
		} else {
			if r.IsRoot() {
				return post.Errorf(post.Invalid, "post #%d is already a thread's first post: a new thread's title is for moving a reply", id)
			}
			thread, out.Thread = r.pk, id
		}
		for _, pk := range moving {
			if pk == r.pk {
				continue
			}
			if _, err := tx.ExecContext(ctx, "UPDATE posts SET thread = ? WHERE pk = ?", thread, pk); err != nil {
				return err
			}
		}
		// id keeps no title unless it is the new thread's root, and no
		// state, pin, or lock of the thread that it may have started.
		if m.To == 0 {
			thread = nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE posts SET thread = ?, reply_to = ?, title = ?, title_fold = ?,
			state = 'open', pinned = 0, locked = 0 WHERE pk = ?`, thread, replyTo, title, post.Fold(title), r.pk); err != nil {
			return err
		}
		out.Moved = make([]int64, 0, len(moving))
		for _, pk := range moving {
			var n int64
			if err := tx.QueryRowContext(ctx, "SELECT n FROM posts WHERE pk = ?", pk).Scan(&n); err != nil {
				return err
			}
			out.Moved = append(out.Moved, n)
		}
		slices.Sort(out.Moved)
		return nil
	})
	return out, err
}
