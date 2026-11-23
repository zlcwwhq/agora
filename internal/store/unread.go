package store

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/zlcwwhq/agora/internal/post"
)

// What a reader has seen of a board: the posts whose ids are at most its
// cursor, readers.seen, its own posts, a name's including those of the
// sessions that it adopted, and the posts that it has a seen mark for. It
// has seen a post's latest edit if the edit's number, posts.edit_n, is at
// most readers.edits_seen or the edit_n of the post's seen mark, or if it
// made the edit itself.

// inList returns n placeholders for SQL's IN.
func inList(n int) string { return "(" + strings.Repeat("?, ", n-1) + "?)" }

// ownSQL is the condition that the post aliased as alias is the actor's.
func ownSQL(a post.Actor, alias string) (string, []any) {
	sessions := a.Sessions()
	args := []any{a.Harness}
	for _, session := range sessions {
		args = append(args, session)
	}
	return "(" + alias + ".harness = ? AND " + alias + ".session IN " + inList(len(sessions)) + ")", args
}

// seenSQL is the condition that the actor, whose cursor is c, has seen the
// post p.
func seenSQL(a post.Actor, c cursor) (string, []any) {
	own, args := ownSQL(a, "p")
	return "(p.n <= ? OR " + own + " OR EXISTS (SELECT 1 FROM seen s WHERE s.post = p.pk AND s.reader = ?))",
		append(append([]any{c.seen}, args...), a.Reader())
}

// editUnseenSQL is the condition that another identity than the actor made
// p's latest edit, which the actor hasn't seen. The latest edit's identity is
// the editor's, or the author's for a post without one.
func editUnseenSQL(a post.Actor, c cursor) (string, []any) {
	sessions := a.Sessions()
	args := []any{c.editsSeen, a.Reader(), a.Harness}
	for _, session := range sessions {
		args = append(args, session)
	}
	return `p.edit_n > max(?, coalesce((SELECT s.edit_n FROM seen s WHERE s.post = p.pk AND s.reader = ?), 0))
		AND NOT (CASE WHEN p.editor_harness <> '' THEN p.editor_harness ELSE p.harness END = ?
			AND CASE WHEN p.editor_harness <> '' THEN p.editor_session ELSE p.session END IN ` + inList(len(sessions)) + ")", args
}

// notNameSQL is the condition that the mention m doesn't name a name.
const notNameSQL = "NOT EXISTS (SELECT 1 FROM names n WHERE n.board = p.board AND n.harness = m.harness AND n.name = m.session)"

// forMeSQL is the condition that p is for the actor: in a thread that holds
// a post of its, or mentioning it, as api/cli.md's Mentions says. A session
// that a name adopted keeps only the mentions of its whole harness.
func forMeSQL(a post.Actor) (string, []any) {
	var args []any
	var or []string
	if a.AdoptedBy == "" {
		own, ownArgs := ownSQL(a, "o")
		or = append(or, "EXISTS (SELECT 1 FROM posts o WHERE (o.pk = coalesce(p.thread, p.pk) OR o.thread = coalesce(p.thread, p.pk)) AND "+own+")")
		args = append(args, ownArgs...)
	}
	match := []string{"m.session = ''"}
	var matchArgs []any
	prefixes := []string{a.Session}
	if a.Name {
		match = append(match, "m.session = ?")
		matchArgs = append(matchArgs, a.Session)
		prefixes = a.Adopted
	} else if a.AdoptedBy != "" {
		prefixes = nil
	}
	if len(prefixes) > 0 {
		var alts []string
		for _, session := range prefixes {
			alts = append(alts, "substr(?, 1, length(m.session)) = m.session")
			matchArgs = append(matchArgs, session)
		}
		match = append(match, "(("+strings.Join(alts, " OR ")+") AND "+notNameSQL+")")
	}
	or = append(or, "EXISTS (SELECT 1 FROM mentions m WHERE m.post = p.pk AND m.harness = ? AND ("+strings.Join(match, " OR ")+"))")
	args = append(append(args, a.Harness), matchArgs...)
	return "(" + strings.Join(or, " OR ") + ")", args
}

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
	if req.Wait < 0 || req.Wait > int64(post.MaxWait/time.Second) {
		return post.Unread{}, post.Errorf(post.Invalid, "invalid wait %d: want 0 to %d seconds", req.Wait, int64(post.MaxWait/time.Second))
	}
	if req.Wait > 0 {
		return s.wait(ctx, who, board, req)
	}
	out, _, err := s.unread(ctx, who, board, req, !req.Peek)
	return out, err
}

