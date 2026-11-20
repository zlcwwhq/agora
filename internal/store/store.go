// Package store keeps the board in a SQLite database, whose schema
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

// Store is a board's database. Its methods are safe for concurrent use; they
// run one at a time.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// Open opens the database at path, creating it and its schema if it doesn't
// exist, and keeps it locked until Close, so that no other process uses it at
// the same time. now gives the time of posts and edits.
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
// isn't the board's or comes from a later agora. Its exclusive transaction
// takes the lock that Open promises.
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
	case version < 1:
		return errors.New("it isn't an agora board")
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

// stamp formats the current time as posts record it.
func (s *Store) stamp() string { return s.now().Format(time.RFC3339) }

// Post starts a thread. created is false when the request repeats an earlier
// one's nonce, which published nothing.
func (s *Store) Post(ctx context.Context, who post.Identity, req post.NewThread) (pub post.Published, created bool, err error) {
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
	return s.publish(ctx, who, 0, title, body, tags, req.Nonce)
}

// Reply replies to the post to. created is as for Post.
func (s *Store) Reply(ctx context.Context, who post.Identity, to int64, req post.NewReply) (pub post.Published, created bool, err error) {
	body, err := post.ReplyBody(req.Body)
	if err != nil {
		return pub, false, err
	}
	tags, err := post.Tags(req.Tags)
	if err != nil {
		return pub, false, err
	}
	return s.publish(ctx, who, to, "", body, tags, req.Nonce)
}

// idemKey is the key under which a request with a nonce is remembered: the
// address it was sent to, its identity, its nonce, and its content.
func idemKey(to int64, who post.Identity, nonce, title string, tags []string, body string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		fmt.Sprint(to), who.Header(), nonce, title, strings.Join(tags, "\x1f"), body,
	}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (s *Store) publish(ctx context.Context, who post.Identity, to int64, title, body string, tags []string, nonce string) (pub post.Published, created bool, err error) {
	if !post.ValidNonce(nonce) {
		return pub, false, post.Errorf(post.Invalid, "invalid nonce %q: want 1 to 128 ASCII letters, digits, '_' and '-'", nonce)
	}
	err = s.tx(ctx, func(tx *sql.Tx) error {
		var idem any
		if nonce != "" {
			key := idemKey(to, who, nonce, title, tags, body)
			err := tx.QueryRowContext(ctx, "SELECT id, coalesce(thread, id) FROM posts WHERE idem = ?", key).Scan(&pub.ID, &pub.Thread)
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
			var root int64
			err := tx.QueryRowContext(ctx, "SELECT coalesce(thread, id) FROM posts WHERE id = ?", to).Scan(&root)
			if errors.Is(err, sql.ErrNoRows) {
				return post.NoPost(to)
			}
			if err != nil {
				return err
			}
			thread, replyTo, pub.Thread = root, to, root
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO posts
			(thread, reply_to, harness, session, node, created, title, body, title_fold, body_fold, idem)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			thread, replyTo, who.Harness, who.Session, who.Node, s.stamp(), title, body, post.Fold(title), post.Fold(body), idem)
		if err != nil {
			return err
		}
		if pub.ID, err = res.LastInsertId(); err != nil {
			return err
		}
		if to == 0 {
			pub.Thread = pub.ID
		}
		if err := setTags(ctx, tx, pub.ID, tags); err != nil {
			return err
		}
		if err := setRefs(ctx, tx, pub.ID, body); err != nil {
			return err
		}
		// A new reader starts reading at its first post.
		_, err = tx.ExecContext(ctx, "INSERT INTO readers (reader, seen) VALUES (?, ?) ON CONFLICT DO NOTHING", who.Reader(), pub.ID)
		created = true
		return err
	})
	return pub, created, err
}

// setTags replaces a post's tags.
func setTags(ctx context.Context, q querier, id int64, tags []string) error {
	if _, err := q.ExecContext(ctx, "DELETE FROM tags WHERE post = ?", id); err != nil {
		return err
	}
	for i, tag := range tags {
		if _, err := q.ExecContext(ctx, "INSERT INTO tags (post, pos, tag) VALUES (?, ?, ?)", id, i, tag); err != nil {
			return err
		}
	}
	return nil
}

