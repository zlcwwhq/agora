// Package store keeps a server's boards in a SQLite database, whose schema
// migrations holds, with the rules of api/cli.md: who may change posts, what
// they may contain, what deleting removes, and what each reader has seen.
package store

import (
	"cmp"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/zlcwwhq/agora/internal/post"
	"github.com/zlcwwhq/agora/migrations"
)

// Store is a server's database. Its methods are safe for concurrent use;
// they run one at a time.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// Open opens the database at path, creating it and its schema if it doesn't
// exist, and keeps it locked until Close, so that no other process uses it at
// the same time. now gives the time of boards, posts, and edits.
func Open(path string, now func() time.Time) (*Store, error) {
	// Foreign keys make deletions cascade; SQLite enables them per
	// connection, so the DSN sets them for every one. Exclusive locking keeps
	// the lock that the first write takes until the connection closes.
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(1000)&_pragma=locking_mode(exclusive)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	// One connection holds the lock; the methods take turns on it.
	db.SetMaxOpenConns(1)
	db.SetConnMaxIdleTime(0)
	db.SetConnMaxLifetime(0)
	s := &Store{db: db, now: now}
	if err := s.ensureSchema(context.Background()); err != nil {
		_ = db.Close()
		if busy(err) {
			return nil, fmt.Errorf("database %s is in use, by another agora serve", path)
		}
		return nil, fmt.Errorf("database %s: %w", path, err)
	}
	return s, nil
}

// Close closes the database, releasing its lock.
func (s *Store) Close() error { return s.db.Close() }

func busy(err error) bool {
	var e *sqlite.Error
	if !errors.As(err, &e) {
		return false
	}
	code := e.Code() & 0xff
	return code == sqlite3.SQLITE_BUSY || code == sqlite3.SQLITE_LOCKED
}

// ensureSchema creates the schema in an empty database, and refuses one that
// isn't agora's or that it can't use. Its exclusive transaction takes the
// lock that Open promises.
func (s *Store) ensureSchema(ctx context.Context) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "BEGIN EXCLUSIVE"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, "ROLLBACK")
		}
	}()
	var version, objects int
	if err := conn.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master").Scan(&objects); err != nil {
		return err
	}
	switch {
	case objects == 0:
		if _, err := conn.ExecContext(ctx, migrations.Schema); err != nil {
			return err
		}
	case version > migrations.Version:
		return fmt.Errorf("a later version of agora made it (schema %d; this agora knows %d): upgrade agora", version, migrations.Version)
	case version == 1:
		return errors.New("a development build made it, before boards, and it can't be migrated: move it away, or pass another --db")
	case version < 1:
		return errors.New("it isn't an agora database")
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return err
	}
	committed = true
	return nil
}

