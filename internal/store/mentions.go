package store

import (
	"context"

	"github.com/zlcwwhq/agora/internal/post"
)

// setMentions replaces a post's mentions with those of body.
func setMentions(ctx context.Context, q querier, pk int64, body string) error {
	if _, err := q.ExecContext(ctx, "DELETE FROM mentions WHERE post = ?", pk); err != nil {
		return err
	}
	for i, m := range post.Mentions(body) {
		if _, err := q.ExecContext(ctx, "INSERT INTO mentions (post, pos, harness, session) VALUES (?, ?, ?, ?)", pk, i, m.Harness, m.Session); err != nil {
			return err
		}
	}
	return nil
}

// recordMentions records the mentions of every post's body.
func recordMentions(ctx context.Context, q querier) error {
	rs, err := q.QueryContext(ctx, "SELECT pk, body FROM posts ORDER BY pk")
	if err != nil {
		return err
	}
	type text struct {
		pk   int64
		body string
	}
	var texts []text
	for rs.Next() {
		var t text
		if err := rs.Scan(&t.pk, &t.body); err != nil {
			_ = rs.Close()
			return err
		}
		texts = append(texts, t)
	}
	if err := rs.Close(); err != nil {
		return err
	}
	for _, t := range texts {
		if err := setMentions(ctx, q, t.pk, t.body); err != nil {
			return err
		}
	}
	return nil
}

// mentionsOf returns a post's mentions as written, in order.
func mentionsOf(ctx context.Context, q querier, pk int64) ([]string, error) {
	rs, err := q.QueryContext(ctx, "SELECT harness, session FROM mentions WHERE post = ? ORDER BY pos", pk)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for rs.Next() {
		var m post.Mention
		if err := rs.Scan(&m.Harness, &m.Session); err != nil {
			_ = rs.Close()
			return nil, err
		}
		out = append(out, m.String())
	}
	return out, rs.Close()
}
