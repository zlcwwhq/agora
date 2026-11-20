package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/zlcwwhq/agora/internal/post"
)

// What a reader has seen of a board: the posts whose ids are at most its
// cursor, readers.seen, its own posts, and the posts that it has a seen mark
// for. It has seen a post's latest edit if the edit's number, posts.edit_n,
// is at most readers.edits_seen or the edit_n of the post's seen mark, or if
// it made the edit itself.

// seenSQL is the condition that the reader has seen the post p, with the
// arguments cursor, harness, session, and reader.
const seenSQL = `(p.n <= ? OR (p.harness = ? AND p.session = ?)
	OR EXISTS (SELECT 1 FROM seen s WHERE s.post = p.pk AND s.reader = ?))`

// editUnseenSQL is the condition that another identity than the reader made
// p's latest edit, which the reader hasn't seen, with the arguments
// edits_seen, reader, harness, and session. The latest edit's identity is
// the editor's, or the author's for a post without one.
const editUnseenSQL = `p.edit_n > max(?, coalesce((SELECT s.edit_n FROM seen s WHERE s.post = p.pk AND s.reader = ?), 0))
	AND NOT (CASE WHEN p.editor_harness <> '' THEN p.editor_harness ELSE p.harness END = ?
		AND CASE WHEN p.editor_harness <> '' THEN p.editor_session ELSE p.session END = ?)`

// forMeSQL is the condition that p is for the reader: in a thread that holds
// a post of the reader's, or mentioning it, with the arguments harness,
// session, harness, and session.
const forMeSQL = `(EXISTS (SELECT 1 FROM posts o WHERE (o.pk = coalesce(p.thread, p.pk) OR o.thread = coalesce(p.thread, p.pk))
		AND o.harness = ? AND o.session = ?)
	OR EXISTS (SELECT 1 FROM mentions m WHERE m.post = p.pk AND m.harness = ? AND substr(?, 1, length(m.session)) = m.session))`

// cursor is what a reader has seen of a board, but for its seen marks.
type cursor struct {
	seen, editsSeen int64
}

// Unread returns the posts of a board that the identity's reader hasn't
// seen, and those that it has seen that another identity edited since, of
// the threads and posts that req keeps. Unless req.Peek, it marks them seen:
// every post and edit of the board without req.ToMe and req.Thread, else
// only the posts that it returns, each with its latest edit.
func (s *Store) Unread(ctx context.Context, who post.Identity, board string, req post.UnreadRequest) (post.Unread, error) {
	if req.Since < 0 {
		return post.Unread{}, post.Errorf(post.Invalid, "since must be at least 0")
	}
	if req.Thread < 0 {
		return post.Unread{}, post.Errorf(post.Invalid, "invalid thread %d: want a post's id, or 0 for every thread", req.Thread)
	}
	var out post.Unread
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkBoard(ctx, tx, board); err != nil {
			return err
		}
		var keep string
		var keepArgs []any
		if req.ToMe {
			keep += " AND " + forMeSQL
			keepArgs = append(keepArgs, who.Harness, who.Session, who.Harness, who.Session)
		}
		if req.Thread != 0 {
			root, err := rootOf(ctx, tx, board, req.Thread)
			if err != nil {
				return err
			}
			keep += " AND coalesce(p.thread, p.pk) = ?"
			keepArgs = append(keepArgs, root)
		}
		c, isNew, err := s.startReading(ctx, tx, who, board, req.Since)
		if err != nil {
			return err
		}
		out.NewReader = isNew
		unread, err := rows(ctx, tx, postSelect+"WHERE p.board = ? AND NOT "+seenSQL+keep+" ORDER BY p.n",
			append([]any{board, c.seen, who.Harness, who.Session, who.Reader()}, keepArgs...)...)
		if err != nil {
			return err
		}
		edited, err := rows(ctx, tx, postSelect+"WHERE p.board = ? AND "+seenSQL+" AND "+editUnseenSQL+keep+" ORDER BY p.n",
			append([]any{board, c.seen, who.Harness, who.Session, who.Reader(), c.editsSeen, who.Reader(), who.Harness, who.Session}, keepArgs...)...)
		if err != nil {
			return err
		}
		out.Posts, out.Edited = posts(unread), posts(edited)
		switch {
		case req.Peek:
			return nil
		case !req.ToMe && req.Thread == 0:
			return seeAll(ctx, tx, who.Reader(), board)
		}
		return see(ctx, tx, who.Reader(), append(unread, edited...))
	})
	return out, err
}

// startReading returns the reader's cursor on a board, starting it if the
// reader is new: as if it had seen every post created more than since
// seconds ago, or every post if since is 0, and every edit until now.
func (s *Store) startReading(ctx context.Context, q querier, who post.Identity, board string, since int64) (cursor, bool, error) {
	var c cursor
	err := q.QueryRowContext(ctx, "SELECT seen, edits_seen FROM readers WHERE board = ? AND reader = ?", board, who.Reader()).
		Scan(&c.seen, &c.editsSeen)
	if !errors.Is(err, sql.ErrNoRows) {
		return c, false, err
	}
	if err := q.QueryRowContext(ctx, "SELECT last_n, last_edit FROM boards WHERE id = ?", board).Scan(&c.seen, &c.editsSeen); err != nil {
		return c, true, err
	}
	if since > 0 {
		cutoff := s.now().Add(-time.Duration(since) * time.Second).Unix()
		if err := q.QueryRowContext(ctx, `SELECT coalesce(max(n), 0) FROM posts
			WHERE board = ? AND CAST(strftime('%s', created) AS INTEGER) < ?`, board, cutoff).Scan(&c.seen); err != nil {
			return c, true, err
		}
	}
	_, err = q.ExecContext(ctx, "INSERT INTO readers (board, reader, seen, edits_seen) VALUES (?, ?, ?, ?)", board, who.Reader(), c.seen, c.editsSeen)
	return c, true, err
}

// seeAll marks every post and edit of a board seen by reader: its cursor
// moves to the board's newest post, and its seen marks there are no longer
// needed.
func seeAll(ctx context.Context, q querier, reader, board string) error {
	if _, err := q.ExecContext(ctx, `UPDATE readers SET seen = max(seen, (SELECT last_n FROM boards WHERE id = ?)),
		edits_seen = max(edits_seen, (SELECT last_edit FROM boards WHERE id = ?)) WHERE board = ? AND reader = ?`, board, board, board, reader); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, "DELETE FROM seen WHERE reader = ? AND post IN (SELECT pk FROM posts WHERE board = ?)", reader, board)
	return err
}

// see marks posts seen by reader, each with its latest edit.
func see(ctx context.Context, q querier, reader string, rs []row) error {
	for _, r := range rs {
		if _, err := q.ExecContext(ctx, `INSERT INTO seen (post, reader, edit_n) SELECT pk, ?, edit_n FROM posts WHERE pk = ?
			ON CONFLICT (post, reader) DO UPDATE SET edit_n = excluded.edit_n`, reader, r.pk); err != nil {
			return err
		}
	}
	return nil
}