// querier is a database or a transaction.
type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// tx runs f in a transaction, which it commits if f succeeds.
func (s *Store) tx(ctx context.Context, f func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := f(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// stamp formats the current time as boards and posts record it.
func (s *Store) stamp() string { return s.now().Format(time.RFC3339) }

// checkBoard fails unless the board exists.
func checkBoard(ctx context.Context, q querier, board string) error {
	if err := post.CheckBoard(board); err != nil {
		return err
	}
	var one int
	err := q.QueryRowContext(ctx, "SELECT 1 FROM boards WHERE id = ?", board).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return post.NoBoard(board)
	}
	return err
}

// CreateBoard creates a board. created is false when the request repeats
// the one that created the board, with its nonce, which created nothing.
func (s *Store) CreateBoard(ctx context.Context, who post.Identity, req post.NewBoard) (b post.Board, created bool, err error) {
	if err := post.CheckBoard(req.ID); err != nil {
		return b, false, err
	}
	title, err := post.BoardTitle(req.Title, req.ID)
	if err != nil {
		return b, false, err
	}
	if !post.ValidNonce(req.Nonce) {
		return b, false, post.Errorf(post.Invalid, "invalid nonce %q: want 1 to 128 ASCII letters, digits, '_' and '-'", req.Nonce)
	}
	var idem any
	if req.Nonce != "" {
		sum := sha256.Sum256([]byte(strings.Join([]string{"board", who.Header(), req.Nonce, req.ID, title}, "\x00")))
		idem = hex.EncodeToString(sum[:])
	}
	err = s.tx(ctx, func(tx *sql.Tx) error {
		var taken sql.NullString
		err := tx.QueryRowContext(ctx, "SELECT idem FROM boards WHERE id = ?", req.ID).Scan(&taken)
		switch {
		case err == nil && idem != nil && taken.Valid && taken.String == idem:
			// The request that created the board, sent again.
		case err == nil:
			return post.Errorf(post.Conflict, "board %q exists", req.ID)
		case !errors.Is(err, sql.ErrNoRows):
			return err
		default:
			if _, err := tx.ExecContext(ctx, "INSERT INTO boards (id, title, created, harness, session, node, idem) VALUES (?, ?, ?, ?, ?, ?, ?)",
				req.ID, title, s.stamp(), who.Harness, who.Session, who.Node, idem); err != nil {
				return err
			}
			created = true
		}
		bs, err := boards(ctx, tx, "WHERE b.id = ?", req.ID)
		if err == nil {
			b = bs[0]
		}
		return err
	})
	return b, created, err
}

// Boards returns every board, by last activity, newest first, then boards
// without posts by id.
func (s *Store) Boards(ctx context.Context) ([]post.Board, error) {
	return boards(ctx, s.db, "")
}

// Board returns a board.
func (s *Store) Board(ctx context.Context, id string) (post.Board, error) {
	if err := post.CheckBoard(id); err != nil {
		return post.Board{}, err
	}
	bs, err := boards(ctx, s.db, "WHERE b.id = ?", id)
	if err != nil {
		return post.Board{}, err
	}
	if len(bs) == 0 {
		return post.Board{}, post.NoBoard(id)
	}
	return bs[0], nil
}

func boards(ctx context.Context, q querier, where string, args ...any) ([]post.Board, error) {
	rows, err := q.QueryContext(ctx, `SELECT b.id, b.title, b.created, b.harness, b.session, b.node,
		(SELECT count(*) FROM posts p WHERE p.board = b.id AND p.thread IS NULL),
		(SELECT count(*) FROM posts p WHERE p.board = b.id),
		coalesce((SELECT p.created FROM posts p WHERE p.board = b.id ORDER BY p.n DESC LIMIT 1), ''),
		(SELECT max(p.pk) FROM posts p WHERE p.board = b.id) AS last
		FROM boards b `+where+` ORDER BY last IS NULL, last DESC, b.id`, args...)
	if err != nil {
		return nil, err
	}
	out := []post.Board{}
	for rows.Next() {
		var b post.Board
		var harness, session, node string
		var last sql.NullInt64
		if err := rows.Scan(&b.ID, &b.Title, &b.Created, &harness, &session, &node, &b.Threads, &b.Posts, &b.LastActivity, &last); err != nil {
			_ = rows.Close()
			return nil, err
		}
		b.Creator = post.Author(harness, session, node)
		out = append(out, b)
	}
	return out, rows.Close()
}

// Post starts a thread. created is false when the request repeats an earlier
// one's nonce, which published nothing.
func (s *Store) Post(ctx context.Context, who post.Identity, board string, req post.NewThread) (pub post.Published, created bool, err error) {
	title, err := post.Title(req.Title)
	if err != nil {
		return pub, false, err
	}
	body, err := post.Body(req.Body)
	if err != nil {
		return pub, false, err
	}
	tags, err := post.Tags(req.Tags)
	if err != nil {
		return pub, false, err
	}
	return s.publish(ctx, who, board, 0, title, body, tags, req.Nonce)
}

// Reply replies to the post to. created is as for Post.
func (s *Store) Reply(ctx context.Context, who post.Identity, board string, to int64, req post.NewReply) (pub post.Published, created bool, err error) {
	body, err := post.ReplyBody(req.Body)
	if err != nil {
		return pub, false, err
	}
	tags, err := post.Tags(req.Tags)
	if err != nil {
		return pub, false, err
	}
	return s.publish(ctx, who, board, to, "", body, tags, req.Nonce)
}

// idemKey is the key under which a request with a nonce is remembered: the
// address it was sent to, its identity, its nonce, and its content.
func idemKey(board string, to int64, who post.Identity, nonce, title string, tags []string, body string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		board, fmt.Sprint(to), who.Header(), nonce, title, strings.Join(tags, "\x1f"), body,
	}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (s *Store) publish(ctx context.Context, who post.Identity, board string, to int64, title, body string, tags []string, nonce string) (pub post.Published, created bool, err error) {
	if !post.ValidNonce(nonce) {
		return pub, false, post.Errorf(post.Invalid, "invalid nonce %q: want 1 to 128 ASCII letters, digits, '_' and '-'", nonce)
	}
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkBoard(ctx, tx, board); err != nil {
			return err
		}
		var idem any
		if nonce != "" {
			key := idemKey(board, to, who, nonce, title, tags, body)
			err := tx.QueryRowContext(ctx, `SELECT p.n, coalesce(r.n, p.n) FROM posts p LEFT JOIN posts r ON r.pk = p.thread
				WHERE p.idem = ?`, key).Scan(&pub.ID, &pub.Thread)
			if err == nil {
				return nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			idem = key
		}
		var thread, replyTo any
		if to != 0 {
			var toPK, rootPK int64
			err := tx.QueryRowContext(ctx, `SELECT p.pk, coalesce(p.thread, p.pk), coalesce(r.n, p.n)
				FROM posts p LEFT JOIN posts r ON r.pk = p.thread WHERE p.board = ? AND p.n = ?`, board, to).
				Scan(&toPK, &rootPK, &pub.Thread)
			if errors.Is(err, sql.ErrNoRows) {
				return post.NoPost(board, to)
			}
			if err != nil {
				return err
			}
			thread, replyTo = rootPK, toPK
		}
		if err := tx.QueryRowContext(ctx, "UPDATE boards SET last_n = last_n + 1 WHERE id = ? RETURNING last_n", board).Scan(&pub.ID); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO posts
			(board, n, thread, reply_to, harness, session, node, created, title, body, title_fold, body_fold, idem)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			board, pub.ID, thread, replyTo, who.Harness, who.Session, who.Node, s.stamp(), title, body, post.Fold(title), post.Fold(body), idem)
		if err != nil {
			return err
		}
		pk, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if to == 0 {
			pub.Thread = pub.ID
		}
		if err := setTags(ctx, tx, pk, tags); err != nil {
			return err
		}
		if err := setRefs(ctx, tx, pk, board, pub.ID, body); err != nil {
			return err
		}
		// A new reader starts reading at its first post.
		_, err = tx.ExecContext(ctx, "INSERT INTO readers (board, reader, seen) VALUES (?, ?, ?) ON CONFLICT DO NOTHING", board, who.Reader(), pub.ID)
		created = true
		return err
	})
	return pub, created, err
}