// setRefs replaces a post's references with those of body to other posts
// that exist.
func setRefs(ctx context.Context, q querier, id int64, body string) error {
	if _, err := q.ExecContext(ctx, "DELETE FROM refs WHERE src = ?", id); err != nil {
		return err
	}
	pos := 0
	for _, dst := range post.Refs(body) {
		if dst == id {
			continue
		}
		res, err := q.ExecContext(ctx, "INSERT INTO refs (src, dst, pos) SELECT ?, id, ? FROM posts WHERE id = ?", id, pos, dst)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			pos++
		}
	}
	return nil
}

// postColumns are the columns that scanPost reads, of posts as p.
const postColumns = `p.id, coalesce(p.thread, p.id), p.reply_to, p.harness, p.session, p.node, p.created,
	p.edited, p.editor_harness, p.editor_session, p.editor_node, p.title, p.body`

type scanner interface{ Scan(dest ...any) error }

func scanPost(row scanner) (post.Post, error) {
	var p post.Post
	var replyTo sql.NullInt64
	var eh, es, en string
	err := row.Scan(&p.ID, &p.Thread, &replyTo, &p.Harness, &p.Session, &p.Node, &p.Created,
		&p.Edited, &eh, &es, &en, &p.Title, &p.Body)
	if err != nil {
		return p, err
	}
	if replyTo.Valid {
		p.ReplyTo = &replyTo.Int64
	}
	p.Author = post.Author(p.Harness, p.Session, p.Node)
	if eh != "" && (eh != p.Harness || es != p.Session) {
		p.Editor = post.Author(eh, es, en)
	}
	return p, nil
}

