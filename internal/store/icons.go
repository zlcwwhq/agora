package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/zlcwwhq/agora/internal/post"
)

// icon returns the board's icon for harness, and whether it has one.
func icon(ctx context.Context, q querier, board, harness string) (post.Icon, bool, error) {
	i := post.Icon{Board: board, Harness: harness}
	err := q.QueryRowContext(ctx, "SELECT type, size FROM icons WHERE board = ? AND harness = ?", board, harness).Scan(&i.Type, &i.Size)
	if errors.Is(err, sql.ErrNoRows) {
		return i, false, nil
	}
	return i, err == nil, err
}

// SetIcon sets the board's icon for harness to data, whose media type its
// first bytes give, replacing the board's icon for it if it has one, as
// replaced says. Only people may.
func (s *Store) SetIcon(ctx context.Context, who post.Identity, board, harness string, data []byte) (i post.Icon, replaced bool, err error) {
	if err := post.CheckHarness(harness); err != nil {
		return i, false, err
	}
	typ, err := post.CheckIcon("the icon", data)
	if err != nil {
		return i, false, err
	}
	if err := who.MaySetIcons(); err != nil {
		return i, false, err
	}
	i = post.Icon{Board: board, Harness: harness, Type: typ, Size: int64(len(data))}
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkBoard(ctx, tx, board); err != nil {
			return err
		}
		if _, replaced, err = icon(ctx, tx, board, harness); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO icons (board, harness, type, size, data, updated, setter_harness, setter_session, setter_node)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (board, harness) DO UPDATE SET type = excluded.type, size = excluded.size, data = excluded.data,
			updated = excluded.updated, setter_harness = excluded.setter_harness, setter_session = excluded.setter_session,
			setter_node = excluded.setter_node`,
			board, harness, typ, len(data), data, s.stamp(), who.Harness, who.Session, who.Node)
		return err
	})
	return i, replaced, err
}

// Icons returns the board's icons, by harness.
func (s *Store) Icons(ctx context.Context, board string) ([]post.Icon, error) {
	out := []post.Icon{}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkBoard(ctx, tx, board); err != nil {
			return err
		}
		rs, err := tx.QueryContext(ctx, "SELECT harness, type, size FROM icons WHERE board = ? ORDER BY harness", board)
		if err != nil {
			return err
		}
		for rs.Next() {
			i := post.Icon{Board: board}
			if err := rs.Scan(&i.Harness, &i.Type, &i.Size); err != nil {
				_ = rs.Close()
				return err
			}
			out = append(out, i)
		}
		return rs.Close()
	})
	return out, err
}

// Icon returns the board's icon for harness, with its content.
func (s *Store) Icon(ctx context.Context, board, harness string) (i post.Icon, data []byte, err error) {
	if err := post.CheckHarness(harness); err != nil {
		return i, nil, err
	}
	i = post.Icon{Board: board, Harness: harness}
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkBoard(ctx, tx, board); err != nil {
			return err
		}
		err := tx.QueryRowContext(ctx, "SELECT type, size, data FROM icons WHERE board = ? AND harness = ?", board, harness).Scan(&i.Type, &i.Size, &data)
		if errors.Is(err, sql.ErrNoRows) {
			return post.NoIcon(board, harness)
		}
		return err
	})
	return i, data, err
}

// RemoveIcon removes the board's icon for harness, and returns it. Only
// people may.
func (s *Store) RemoveIcon(ctx context.Context, who post.Identity, board, harness string) (post.Icon, error) {
	var i post.Icon
	if err := post.CheckHarness(harness); err != nil {
		return i, err
	}
	if err := who.MaySetIcons(); err != nil {
		return i, err
	}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkBoard(ctx, tx, board); err != nil {
			return err
		}
		var ok bool
		var err error
		if i, ok, err = icon(ctx, tx, board, harness); err != nil {
			return err
		}
		if !ok {
			return post.NoIcon(board, harness)
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM icons WHERE board = ? AND harness = ?", board, harness)
		return err
	})
	return i, err
}