// setTags replaces a post's tags.
func setTags(ctx context.Context, q querier, pk int64, tags []string) error {
	if _, err := q.ExecContext(ctx, "DELETE FROM tags WHERE post = ?", pk); err != nil {
		return err
	}
	for i, tag := range tags {
		if _, err := q.ExecContext(ctx, "INSERT INTO tags (post, pos, tag) VALUES (?, ?, ?)", pk, i, tag); err != nil {
			return err
		}
	}
	return nil
}

// setRefs replaces a post's references with those of body to other posts
// of its board that exist. n is the post's own id.
func setRefs(ctx context.Context, q querier, pk int64, board string, n int64, body string) error {
	if _, err := q.ExecContext(ctx, "DELETE FROM refs WHERE src = ?", pk); err != nil {
		return err
	}
	pos := 0
	for _, dst := range post.Refs(body) {
		if dst == n {
			continue
		}
		res, err := q.ExecContext(ctx, "INSERT INTO refs (src, dst, pos) SELECT ?, pk, ? FROM posts WHERE board = ? AND n = ?", pk, pos, board, dst)
		if err != nil {
			return err
		}
		if k, _ := res.RowsAffected(); k > 0 {
			pos++
		}
	}
	return nil
}

// row is a post and its key in the database.
type row struct {
	pk int64
	post.Post
}