// fill reads the posts' tags and references.
func fill(ctx context.Context, q querier, posts []post.Post) error {
	for i := range posts {
		p := &posts[i]
		var err error
		if p.Tags, err = column[string](ctx, q, "SELECT tag FROM tags WHERE post = ? ORDER BY pos", p.ID); err != nil {
			return err
		}
		if p.Refs, err = column[int64](ctx, q, "SELECT dst FROM refs WHERE src = ? ORDER BY pos", p.ID); err != nil {
			return err
		}
	}
	return nil
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

// posts returns the posts that a query of postColumns selects, filled.
func posts(ctx context.Context, q querier, query string, args ...any) ([]post.Post, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out := []post.Post{}
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return out, fill(ctx, q, out)
}

func get(ctx context.Context, q querier, id int64) (post.Post, error) {
	ps, err := posts(ctx, q, "SELECT "+postColumns+" FROM posts p WHERE p.id = ?", id)
	if err != nil {
		return post.Post{}, err
	}
	if len(ps) == 0 {
		return post.Post{}, post.NoPost(id)
	}
	return ps[0], nil
}

// Get returns a post.
func (s *Store) Get(ctx context.Context, id int64) (post.Post, error) { return get(ctx, s.db, id) }

// Edit changes a post's fields that e sets.
func (s *Store) Edit(ctx context.Context, who post.Identity, id int64, e post.Edit) (post.Post, error) {
	if e.Empty() {
		return post.Post{}, post.Errorf(post.Invalid, "nothing to change: give a title, tags, or a body")
	}
	var out post.Post
	err := s.tx(ctx, func(tx *sql.Tx) error {
		p, err := get(ctx, tx, id)
		if err != nil {
			return err
		}
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
			edited = ?, editor_harness = ?, editor_session = ?, editor_node = ? WHERE id = ?`,
			title, body, post.Fold(title), post.Fold(body), s.stamp(), who.Harness, who.Session, who.Node, id); err != nil {
			return err
		}
		if !slices.Equal(tags, p.Tags) {
			if err := setTags(ctx, tx, id, tags); err != nil {
				return err
			}
		}
		if body != p.Body {
			if err := setRefs(ctx, tx, id, body); err != nil {
				return err
			}
		}
		out, err = get(ctx, tx, id)
		return err
	})
	return out, err
}

// Delete deletes a post, and a root's whole thread.
func (s *Store) Delete(ctx context.Context, who post.Identity, id int64) (post.Removed, error) {
	var out post.Removed
	err := s.tx(ctx, func(tx *sql.Tx) error {
		p, err := get(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := who.MayChange(p, "delete"); err != nil {
			return err
		}
		out = post.Removed{ID: id, Thread: p.Thread, Removed: []int64{id}}
		if p.IsRoot() {
			if out.Removed, err = column[int64](ctx, tx, "SELECT id FROM posts WHERE id = ? OR thread = ? ORDER BY id", id, id); err != nil {
				return err
			}
		}
		// The foreign keys remove the thread's replies, the posts' tags and
		// references, and the references to them, and clear the replies'
		// reply_to.
		_, err = tx.ExecContext(ctx, "DELETE FROM posts WHERE id = ?", id)
		return err
	})
	return out, err
}

// Show returns the thread that contains id.
func (s *Store) Show(ctx context.Context, id int64) (post.Show, error) {
	var out post.Show
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var root int64
		err := tx.QueryRowContext(ctx, "SELECT coalesce(thread, id) FROM posts WHERE id = ?", id).Scan(&root)
		if errors.Is(err, sql.ErrNoRows) {
			return post.NoPost(id)
		}
		if err != nil {
			return err
		}
		t, err := thread(ctx, tx, root)
		if err != nil {
			return err
		}
		out = post.Show{Focus: id, Thread: t.Thread}
		if out.Posts, err = posts(ctx, tx, "SELECT "+postColumns+" FROM posts p WHERE p.id = ? OR p.thread = ? ORDER BY p.id", root, root); err != nil {
			return err
		}
		out.Backlinks, err = backlinks(ctx, tx, root)
		return err
	})
	return out, err
}

func backlinks(ctx context.Context, q querier, root int64) ([]post.Backlink, error) {
	rows, err := q.QueryContext(ctx, `SELECT r.src, r.dst, coalesce(s.thread, s.id), t.title
		FROM refs r JOIN posts s ON s.id = r.src JOIN posts d ON d.id = r.dst JOIN posts t ON t.id = coalesce(s.thread, s.id)
		WHERE coalesce(d.thread, d.id) = ? AND coalesce(s.thread, s.id) <> ?
		ORDER BY r.src, r.dst`, root, root)
	if err != nil {
		return nil, err
	}
	out := []post.Backlink{}
	for rows.Next() {
		var b post.Backlink
		if err := rows.Scan(&b.ID, &b.To, &b.Thread, &b.Title); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Close()
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
	err := q.QueryRowContext(ctx, `SELECT r.id, r.title, r.harness, r.session, r.node, r.created,
		(SELECT count(*) FROM posts c WHERE c.thread = r.id),
		l.id, l.created
		FROM posts r JOIN posts l ON l.id = (SELECT max(id) FROM posts m WHERE m.id = r.id OR m.thread = r.id)
		WHERE r.id = ? AND r.thread IS NULL`, root).
		Scan(&t.ID, &t.Title, &harness, &session, &node, &t.Created, &t.Replies, &t.last, &t.LastActivity)
	if errors.Is(err, sql.ErrNoRows) {
		return t, post.NoPost(root)
	}
	if err != nil {
		return t, err
	}
	t.Author = post.Author(harness, session, node)
	t.Tags, err = column[string](ctx, q, `SELECT DISTINCT t.tag FROM tags t JOIN posts p ON p.id = t.post
		WHERE p.id = ? OR p.thread = ? ORDER BY t.tag`, root, root)
	return t, err
}

// filter returns the conditions of f: on the author of the posts aliased as
// alias, and on the tags of the thread whose root's id is threadExpr.
func filter(f post.Filter, alias, threadExpr string) (string, []any, error) {
	var where strings.Builder
	var args []any
	for _, tag := range f.Tags {
		t, ok := post.NormalizeTag(tag)
		if !ok {
			return "", nil, post.Errorf(post.Invalid, "invalid tag %q", tag)
		}
		where.WriteString(" AND EXISTS (SELECT 1 FROM tags ft JOIN posts fp ON fp.id = ft.post WHERE (fp.id = " +
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

// List returns threads by last activity, newest first.
func (s *Store) List(ctx context.Context, f post.Filter) ([]post.Thread, error) {
	where, args, err := filter(f, "r", "r.id")
	if err != nil {
		return nil, err
	}
	var out []post.Thread
	err = s.tx(ctx, func(tx *sql.Tx) error {
		ids, err := column[int64](ctx, tx, `SELECT r.id FROM posts r WHERE r.thread IS NULL`+where+`
			ORDER BY (SELECT max(m.id) FROM posts m WHERE m.id = r.id OR m.thread = r.id) DESC, r.id DESC
			LIMIT ?`, append(args, limit(f))...)
		if err != nil {
			return err
		}
		out = make([]post.Thread, 0, len(ids))
		for _, id := range ids {
			t, err := thread(ctx, tx, id)
			if err != nil {
				return err
			}
			out = append(out, t.Thread)
		}
		return nil
	})
	return out, err
}

// Search returns threads with posts that match q: threads whose titles match
// first, then by last activity.
func (s *Store) Search(ctx context.Context, q post.Query) ([]post.Result, error) {
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
	where, args, err := filter(q.Filter, "p", "coalesce(p.thread, p.id)")
	if err != nil {
		return nil, err
	}
	query := "SELECT p.id, coalesce(p.thread, p.id), p.title, p.title_fold, p.body, p.body_fold FROM posts p WHERE 1"
	var termArgs []any
	for _, t := range terms {
		query += " AND instr(" + haystack + ", ?) > 0"
		termArgs = append(termArgs, t)
	}
	query += where + " ORDER BY p.id"
	args = append(termArgs, args...)

	type result struct {
		info    threadInfo
		titled  bool
		matches []post.Match
	}
	var results []*result
	err = s.tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return err
		}
		byThread := map[int64]*result{}
		var order []int64
		for rows.Next() {
			var id, root int64
			var title, titleFold, body, bodyFold string
			if err := rows.Scan(&id, &root, &title, &titleFold, &body, &bodyFold); err != nil {
				_ = rows.Close()
				return err
			}
			r := byThread[root]
			if r == nil {
				r = &result{}
				byThread[root] = r
				order = append(order, root)
			}
			m := post.Match{ID: id, Field: post.FieldBody, Snippet: snippet(body, bodyFold, terms)}
			if q.Field != post.FieldBody && title != "" && containsAll(titleFold, terms) {
				m.Field, m.Snippet = post.FieldTitle, title
				r.titled = true
			}
			r.matches = append(r.matches, m)
		}
		if err := rows.Close(); err != nil {
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

// Tags returns all tags with the number of threads that carry them, most used
// first.
func (s *Store) Tags(ctx context.Context) ([]post.TagCount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.tag, count(DISTINCT coalesce(p.thread, p.id))
		FROM tags t JOIN posts p ON p.id = t.post GROUP BY t.tag ORDER BY 2 DESC, 1`)
	if err != nil {
		return nil, err
	}
	out := []post.TagCount{}
	for rows.Next() {
		var tc post.TagCount
		if err := rows.Scan(&tc.Tag, &tc.Threads); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, tc)
	}
	return out, rows.Close()
}

// Harnesses returns the harnesses that have posted with their numbers of
// posts, most first.
func (s *Store) Harnesses(ctx context.Context) ([]post.HarnessCount, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT harness, count(*) FROM posts GROUP BY harness ORDER BY 2 DESC, 1")
	if err != nil {
		return nil, err
	}
	out := []post.HarnessCount{}
	for rows.Next() {
		var hc post.HarnessCount
		if err := rows.Scan(&hc.Harness, &hc.Posts); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, hc)
	}
	return out, rows.Close()
}

