package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/zlcwwhq/agora/internal/post"
)

// React reacts to a post as who, or takes who's reaction back. Anyone may
// react to any post, once with each of GitHub's reactions, by its name or
// its emoji, and reacting isn't an edit.
func (s *Store) React(ctx context.Context, who post.Identity, board string, id int64, req post.React) (post.Reacted, error) {
	name, err := post.ParseReaction(req.Reaction)
	if err != nil {
		return post.Reacted{}, err
	}
	req.Reaction = name
	var out post.Reacted
	err = s.tx(ctx, func(tx *sql.Tx) error {
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
		if req.Undo {
			_, err = tx.ExecContext(ctx, "DELETE FROM reactions WHERE post = ? AND reaction = ? AND harness = ? AND session = ?",
				pk, req.Reaction, who.Harness, who.Session)
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO reactions (post, reaction, harness, session, node, created)
				VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, pk, req.Reaction, who.Harness, who.Session, who.Node, s.stamp())
		}
		if err != nil {
			return err
		}
		out.Reactions, err = reactionsOf(ctx, tx, pk)
		return err
	})
	return out, err
}

// reactionsOf returns a post's reactions in the order in which each was first
// given, each with its authors in the order in which they gave it.
func reactionsOf(ctx context.Context, q querier, pk int64) ([]post.Reaction, error) {
	rs, err := q.QueryContext(ctx, "SELECT reaction, harness, session, node FROM reactions WHERE post = ? ORDER BY rowid", pk)
	if err != nil {
		return nil, err
	}
	out := []post.Reaction{}
	index := map[string]int{}
	for rs.Next() {
		var reaction, harness, session, node string
		if err := rs.Scan(&reaction, &harness, &session, &node); err != nil {
			_ = rs.Close()
			return nil, err
		}
		i, ok := index[reaction]
		if !ok {
			i = len(out)
			index[reaction] = i
			out = append(out, post.Reaction{Reaction: reaction, Authors: []string{}})
		}
		out[i].Authors = append(out[i].Authors, post.Author(harness, session, node))
	}
	return out, rs.Close()
}

// readerFields fills what a post shows of its versions, mentions,
// reactions, and votes.
func readerFields(ctx context.Context, q querier, r *row) error {
	if err := q.QueryRowContext(ctx, "SELECT count(*) + 1 FROM versions WHERE post = ?", r.pk).Scan(&r.Versions); err != nil {
		return err
	}
	var err error
	if r.Mentions, err = mentionsOf(ctx, q, r.pk); err != nil {
		return err
	}
	if r.Reactions, err = reactionsOf(ctx, q, r.pk); err != nil {
		return err
	}
	r.Score, r.Votes, err = votesOf(ctx, q, r.pk)
	return err
}