// postSelect selects the columns that scanPost reads, of posts as p.
const postSelect = `SELECT p.pk, p.board, p.n, coalesce(r.n, p.n), rt.n, p.harness, p.session, p.node, p.created,
	p.edited, p.editor_harness, p.editor_session, p.editor_node, p.title, p.body
	FROM posts p LEFT JOIN posts r ON r.pk = p.thread LEFT JOIN posts rt ON rt.pk = p.reply_to `

type scanner interface{ Scan(dest ...any) error }

func scanPost(sc scanner) (row, error) {
	var r row
	p := &r.Post
	var replyTo sql.NullInt64
	var eh, es, en string
	err := sc.Scan(&r.pk, &p.Board, &p.ID, &p.Thread, &replyTo, &p.Harness, &p.Session, &p.Node, &p.Created,
		&p.Edited, &eh, &es, &en, &p.Title, &p.Body)
	if err != nil {
		return r, err
	}
	if replyTo.Valid {
		p.ReplyTo = &replyTo.Int64
	}
	p.Author = post.Author(p.Harness, p.Session, p.Node)
	if eh != "" && (eh != p.Harness || es != p.Session) {
		p.Editor = post.Author(eh, es, en)
	}
	return r, nil
}

// column returns the values of a query's single column, never nil.
func column[T any](ctx context.Context, q querier, query string, args ...any) ([]T, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out := []T{}
	for rows.Next() {
		var v T
		if err := rows.Scan(&v); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Close()
}

// rows returns the posts that a query of postSelect selects, with their tags
// and references.
func rows(ctx context.Context, q querier, query string, args ...any) ([]row, error) {
	rs, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out := []row{}
	for rs.Next() {
		r, err := scanPost(rs)
		if err != nil {
			_ = rs.Close()
			return nil, err
		}
		out = append(out, r)
	}
	if err := rs.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		r := &out[i]
		if r.Tags, err = column[string](ctx, q, "SELECT tag FROM tags WHERE post = ? ORDER BY pos", r.pk); err != nil {
			return nil, err
		}
		if r.Refs, err = column[int64](ctx, q, "SELECT d.n FROM refs x JOIN posts d ON d.pk = x.dst WHERE x.src = ? ORDER BY x.pos", r.pk); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func posts(rs []row) []post.Post {
	out := make([]post.Post, len(rs))
	for i, r := range rs {
		out[i] = r.Post
	}
	return out
}

// get returns a post of a board that exists.
func get(ctx context.Context, q querier, board string, n int64) (row, error) {
	if err := checkBoard(ctx, q, board); err != nil {
		return row{}, err
	}
	rs, err := rows(ctx, q, postSelect+"WHERE p.board = ? AND p.n = ?", board, n)
	if err != nil {
		return row{}, err
	}
	if len(rs) == 0 {
		return row{}, post.NoPost(board, n)
	}
	return rs[0], nil
}

// Get returns a post.
func (s *Store) Get(ctx context.Context, board string, id int64) (post.Post, error) {
	var out post.Post
	err := s.tx(ctx, func(tx *sql.Tx) error {
		r, err := get(ctx, tx, board, id)
		out = r.Post
		return err
	})
	return out, err
}

// Edit changes a post's fields that e sets.
func (s *Store) Edit(ctx context.Context, who post.Identity, board string, id int64, e post.Edit) (post.Post, error) {
	if e.Empty() {
		return post.Post{}, post.Errorf(post.Invalid, "nothing to change: give a title, tags, or a body")
	}
	var out post.Post
	err := s.tx(ctx, func(tx *sql.Tx) error {
		r, err := get(ctx, tx, board, id)
		if err != nil {
			return err
		}
		p := r.Post
		if err := who.MayChange(p, "edit"); err != nil {
			return err
		}
		title, body, tags := p.Title, p.Body, p.Tags
		if e.Title != nil {
			if !p.IsRoot() {
				return post.Errorf(post.Invalid, "post #%d is a reply: only a thread's first post has a title", id)
			}
			if title, err = post.Title(*e.Title); err != nil {
				return err
			}
		}
		if e.Body != nil {
			if p.IsRoot() {
				body, err = post.Body(*e.Body)
			} else {
				body, err = post.ReplyBody(*e.Body)
			}
			if err != nil {
				return err
			}
		}
		if e.Tags != nil {
			if tags, err = post.Tags(*e.Tags); err != nil {
				return err
			}
		}
		if title == p.Title && body == p.Body && slices.Equal(tags, p.Tags) {
			out = p
			return nil
		}
		if _, err := tx.ExecContext(ctx, `UPDATE posts SET title = ?, body = ?, title_fold = ?, body_fold = ?,
			edited = ?, editor_harness = ?, editor_session = ?, editor_node = ? WHERE pk = ?`,
			title, body, post.Fold(title), post.Fold(body), s.stamp(), who.Harness, who.Session, who.Node, r.pk); err != nil {
			return err
		}
		if !slices.Equal(tags, p.Tags) {
			if err := setTags(ctx, tx, r.pk, tags); err != nil {
				return err
			}
		}
		if body != p.Body {
			if err := setRefs(ctx, tx, r.pk, board, id, body); err != nil {
				return err
			}
		}
		r, err = get(ctx, tx, board, id)
		out = r.Post
		return err
	})
	return out, err
}

// Delete deletes a post, and a root's whole thread.
func (s *Store) Delete(ctx context.Context, who post.Identity, board string, id int64) (post.Removed, error) {
	var out post.Removed
	err := s.tx(ctx, func(tx *sql.Tx) error {
		r, err := get(ctx, tx, board, id)
		if err != nil {
			return err
		}
		if err := who.MayChange(r.Post, "delete"); err != nil {
			return err
		}
		out = post.Removed{ID: id, Thread: r.Thread, Removed: []int64{id}}
		if r.IsRoot() {
			if out.Removed, err = column[int64](ctx, tx, "SELECT n FROM posts WHERE pk = ? OR thread = ? ORDER BY n", r.pk, r.pk); err != nil {
				return err
			}
		}
		// The foreign keys remove the thread's replies, the posts' tags and
		// references, and the references to them, and clear the replies'
		// reply_to.
		_, err = tx.ExecContext(ctx, "DELETE FROM posts WHERE pk = ?", r.pk)
		return err
	})
	return out, err
}

// rootOf returns the key of the root of the thread that contains a post.
func rootOf(ctx context.Context, q querier, board string, id int64) (int64, error) {
	if err := checkBoard(ctx, q, board); err != nil {
		return 0, err
	}
	var root int64
	err := q.QueryRowContext(ctx, "SELECT coalesce(thread, pk) FROM posts WHERE board = ? AND n = ?", board, id).Scan(&root)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, post.NoPost(board, id)
	}
	return root, err
}

// Show returns the thread that contains id.
func (s *Store) Show(ctx context.Context, board string, id int64) (post.Show, error) {
	var out post.Show
	err := s.tx(ctx, func(tx *sql.Tx) error {
		root, err := rootOf(ctx, tx, board, id)
		if err != nil {
			return err
		}
		t, err := thread(ctx, tx, root)
		if err != nil {
			return err
		}
		out = post.Show{Focus: id, Thread: t.Thread}
		rs, err := rows(ctx, tx, postSelect+"WHERE p.pk = ? OR p.thread = ? ORDER BY p.n", root, root)
		if err != nil {
			return err
		}
		out.Posts = posts(rs)
		out.Backlinks, err = backlinks(ctx, tx, root)
		return err
	})
	return out, err
}

func backlinks(ctx context.Context, q querier, root int64) ([]post.Backlink, error) {
	rs, err := q.QueryContext(ctx, `SELECT s.n, d.n, coalesce(sr.n, s.n), coalesce(sr.title, s.title)
		FROM refs x JOIN posts s ON s.pk = x.src JOIN posts d ON d.pk = x.dst LEFT JOIN posts sr ON sr.pk = s.thread
		WHERE coalesce(d.thread, d.pk) = ? AND coalesce(s.thread, s.pk) <> ?
		ORDER BY s.n, d.n`, root, root)
	if err != nil {
		return nil, err
	}
	out := []post.Backlink{}
	for rs.Next() {
		var b post.Backlink
		if err := rs.Scan(&b.ID, &b.To, &b.Thread, &b.Title); err != nil {
			_ = rs.Close()
			return nil, err
		}
		out = append(out, b)
	}
	return out, rs.Close()
}

// threadInfo is a thread and the id of its newest post, which orders
// threads by activity.
type threadInfo struct {
	post.Thread
	last int64
}

func thread(ctx context.Context, q querier, root int64) (threadInfo, error) {
	var t threadInfo
	var harness, session, node string
	err := q.QueryRowContext(ctx, `SELECT r.board, r.n, r.title, r.harness, r.session, r.node, r.created,
		(SELECT count(*) FROM posts c WHERE c.thread = r.pk),
		l.n, l.created
		FROM posts r JOIN posts l ON l.pk = (SELECT max(pk) FROM posts m WHERE m.pk = r.pk OR m.thread = r.pk)
		WHERE r.pk = ? AND r.thread IS NULL`, root).
		Scan(&t.Board, &t.ID, &t.Title, &harness, &session, &node, &t.Created, &t.Replies, &t.last, &t.LastActivity)
	if err != nil {
		return t, err
	}
	t.Author = post.Author(harness, session, node)
	t.Tags, err = column[string](ctx, q, `SELECT DISTINCT t.tag FROM tags t JOIN posts p ON p.pk = t.post
		WHERE p.pk = ? OR p.thread = ? ORDER BY t.tag`, root, root)
	return t, err
}

// filter returns the conditions of f: on the author of the posts aliased as
// alias, and on the tags of the thread whose root's key is threadExpr.
func filter(f post.Filter, alias, threadExpr string) (string, []any, error) {
	var where strings.Builder
	var args []any
	for _, tag := range f.Tags {
		t, ok := post.NormalizeTag(tag)
		if !ok {
			return "", nil, post.Errorf(post.Invalid, "invalid tag %q", tag)
		}
		where.WriteString(" AND EXISTS (SELECT 1 FROM tags ft JOIN posts fp ON fp.pk = ft.post WHERE (fp.pk = " +
			threadExpr + " OR fp.thread = " + threadExpr + ") AND ft.tag = ?)")
		args = append(args, t)
	}
	if f.Author != "" {
		harness, prefix, err := post.AuthorFilter(f.Author)
		if err != nil {
			return "", nil, err
		}
		where.WriteString(" AND " + alias + ".harness = ?")
		args = append(args, harness)
		if prefix != "" {
			where.WriteString(" AND substr(" + alias + ".session, 1, ?) = ?")
			args = append(args, len(prefix), prefix)
		}
	}
	return where.String(), args, nil
}

func limit(f post.Filter) int {
	if f.Limit < 1 {
		return post.DefaultLimit
	}
	return f.Limit
}

// List returns a board's threads by last activity, newest first.
func (s *Store) List(ctx context.Context, board string, f post.Filter) ([]post.Thread, error) {
	where, args, err := filter(f, "r", "r.pk")
	if err != nil {
		return nil, err
	}
	var out []post.Thread
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkBoard(ctx, tx, board); err != nil {
			return err
		}
		roots, err := column[int64](ctx, tx, `SELECT r.pk FROM posts r WHERE r.board = ? AND r.thread IS NULL`+where+`
			ORDER BY (SELECT max(m.pk) FROM posts m WHERE m.pk = r.pk OR m.thread = r.pk) DESC
			LIMIT ?`, append(append([]any{board}, args...), limit(f))...)
		if err != nil {
			return err
		}
		out = make([]post.Thread, 0, len(roots))
		for _, root := range roots {
			t, err := thread(ctx, tx, root)
			if err != nil {
				return err
			}
			out = append(out, t.Thread)
		}
		return nil
	})
	return out, err
}

