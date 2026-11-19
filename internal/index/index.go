// Package index keeps a board's local SQLite index, which holds only what can
// be derived from the post files, and answers queries from it. Its schema is
// migrations/index.sql.
package index

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	mrand "math/rand/v2"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/zlcwwhq/agora/internal/post"
	"github.com/zlcwwhq/agora/migrations"

	"modernc.org/sqlite" // also registers the sqlite driver
	sqlite3 "modernc.org/sqlite/lib"
)

// DB is an open index.
type DB struct {
	db *sql.DB
}

// Open opens the index at path, creating it if needed, and deleting and
// recreating it if it has another schema version.
func Open(path string) (*DB, error) {
	d, current, err := open(path)
	if err != nil {
		return nil, err
	}
	if current {
		return d, nil
	}
	if err := d.Close(); err != nil {
		return nil, err
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	d, current, err = open(path)
	if err != nil {
		return nil, err
	}
	if !current {
		_ = d.Close()
		return nil, fmt.Errorf("index %s: can't create the current schema", path)
	}
	return d, nil
}

// openTimeout bounds how long open retries while other processes hold locks
// that SQLite doesn't wait for, as when they switch a new database to WAL.
const openTimeout = 15 * time.Second

// open opens the database and creates the schema if the database is empty,
// reporting whether it then has the current schema version. It retries while
// the database is busy.
func open(path string) (*DB, bool, error) {
	deadline := time.Now().Add(openTimeout)
	for {
		d, current, err := openOnce(path)
		if err == nil || !busy(err) || time.Now().After(deadline) {
			return d, current, err
		}
		time.Sleep(time.Duration(10+mrand.IntN(40)) * time.Millisecond)
	}
}

// busy reports whether err means that another connection holds a lock.
func busy(err error) bool {
	var e *sqlite.Error
	if !errors.As(err, &e) {
		return false
	}
	code := e.Code() & 0xff
	return code == sqlite3.SQLITE_BUSY || code == sqlite3.SQLITE_LOCKED
}

func openOnce(path string) (*DB, bool, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(wal)&_txlock=immediate")
	if err != nil {
		return nil, false, err
	}
	db.SetMaxOpenConns(1)
	d := &DB{db: db}
	current, err := d.ensureSchema()
	if err != nil {
		_ = db.Close()
		return nil, false, fmt.Errorf("index %s: %w", path, err)
	}
	return d, current, nil
}

func (d *DB) ensureSchema() (bool, error) {
	// The transaction is immediate, so concurrent processes create the schema
	// one after another, and the later one sees it.
	tx, err := d.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var version, objects int
	if err := tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return false, err
	}
	if err := tx.QueryRow("SELECT count(*) FROM sqlite_master").Scan(&objects); err != nil {
		return false, err
	}
	if objects > 0 {
		return version == migrations.IndexVersion, nil
	}
	if _, err := tx.Exec(migrations.Index); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// Close closes the index.
func (d *DB) Close() error { return d.db.Close() }

// File is a post file on the board.
type File struct {
	Path    string
	ID      post.ID
	Size    int64
	MtimeNs int64
}

// Warning reports a file that Sync skipped.
type Warning struct {
	Path string
	Err  error
}

func (w Warning) String() string { return fmt.Sprintf("skipping %s: %v", w.Path, w.Err) }

// Sync brings the index up to date with files, the board's complete list of
// post files, reading the files that are new or changed with read. It returns
// warnings for files that aren't valid posts, which it records so as not to
// warn again until they change.
func (d *DB) Sync(files []File, read func(path string) ([]byte, error)) ([]Warning, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	type known struct {
		size, mtime int64
		postID      sql.NullString
	}
	existing := map[string]known{}
	rows, err := tx.Query("SELECT path, size, mtime_ns, post_id FROM files")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var path string
		var k known
		if err := rows.Scan(&path, &k.size, &k.mtime, &k.postID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		existing[path] = k
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	var warnings []Warning
	for _, f := range files {
		k, ok := existing[f.Path]
		delete(existing, f.Path)
		if ok && k.size == f.Size && k.mtime == f.MtimeNs {
			continue
		}
		if err := deletePost(tx, f.ID.String()); err != nil {
			return nil, err
		}
		data, err := read(f.Path)
		if errors.Is(err, fs.ErrNotExist) {
			if _, err := tx.Exec("DELETE FROM files WHERE path = ?", f.Path); err != nil {
				return nil, err
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		var postID sql.NullString
		p, err := post.Parse(data)
		if err == nil && p.ID != f.ID {
			err = fmt.Errorf("its id %s disagrees with its path", p.ID)
		}
		if err != nil {
			warnings = append(warnings, Warning{Path: f.Path, Err: err})
		} else {
			if err := insertPost(tx, p); err != nil {
				return nil, err
			}
			postID = sql.NullString{String: p.ID.String(), Valid: true}
		}
		if _, err := tx.Exec(
			"INSERT OR REPLACE INTO files (path, size, mtime_ns, post_id) VALUES (?, ?, ?, ?)",
			f.Path, f.Size, f.MtimeNs, postID,
		); err != nil {
			return nil, err
		}
	}
	for path, k := range existing {
		if k.postID.Valid {
			if err := deletePost(tx, k.postID.String); err != nil {
				return nil, err
			}
		}
		if _, err := tx.Exec("DELETE FROM files WHERE path = ?", path); err != nil {
			return nil, err
		}
	}
	return warnings, tx.Commit()
}

func deletePost(tx *sql.Tx, id string) error {
	for _, q := range []string{
		"DELETE FROM posts WHERE id = ?",
		"DELETE FROM tags WHERE post_id = ?",
		"DELETE FROM refs WHERE src = ?",
	} {
		if _, err := tx.Exec(q, id); err != nil {
			return err
		}
	}
	return nil
}

func insertPost(tx *sql.Tx, p post.Post) error {
	created, err := p.CreatedTime()
	if err != nil {
		return err
	}
	id := p.ID.String()
	if _, err := tx.Exec(`INSERT INTO posts
		(id, node, n, thread, reply_to, harness, session, created, created_unix, title, body, title_fold, body_fold, deleted)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, p.ID.Node, p.ID.N, p.ThreadID().String(), p.ReplyTo.String(), p.Harness, p.Session,
		p.Created, created.Unix(), p.Title, p.Body, post.Fold(p.Title), post.Fold(p.Body), p.Deleted,
	); err != nil {
		return err
	}
	for _, tag := range p.Tags {
		if _, err := tx.Exec("INSERT OR IGNORE INTO tags (post_id, tag) VALUES (?, ?)", id, tag); err != nil {
			return err
		}
	}
	for _, ref := range post.Refs(p.Body, p.ID) {
		if _, err := tx.Exec("INSERT OR IGNORE INTO refs (src, dst) VALUES (?, ?)", id, ref.String()); err != nil {
			return err
		}
	}
	return nil
}

// Entry is an indexed post with the references in its body.
type Entry struct {
	post.Post
	Refs []post.ID
}

const entryColumns = "id, thread, reply_to, harness, session, created, title, body, deleted"

// displayOrder orders posts of the table aliased p for display.
const displayOrder = "p.created_unix, p.node, p.n"

func (d *DB) entries(query string, args ...any) ([]Entry, error) {
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for rows.Next() {
		var p post.Post
		var id, thread, replyTo string
		if err := rows.Scan(&id, &thread, &replyTo, &p.Harness, &p.Session, &p.Created, &p.Title, &p.Body, &p.Deleted); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if p.ID, err = post.ParseID(id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if replyTo != "" {
			if p.Thread, err = post.ParseID(thread); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if p.ReplyTo, err = post.ParseID(replyTo); err != nil {
				_ = rows.Close()
				return nil, err
			}
		}
		out = append(out, Entry{Post: p, Refs: post.Refs(p.Body, p.ID)})
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Tags, err = d.strings("SELECT tag FROM tags WHERE post_id = ? ORDER BY tag", out[i].ID.String()); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (d *DB) strings(query string, args ...any) ([]string, error) {
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Close()
}

// Get returns the post with id, and whether it is in the index.
func (d *DB) Get(id post.ID) (Entry, bool, error) {
	es, err := d.entries("SELECT "+entryColumns+" FROM posts WHERE id = ?", id.String())
	if err != nil || len(es) == 0 {
		return Entry{}, false, err
	}
	return es[0], true, nil
}

// ThreadPosts returns the posts of the thread whose root is root, in display
// order.
func (d *DB) ThreadPosts(root post.ID) ([]Entry, error) {
	return d.entries("SELECT "+entryColumns+" FROM posts p WHERE thread = ? ORDER BY "+displayOrder, root.String())
}

// Thread summarizes a thread.
type Thread struct {
	ID post.ID
	// Title, Harness, Session, Created, and Deleted are the root's, and
	// empty if the root hasn't arrived. Title is empty if the root is
	// deleted.
	Title, Harness, Session, Created, Deleted string
	// LastActivity is the creation time of the thread's latest post that
	// isn't deleted.
	LastActivity string
	lastUnix     int64
	// Replies counts the replies that aren't deleted.
	Replies int
	// Tags is the union of the thread's posts' tags, sorted.
	Tags []string
}

// Filter restricts the threads or posts that queries return.
type Filter struct {
	// Tags are required of the thread, all of them.
	Tags []string
	// Harness and SessionPrefix select an author; empty selects any.
	Harness, SessionPrefix string
}

func (f Filter) where(table string) (string, []any) {
	var clauses []string
	var args []any
	for _, tag := range f.Tags {
		clauses = append(clauses, table+".thread IN (SELECT tp.thread FROM posts tp JOIN tags t ON t.post_id = tp.id WHERE t.tag = ?)")
		args = append(args, tag)
	}
	if f.Harness != "" {
		clauses = append(clauses, table+".harness = ?")
		args = append(args, f.Harness)
	}
	if f.SessionPrefix != "" {
		clauses = append(clauses, "substr("+table+".session, 1, ?) = ?")
		args = append(args, len(f.SessionPrefix), f.SessionPrefix)
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " AND " + strings.Join(clauses, " AND "), args
}

// threadQuery selects thread summaries of roots aliased r. Deleted posts
// count neither as activity nor as replies.
const threadQuery = `SELECT r.id, r.title, r.harness, r.session, r.created,
	COALESCE((SELECT p.created FROM posts p WHERE p.thread = r.id AND p.deleted = ''
		ORDER BY p.created_unix DESC, p.node DESC, p.n DESC LIMIT 1), ''),
	COALESCE((SELECT max(p.created_unix) FROM posts p WHERE p.thread = r.id AND p.deleted = ''), 0),
	(SELECT count(*) FROM posts p WHERE p.thread = r.id AND p.reply_to != '' AND p.deleted = ''),
	r.deleted
	FROM posts r WHERE r.reply_to = ''`

// live restricts threads of roots aliased r to those with a post that isn't
// deleted.
const live = " AND EXISTS (SELECT 1 FROM posts lp WHERE lp.thread = r.id AND lp.deleted = '')"

func (d *DB) threads(query string, args ...any) ([]Thread, error) {
	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	var out []Thread
	for rows.Next() {
		var t Thread
		var id string
		if err := rows.Scan(&id, &t.Title, &t.Harness, &t.Session, &t.Created, &t.LastActivity, &t.lastUnix, &t.Replies, &t.Deleted); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if t.ID, err = post.ParseID(id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, t)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		if err := d.threadTags(&out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (d *DB) threadTags(t *Thread) error {
	var err error
	t.Tags, err = d.strings(
		"SELECT DISTINCT t.tag FROM tags t JOIN posts p ON p.id = t.post_id WHERE p.thread = ? ORDER BY t.tag",
		t.ID.String())
	return err
}

// ThreadInfo summarizes the thread whose root is root, which needn't have
// arrived yet.
func (d *DB) ThreadInfo(root post.ID) (Thread, error) {
	ts, err := d.threads(threadQuery+" AND r.id = ?", root.String())
	if err != nil {
		return Thread{}, err
	}
	if len(ts) > 0 {
		return ts[0], nil
	}
	t := Thread{ID: root}
	var last sql.NullString
	var lastUnix sql.NullInt64
	err = d.db.QueryRow(`SELECT
		(SELECT p.created FROM posts p WHERE p.thread = ?1 AND p.deleted = '' ORDER BY p.created_unix DESC, p.node DESC, p.n DESC LIMIT 1),
		(SELECT max(p.created_unix) FROM posts p WHERE p.thread = ?1 AND p.deleted = ''),
		(SELECT count(*) FROM posts p WHERE p.thread = ?1 AND p.deleted = '')`, root.String()).Scan(&last, &lastUnix, &t.Replies)
	if err != nil {
		return Thread{}, err
	}
	t.LastActivity, t.lastUnix = last.String, lastUnix.Int64
	return t, d.threadTags(&t)
}

// List returns at most limit threads that match f, by most recent activity,
// leaving out those whose every post is deleted. The author filter applies to
// the root.
func (d *DB) List(f Filter, limit int) ([]Thread, error) {
	where, args := f.where("r")
	args = append(args, limit)
	return d.threads(threadQuery+live+where+" ORDER BY 7 DESC, r.node, r.n LIMIT ?", args...)
}

// Fields that Search matches.
const (
	FieldAny   = ""
	FieldTitle = "title"
	FieldBody  = "body"
)

// Query is a search.
type Query struct {
	// Terms must all occur in one post. They are folded before matching.
	Terms []string
	// Field restricts matching to the title or the body.
	Field string
	// Filter's author applies to the matching post.
	Filter Filter
	Limit  int
}

// Match is a post that matched a search.
type Match struct {
	ID post.ID
	// Field is FieldTitle if the title contains every term, else FieldBody.
	Field   string
	Snippet string
}

// Result is a thread with posts that matched a search.
type Result struct {
	Thread
	Matches []Match
}

// Search returns at most q.Limit threads with posts that match q: threads
// whose titles match first, then by most recent activity.
func (d *DB) Search(q Query) ([]Result, error) {
	var terms []string
	for _, t := range q.Terms {
		if t = post.Fold(strings.TrimSpace(t)); t != "" {
			terms = append(terms, t)
		}
	}
	if len(terms) == 0 {
		return nil, errors.New("no search terms")
	}
	haystack := map[string]string{
		FieldAny:   "p.title_fold || char(10) || p.body_fold",
		FieldTitle: "p.title_fold",
		FieldBody:  "p.body_fold",
	}[q.Field]
	if haystack == "" {
		return nil, fmt.Errorf("unknown field %q", q.Field)
	}
	query := "SELECT p.id, p.thread, p.title, p.title_fold, p.body, p.body_fold FROM posts p WHERE 1"
	var args []any
	for _, t := range terms {
		query += " AND instr(" + haystack + ", ?) > 0"
		args = append(args, t)
	}
	where, filterArgs := q.Filter.where("p")
	query += where + " ORDER BY " + displayOrder
	args = append(args, filterArgs...)

	rows, err := d.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	byThread := map[string]*Result{}
	var order []string
	for rows.Next() {
		var id, thread, title, titleFold, body, bodyFold string
		if err := rows.Scan(&id, &thread, &title, &titleFold, &body, &bodyFold); err != nil {
			_ = rows.Close()
			return nil, err
		}
		m := Match{Field: FieldBody, Snippet: snippet(body, bodyFold, terms)}
		if m.ID, err = post.ParseID(id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if q.Field != FieldBody && containsAll(titleFold, terms) {
			m.Field, m.Snippet = FieldTitle, title
		}
		r := byThread[thread]
		if r == nil {
			r = &Result{}
			byThread[thread] = r
			order = append(order, thread)
		}
		r.Matches = append(r.Matches, m)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	results := make([]Result, 0, len(order))
	for _, thread := range order {
		root, err := post.ParseID(thread)
		if err != nil {
			return nil, err
		}
		r := byThread[thread]
		if r.Thread, err = d.ThreadInfo(root); err != nil {
			return nil, err
		}
		results = append(results, *r)
	}
	titled := func(r Result) bool {
		return slices.ContainsFunc(r.Matches, func(m Match) bool { return m.Field == FieldTitle })
	}
	slices.SortStableFunc(results, func(a, b Result) int {
		if ta, tb := titled(a), titled(b); ta != tb {
			if ta {
				return -1
			}
			return 1
		}
		if a.lastUnix != b.lastUnix {
			if a.lastUnix > b.lastUnix {
				return -1
			}
			return 1
		}
		if a.ID.Less(b.ID) {
			return -1
		}
		if b.ID.Less(a.ID) {
			return 1
		}
		return 0
	})
	if q.Limit > 0 && len(results) > q.Limit {
		results = results[:q.Limit]
	}
	return results, nil
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

// TagCount is a tag and the number of threads that carry it.
type TagCount struct {
	Tag     string
	Threads int
}

// Tags returns all tags, most used first.
func (d *DB) Tags() ([]TagCount, error) {
	rows, err := d.db.Query(`SELECT t.tag, count(DISTINCT p.thread) FROM tags t JOIN posts p ON p.id = t.post_id
		GROUP BY t.tag ORDER BY 2 DESC, 1`)
	if err != nil {
		return nil, err
	}
	out := []TagCount{}
	for rows.Next() {
		var tc TagCount
		if err := rows.Scan(&tc.Tag, &tc.Threads); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, tc)
	}
	return out, rows.Close()
}

// Backlink is a reference from another thread's post to a post of a thread.
type Backlink struct {
	// ID is the referencing post, To the referenced one.
	ID, To post.ID
	// Thread and Title identify the referencing post's thread; Title is
	// empty if its root hasn't arrived or is deleted, as RootDeleted says.
	Thread      post.ID
	Title       string
	RootDeleted bool
}

// Backlinks returns references from other threads to the posts of the thread
// whose root is root.
func (d *DB) Backlinks(root post.ID) ([]Backlink, error) {
	rows, err := d.db.Query(`SELECT p.id, r.dst, p.thread, COALESCE(rt.title, ''), COALESCE(rt.deleted, '') != ''
		FROM refs r
		JOIN posts dst ON dst.id = r.dst
		JOIN posts p ON p.id = r.src
		LEFT JOIN posts rt ON rt.id = p.thread
		WHERE dst.thread = ?1 AND p.thread != ?1
		ORDER BY `+displayOrder+`, dst.node, dst.n`, root.String())
	if err != nil {
		return nil, err
	}
	out := []Backlink{}
	for rows.Next() {
		var src, dst, thread string
		var b Backlink
		if err := rows.Scan(&src, &dst, &thread, &b.Title, &b.RootDeleted); err != nil {
			_ = rows.Close()
			return nil, err
		}
		for _, x := range []struct {
			s  string
			id *post.ID
		}{{src, &b.ID}, {dst, &b.To}, {thread, &b.Thread}} {
			if *x.id, err = post.ParseID(x.s); err != nil {
				_ = rows.Close()
				return nil, err
			}
		}
		out = append(out, b)
	}
	return out, rows.Close()
}

// Unresolved returns the ids that the thread's posts reference but the index
// doesn't have, ordered by id.
func (d *DB) Unresolved(root post.ID) ([]post.ID, error) {
	ss, err := d.strings(`SELECT DISTINCT r.dst FROM refs r JOIN posts p ON p.id = r.src
		WHERE p.thread = ? AND r.dst NOT IN (SELECT id FROM posts)`, root.String())
	if err != nil {
		return nil, err
	}
	out := []post.ID{}
	for _, s := range ss {
		id, err := post.ParseID(s)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	slices.SortFunc(out, func(a, b post.ID) int {
		if a.Less(b) {
			return -1
		}
		if b.Less(a) {
			return 1
		}
		return 0
	})
	return out, nil
}

// Item is what unread tracking needs of a post.
type Item struct {
	ID               post.ID
	Harness, Session string
	CreatedUnix      int64
}

// Items returns every post, in display order.
func (d *DB) Items() ([]Item, error) {
	rows, err := d.db.Query("SELECT p.id, p.harness, p.session, p.created_unix FROM posts p ORDER BY " + displayOrder)
	if err != nil {
		return nil, err
	}
	var out []Item
	for rows.Next() {
		var it Item
		var id string
		if err := rows.Scan(&id, &it.Harness, &it.Session, &it.CreatedUnix); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if it.ID, err = post.ParseID(id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Close()
}
