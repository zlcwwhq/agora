package store

import (
	"context"
	"database/sql"
	"strings"

	"github.com/zlcwwhq/agora/internal/post"
)

// madeBy returns when the current version of a post was made and by whom:
// at its latest edit, if it has one, else at its creation, and by the latest
// edit's identity, if it has one, else by its author. A post that agora v0.5
// edited has no earlier versions, and its current one is the first.
func madeBy(ctx context.Context, q querier, r row) (string, post.Actor, error) {
	var editor, by post.Actor
	err := q.QueryRowContext(ctx, `SELECT editor_harness, editor_session, editor_node, editor_carrier, editor_model, carrier, model
		FROM posts WHERE pk = ?`, r.pk).
		Scan(&editor.Harness, &editor.Session, &editor.Node, &editor.Carrier, &editor.Model, &by.Carrier, &by.Model)
	at := r.Created
	by.Identity = post.Identity{Harness: r.Harness, Session: r.Session, Node: r.Node}
	if r.Edited != "" {
		at = r.Edited
	}
	if editor.Harness != "" {
		by = editor
	}
	return at, by, err
}

// keepVersion keeps the current version of r, which an edit replaces, and
// returns the board's number for the edit.
func keepVersion(ctx context.Context, q querier, board string, r row) (int64, error) {
	at, by, err := madeBy(ctx, q, r)
	if err != nil {
		return 0, err
	}
	if _, err := q.ExecContext(ctx, `INSERT INTO versions (post, v, time, harness, session, node, title, tags, body, carrier, model)
		SELECT ?, coalesce(max(v), 0) + 1, ?, ?, ?, ?, ?, ?, ?, ?, ? FROM versions WHERE post = ?`,
		r.pk, at, by.Harness, by.Session, by.Node, r.Title, strings.Join(r.Tags, " "), r.Body, by.Carrier, by.Model, r.pk); err != nil {
		return 0, err
	}
	var n int64
	err = q.QueryRowContext(ctx, "UPDATE boards SET last_edit = last_edit + 1 WHERE id = ? RETURNING last_edit", board).Scan(&n)
	return n, err
}

// History returns every version of a post, oldest first, the last being the
// post as it is.
func (s *Store) History(ctx context.Context, board string, id int64) (post.History, error) {
	var out post.History
	err := s.tx(ctx, func(tx *sql.Tx) error {
		r, err := get(ctx, tx, board, id)
		if err != nil {
			return err
		}
		out = post.History{ID: id, Thread: r.Thread, Versions: []post.Version{}}
		rs, err := tx.QueryContext(ctx, "SELECT time, harness, session, node, title, tags, body, carrier, model FROM versions WHERE post = ? ORDER BY v", r.pk)
		if err != nil {
			return err
		}
		for rs.Next() {
			v := post.Version{Version: len(out.Versions) + 1}
			var harness, session, node, tags, carrier string
			if err := rs.Scan(&v.Time, &harness, &session, &node, &v.Title, &tags, &v.Body, &carrier, &v.Model); err != nil {
				_ = rs.Close()
				return err
			}
			v.Author = post.Author(harness, session, node)
			v.Carrier = post.Actor{Identity: post.Identity{Harness: harness, Node: node}, Carrier: carrier}.CarrierAuthor()
			v.Tags = strings.Fields(tags)
			if v.Tags == nil {
				v.Tags = []string{}
			}
			out.Versions = append(out.Versions, v)
		}
		if err := rs.Close(); err != nil {
			return err
		}
		at, by, err := madeBy(ctx, tx, r)
		out.Versions = append(out.Versions, post.Version{
			Version: len(out.Versions) + 1, Time: at, Author: by.Author(), Carrier: by.CarrierAuthor(), Model: by.Model,
			Title: r.Title, Tags: r.Tags, Body: r.Body,
		})
		return err
	})
	return out, err
}