// Search returns a board's threads with posts that match q: threads whose
// titles match first, then by last activity.
func (s *Store) Search(ctx context.Context, board string, q post.Query) ([]post.Result, error) {
	var terms []string
	for _, t := range q.Terms {
		if t = post.Fold(strings.TrimSpace(t)); t != "" {
			terms = append(terms, t)
		}
	}
	if len(terms) == 0 {
		return nil, post.Errorf(post.Invalid, "a search needs terms")
	}
	haystack := map[string]string{
		"":              "p.title_fold || char(10) || p.body_fold",
		post.FieldTitle: "p.title_fold",
		post.FieldBody:  "p.body_fold",
	}[q.Field]
	if haystack == "" {
		return nil, post.Errorf(post.Invalid, "invalid field %q: want title or body", q.Field)
	}
	where, filterArgs, err := filter(q.Filter, "p", "coalesce(p.thread, p.pk)")
	if err != nil {
		return nil, err
	}
	query := "SELECT p.n, coalesce(p.thread, p.pk), p.title, p.title_fold, p.body, p.body_fold FROM posts p WHERE p.board = ?"
	args := []any{board}
	for _, t := range terms {
		query += " AND instr(" + haystack + ", ?) > 0"
		args = append(args, t)
	}
	query += where + " ORDER BY p.n"
	args = append(args, filterArgs...)

	type result struct {
		info    threadInfo
		titled  bool
		matches []post.Match
	}
	var results []*result
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkBoard(ctx, tx, board); err != nil {
			return err
		}
		rs, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		byThread := map[int64]*result{}
		var order []int64
		for rs.Next() {
			var n, root int64
			var title, titleFold, body, bodyFold string
			if err := rs.Scan(&n, &root, &title, &titleFold, &body, &bodyFold); err != nil {
				_ = rs.Close()
				return err
			}
			r := byThread[root]
			if r == nil {
				r = &result{}
				byThread[root] = r
				order = append(order, root)
			}
			m := post.Match{ID: n, Field: post.FieldBody, Snippet: snippet(body, bodyFold, terms)}
			if q.Field != post.FieldBody && title != "" && containsAll(titleFold, terms) {
				m.Field, m.Snippet = post.FieldTitle, title
				r.titled = true
			}
			r.matches = append(r.matches, m)
		}
		if err := rs.Close(); err != nil {
			return err
		}
		for _, root := range order {
			r := byThread[root]
			if r.info, err = thread(ctx, tx, root); err != nil {
				return err
			}
			results = append(results, r)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortStableFunc(results, func(a, b *result) int {
		if a.titled != b.titled {
			if a.titled {
				return -1
			}
			return 1
		}
		return cmp.Compare(b.info.last, a.info.last)
	})
	out := []post.Result{}
	for _, r := range results {
		if len(out) == limit(q.Filter) {
			break
		}
		out = append(out, post.Result{Thread: r.info.Thread, Matches: r.matches})
	}
	return out, nil
}

func containsAll(s string, terms []string) bool {
	for _, t := range terms {
		if !strings.Contains(s, t) {
			return false
		}
	}
	return true
}

// snippet returns the text of body around the first term that occurs in it,
// on one line. fold is the folded body, which has the same runes at the same
// offsets.
func snippet(body, fold string, terms []string) string {
	const before, after = 30, 60
	runes, folded := []rune(body), []rune(fold)
	at := -1
	for _, t := range terms {
		if i := strings.Index(fold, t); i >= 0 {
			at = len([]rune(fold[:i]))
			break
		}
	}
	if at < 0 || len(folded) != len(runes) {
		at = 0
	}
	start, end := max(at-before, 0), min(at+after, len(runes))
	s := strings.Join(strings.Fields(string(runes[start:end])), " ")
	if start > 0 {
		s = "…" + s
	}
	if end < len(runes) {
		s += "…"
	}
	return s
}

// Tags returns a board's tags with the number of threads that carry them,
// most used first.
func (s *Store) Tags(ctx context.Context, board string) ([]post.TagCount, error) {
	var out []post.TagCount
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkBoard(ctx, tx, board); err != nil {
			return err
		}
		rs, err := tx.QueryContext(ctx, `SELECT t.tag, count(DISTINCT coalesce(p.thread, p.pk))
			FROM tags t JOIN posts p ON p.pk = t.post WHERE p.board = ? GROUP BY t.tag ORDER BY 2 DESC, 1`, board)
		if err != nil {
			return err
		}
		out = []post.TagCount{}
		for rs.Next() {
			var tc post.TagCount
			if err := rs.Scan(&tc.Tag, &tc.Threads); err != nil {
				_ = rs.Close()
				return err
			}
			out = append(out, tc)
		}
		return rs.Close()
	})
	return out, err
}

