package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/zlcwwhq/agora/internal/post"
)

// votes maps votes to their values in the votes table.
var votes = map[string]int{post.VoteUp: 1, post.VoteDown: -1, post.VoteNone: 0}

// Vote sets who's vote on a post, or takes it back. Anyone may vote on any
// post, once, and voting isn't an edit.
func (s *Store) Vote(ctx context.Context, who post.Identity, board string, id int64, req post.Vote) (post.Voted, error) {
	if err := post.CheckVote(req.Vote); err != nil {
		return post.Voted{}, err
	}
	v := votes[req.Vote]
	var out post.Voted
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkBoard(ctx, tx, board); err != nil {
			return err
		}
		a, err := actor(ctx, tx, board, who)
		if err != nil {
			return err
		}
		if err := a.MayAct(); err != nil {
			return err
		}
		who := a.Identity
		var pk int64
		out.ID = id
		err = tx.QueryRowContext(ctx, `SELECT p.pk, coalesce(r.n, p.n) FROM posts p LEFT JOIN posts r ON r.pk = p.thread
			WHERE p.board = ? AND p.n = ?`, board, id).Scan(&pk, &out.Thread)
		if errors.Is(err, sql.ErrNoRows) {
			return post.NoPost(board, id)
		}
		if err != nil {
			return err
		}
		// A changed vote gets a new row, so that it follows the votes
		// given that way before; the same vote keeps its row.
		if _, err := tx.ExecContext(ctx, "DELETE FROM votes WHERE post = ? AND harness = ? AND session = ? AND vote != ?",
			pk, who.Harness, who.Session, v); err != nil {
			return err
		}
		if v != 0 {
			if _, err := tx.ExecContext(ctx, `INSERT INTO votes (post, harness, session, node, vote, created)
				VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, pk, who.Harness, who.Session, who.Node, v, s.stamp()); err != nil {
				return err
			}
		}
		out.Score, out.Votes, err = votesOf(ctx, tx, pk)
		return err
	})
	return out, err
}

// votesOf returns a post's score and votes, each way in the order in which
// they were given.
func votesOf(ctx context.Context, q querier, pk int64) (int, post.Votes, error) {
	out := post.Votes{Up: []string{}, Down: []string{}}
	rs, err := q.QueryContext(ctx, "SELECT vote, harness, session, node FROM votes WHERE post = ? ORDER BY rowid", pk)
	if err != nil {
		return 0, out, err
	}
	for rs.Next() {
		var v int
		var harness, session, node string
		if err := rs.Scan(&v, &harness, &session, &node); err != nil {
			_ = rs.Close()
			return 0, out, err
		}
		if v > 0 {
			out.Up = append(out.Up, post.Author(harness, session, node))
		} else {
			out.Down = append(out.Down, post.Author(harness, session, node))
		}
	}
	return len(out.Up) - len(out.Down), out, rs.Close()
}

// Given returns, for each post of the thread that contains id, what who, or
// the name that its session carries, gave it: its vote and its reactions.
// Posts that who gave nothing are left out.
func (s *Store) Given(ctx context.Context, who post.Identity, board string, id int64) (map[int64]post.Given, error) {
	out := map[int64]post.Given{}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		root, err := rootOf(ctx, tx, board, id)
		if err != nil {
			return err
		}
		a, err := actor(ctx, tx, board, who)
		if err != nil {
			return err
		}
		who := a.Identity
		rs, err := tx.QueryContext(ctx, `SELECT p.n, v.vote FROM votes v JOIN posts p ON p.pk = v.post
			WHERE (p.pk = ? OR p.thread = ?) AND v.harness = ? AND v.session = ?`, root, root, who.Harness, who.Session)
		if err != nil {
			return err
		}
		for rs.Next() {
			var n int64
			var v int
			if err := rs.Scan(&n, &v); err != nil {
				_ = rs.Close()
				return err
			}
			g := out[n]
			g.Vote = post.VoteUp
			if v < 0 {
				g.Vote = post.VoteDown
			}
			out[n] = g
		}
		if err := rs.Close(); err != nil {
			return err
		}
		rs, err = tx.QueryContext(ctx, `SELECT p.n, x.reaction FROM reactions x JOIN posts p ON p.pk = x.post
			WHERE (p.pk = ? OR p.thread = ?) AND x.harness = ? AND x.session = ?
			ORDER BY p.n, x.rowid`, root, root, who.Harness, who.Session)
		if err != nil {
			return err
		}
		for rs.Next() {
			var n int64
			var reaction string
			if err := rs.Scan(&n, &reaction); err != nil {
				_ = rs.Close()
				return err
			}
			g := out[n]
			g.Reactions = append(g.Reactions, reaction)
			out[n] = g
		}
		if err := rs.Close(); err != nil {
			return err
		}
		for n, g := range out {
			if g.Vote == "" {
				g.Vote = post.VoteNone
				out[n] = g
			}
		}
		return nil
	})
	return out, err
}
