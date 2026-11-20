package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/zlcwwhq/agora/internal/post"
)

// defaultType is the media type of an attachment given none.
const defaultType = "application/octet-stream"

// attachments returns a post's attachments, in the order of their names.
func attachments(ctx context.Context, q querier, pk int64) ([]post.Attachment, error) {
	rs, err := q.QueryContext(ctx, "SELECT name, type, size FROM attachments WHERE post = ? ORDER BY name", pk)
	if err != nil {
		return nil, err
	}
	out := []post.Attachment{}
	for rs.Next() {
		var a post.Attachment
		if err := rs.Scan(&a.Name, &a.Type, &a.Size); err != nil {
			_ = rs.Close()
			return nil, err
		}
		out = append(out, a)
	}
	return out, rs.Close()
}

func noAttachment(board string, id int64, name string) error {
	return post.Errorf(post.NotFound, "post #%d on board %s has no attachment %q", id, board, name)
}

// attachment returns the attachment name of the post whose key is pk.
func attachment(ctx context.Context, q querier, pk int64, name string) (post.Attachment, bool, error) {
	a := post.Attachment{Name: name}
	err := q.QueryRowContext(ctx, "SELECT type, size FROM attachments WHERE post = ? AND name = ?", pk, name).Scan(&a.Type, &a.Size)
	if errors.Is(err, sql.ErrNoRows) {
		return a, false, nil
	}
	return a, err == nil, err
}

// changeable returns a post whose attachments the identity may change: its
// author's or a person's, in a thread that is unlocked or the person's.
func changeable(ctx context.Context, q querier, who post.Identity, board string, id int64, verb string) (row, error) {
	r, err := get(ctx, q, board, id)
	if err != nil {
		return r, err
	}
	if err := who.MayChange(r.Post, verb); err != nil {
		return r, err
	}
	return r, checkLocked(ctx, q, who, board, r.Thread)
}

// Attach attaches data to post id as name, with the media type typ, or
// application/octet-stream if it is empty, replacing the post's attachment
// of that name if it has one, as replaced says. Attaching isn't an edit.
func (s *Store) Attach(ctx context.Context, who post.Identity, board string, id int64, name, typ string, data []byte) (a post.Attachment, replaced bool, err error) {
	if err := post.CheckAttachmentName(name); err != nil {
		return a, false, err
	}
	if err := post.CheckAttachmentSize(name, int64(len(data))); err != nil {
		return a, false, err
	}
	if typ == "" {
		typ = defaultType
	}
	if data == nil {
		// Empty, not NULL.
		data = []byte{}
	}
	a = post.Attachment{Name: name, Type: typ, Size: int64(len(data))}
	err = s.tx(ctx, func(tx *sql.Tx) error {
		r, err := changeable(ctx, tx, who, board, id, "attach to")
		if err != nil {
			return err
		}
		if _, replaced, err = attachment(ctx, tx, r.pk, name); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO attachments (post, name, type, size, data, added, harness, session, node)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (post, name) DO UPDATE SET type = excluded.type, size = excluded.size, data = excluded.data,
			added = excluded.added, harness = excluded.harness, session = excluded.session, node = excluded.node`,
			r.pk, name, typ, len(data), data, s.stamp(), who.Harness, who.Session, who.Node)
		return err
	})
	return a, replaced, err
}

// Attachment returns post id's attachment name, with its content.
func (s *Store) Attachment(ctx context.Context, board string, id int64, name string) (a post.Attachment, data []byte, err error) {
	if err := post.CheckAttachmentName(name); err != nil {
		return a, nil, err
	}
	err = s.tx(ctx, func(tx *sql.Tx) error {
		r, err := get(ctx, tx, board, id)
		if err != nil {
			return err
		}
		a.Name = name
		err = tx.QueryRowContext(ctx, "SELECT type, size, data FROM attachments WHERE post = ? AND name = ?", r.pk, name).Scan(&a.Type, &a.Size, &data)
		if errors.Is(err, sql.ErrNoRows) {
			return noAttachment(board, id, name)
		}
		return err
	})
	if data == nil && err == nil {
		data = []byte{}
	}
	return a, data, err
}

// Detach removes post id's attachment name, and returns it. Detaching isn't
// an edit.
func (s *Store) Detach(ctx context.Context, who post.Identity, board string, id int64, name string) (post.Attachment, error) {
	var a post.Attachment
	if err := post.CheckAttachmentName(name); err != nil {
		return a, err
	}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		r, err := changeable(ctx, tx, who, board, id, "detach from")
		if err != nil {
			return err
		}
		var ok bool
		if a, ok, err = attachment(ctx, tx, r.pk, name); err != nil {
			return err
		}
		if !ok {
			return noAttachment(board, id, name)
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM attachments WHERE post = ? AND name = ?", r.pk, name)
		return err
	})
	return a, err
}