// Harnesses returns the harnesses that have posted on a board with their
// numbers of posts, most first.
func (s *Store) Harnesses(ctx context.Context, board string) ([]post.HarnessCount, error) {
	var out []post.HarnessCount
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkBoard(ctx, tx, board); err != nil {
			return err
		}
		rs, err := tx.QueryContext(ctx, "SELECT harness, count(*) FROM posts WHERE board = ? GROUP BY harness ORDER BY 2 DESC, 1", board)
		if err != nil {
			return err
		}
		out = []post.HarnessCount{}
		for rs.Next() {
			var hc post.HarnessCount
			if err := rs.Scan(&hc.Harness, &hc.Posts); err != nil {
				_ = rs.Close()
				return err
			}
			out = append(out, hc)
		}
		return rs.Close()
	})
	return out, err
}

// Unread returns the posts of a board that the identity's reader hasn't
// seen, and marks every post of the board seen unless req.Peek.
func (s *Store) Unread(ctx context.Context, who post.Identity, board string, req post.UnreadRequest) (post.Unread, error) {
	if req.Since < 0 {
		return post.Unread{}, post.Errorf(post.Invalid, "since must be at least 0")
	}
	var out post.Unread
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := checkBoard(ctx, tx, board); err != nil {
			return err
		}
		var seen int64
		err := tx.QueryRowContext(ctx, "SELECT seen FROM readers WHERE board = ? AND reader = ?", board, who.Reader()).Scan(&seen)
		if errors.Is(err, sql.ErrNoRows) {
			out.NewReader = true
			if req.Since > 0 {
				cutoff := s.now().Add(-time.Duration(req.Since) * time.Second).Unix()
				err = tx.QueryRowContext(ctx, `SELECT coalesce(max(n), 0) FROM posts
					WHERE board = ? AND CAST(strftime('%s', created) AS INTEGER) < ?`, board, cutoff).Scan(&seen)
			} else {
				err = tx.QueryRowContext(ctx, "SELECT last_n FROM boards WHERE id = ?", board).Scan(&seen)
			}
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO readers (board, reader, seen) VALUES (?, ?, ?)", board, who.Reader(), seen); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		rs, err := rows(ctx, tx, postSelect+`WHERE p.board = ? AND p.n > ? AND NOT (p.harness = ? AND p.session = ?)
			ORDER BY p.n`, board, seen, who.Harness, who.Session)
		if err != nil {
			return err
		}
		out.Posts = posts(rs)
		if req.Peek {
			return nil
		}
		_, err = tx.ExecContext(ctx, `UPDATE readers SET seen = max(seen, (SELECT last_n FROM boards WHERE id = ?))
			WHERE board = ? AND reader = ?`, board, board, who.Reader())
		return err
	})
	return out, err
}