// unread is Unread, marking what it returns seen only if mark is true, and
// returns the actor that who acts as.
func (s *Store) unread(ctx context.Context, who post.Identity, board string, req post.UnreadRequest, mark bool) (post.Unread, post.Actor, error) {
	var out post.Unread
	var a post.Actor
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkBoard(ctx, tx, board); err != nil {
			return err
		}
		var err error
		if a, err = actor(ctx, tx, board, who); err != nil {
			return err
		}
		if !req.Peek {
			if err := a.MayAct(); err != nil {
				return err
			}
		}
		var keep string
		var keepArgs []any
		if req.ToMe {
			cond, args := forMeSQL(a)
			keep += " AND " + cond
			keepArgs = append(keepArgs, args...)
		}
		if req.Thread != 0 {
			root, err := rootOf(ctx, tx, board, req.Thread)
			if err != nil {
				return err
			}
			keep += " AND coalesce(p.thread, p.pk) = ?"
			keepArgs = append(keepArgs, root)
		}
		c, isNew, err := s.startReading(ctx, tx, a.Identity, board, req.Since)
		if err != nil {
			return err
		}
		out.NewReader = isNew
		unread, edited, err := unreadRows(ctx, tx, board, a, c, keep, keepArgs)
		if err != nil {
			return err
		}
		out.Posts, out.Edited = posts(unread), posts(edited)
		switch {
		case !mark:
			return nil
		case !req.ToMe && req.Thread == 0:
			return seeAll(ctx, tx, a.Reader(), board)
		}
		return see(ctx, tx, a.Reader(), append(unread, edited...))
	})
	return out, a, err
}

// unreadRows returns the posts of a board, of those that keep keeps, that
// the actor, whose cursor is c, hasn't seen, and those that it has seen that
// another identity edited since.
func unreadRows(ctx context.Context, q querier, board string, a post.Actor, c cursor, keep string, keepArgs []any) (unread, edited []row, err error) {
	seen, seenArgs := seenSQL(a, c)
	editUnseen, editArgs := editUnseenSQL(a, c)
	unread, err = rows(ctx, q, postSelect+"WHERE p.board = ? AND NOT "+seen+keep+" ORDER BY p.n",
		append(append([]any{board}, seenArgs...), keepArgs...)...)
	if err != nil {
		return nil, nil, err
	}
	edited, err = rows(ctx, q, postSelect+"WHERE p.board = ? AND "+seen+" AND "+editUnseen+keep+" ORDER BY p.n",
		append(append(append([]any{board}, seenArgs...), editArgs...), keepArgs...)...)
	return unread, edited, err
}

// pending returns the ids of the posts that unread --to-me would show the
// actor, a name, unread or edited, in order, marking nothing.
func pending(ctx context.Context, q querier, board string, a post.Actor) ([]int64, error) {
	var c cursor
	err := q.QueryRowContext(ctx, "SELECT seen, edits_seen FROM readers WHERE board = ? AND reader = ?", board, a.Reader()).Scan(&c.seen, &c.editsSeen)
	if errors.Is(err, sql.ErrNoRows) {
		return []int64{}, nil
	}
	if err != nil {
		return nil, err
	}
	forMe, forMeArgs := forMeSQL(a)
	unread, edited, err := unreadRows(ctx, q, board, a, c, " AND "+forMe, forMeArgs)
	if err != nil {
		return nil, err
	}
	out := []int64{}
	for _, r := range append(unread, edited...) {
		out = append(out, r.ID)
	}
	slices.Sort(out)
	return out, nil
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