// Unread returns the posts that the identity's reader hasn't seen, and marks
// every post seen unless req.Peek.
func (s *Store) Unread(ctx context.Context, who post.Identity, req post.UnreadRequest) (post.Unread, error) {
	if req.Since < 0 {
		return post.Unread{}, post.Errorf(post.Invalid, "since must be at least 0")
	}
	var out post.Unread
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var seen int64
		err := tx.QueryRowContext(ctx, "SELECT seen FROM readers WHERE reader = ?", who.Reader()).Scan(&seen)
		if errors.Is(err, sql.ErrNoRows) {
			out.NewReader = true
			if req.Since > 0 {
				cutoff := s.now().Add(-time.Duration(req.Since) * time.Second).Unix()
				err = tx.QueryRowContext(ctx, "SELECT coalesce(max(id), 0) FROM posts WHERE CAST(strftime('%s', created) AS INTEGER) < ?", cutoff).Scan(&seen)
			} else {
				err = tx.QueryRowContext(ctx, "SELECT coalesce(max(id), 0) FROM posts").Scan(&seen)
			}
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO readers (reader, seen) VALUES (?, ?)", who.Reader(), seen); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if out.Posts, err = posts(ctx, tx, "SELECT "+postColumns+` FROM posts p
			WHERE p.id > ? AND NOT (p.harness = ? AND p.session = ?) ORDER BY p.id`, seen, who.Harness, who.Session); err != nil {
			return err
		}
		if req.Peek {
			return nil
		}
		_, err = tx.ExecContext(ctx, "UPDATE readers SET seen = max(seen, (SELECT coalesce(max(id), 0) FROM posts)) WHERE reader = ?", who.Reader())
		return err
	})
	return out, err
}
