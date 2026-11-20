package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zlcwwhq/agora/internal/post"
)

var (
	alice  = post.Identity{Harness: "human", Session: "alice", Node: "laptop"}
	codex  = post.Identity{Harness: "codex", Session: "019a2b3c-0000", Node: "gpu"}
	claude = post.Identity{Harness: "claude-code", Session: "95d3e5c1-1111", Node: "laptop"}
)

// clock is a fake clock that advances a minute each time it is read.
type clock struct{ t time.Time }

func (c *clock) now() time.Time {
	c.t = c.t.Add(time.Minute)
	return c.t
}

func open(t *testing.T) (*Store, *clock) {
	t.Helper()
	c := &clock{t: time.Date(2026, 11, 20, 9, 0, 0, 0, time.FixedZone("CET", 3600))}
	s, err := Open(filepath.Join(t.TempDir(), "board.db"), c.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if _, err := s.CreateBoard(context.Background(), alice, post.NewBoard{ID: "b"}); err != nil {
		t.Fatal(err)
	}
	return s, c
}

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

func newThread(t *testing.T, s *Store, who post.Identity, title, body string, tags ...string) int64 {
	t.Helper()
	pub, created, err := s.Post(context.Background(), who, "b", post.NewThread{Title: title, Body: body, Tags: tags})
	if err != nil || !created {
		t.Fatalf("Post: %v, created %v", err, created)
	}
	return pub.ID
}

func reply(t *testing.T, s *Store, who post.Identity, to int64, body string, tags ...string) int64 {
	t.Helper()
	pub, created, err := s.Reply(context.Background(), who, "b", to, post.NewReply{Body: body, Tags: tags})
	if err != nil || !created {
		t.Fatalf("Reply: %v, created %v", err, created)
	}
	return pub.ID
}

func count(t *testing.T, s *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestThreadsAreFlat(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	root := newThread(t, s, codex, "Star edges", "Proposal", "Mutagen")
	r1 := reply(t, s, claude, root, "Agreed", "design")
	r2 := reply(t, s, alice, r1, "Me too")

	show := must[post.Show](t)(s.Show(ctx, "b", r2))
	if show.Focus != r2 || show.Thread.ID != root || show.Thread.Replies != 2 {
		t.Fatalf("thread = %+v", show.Thread)
	}
	if !slices.Equal(show.Thread.Tags, []string{"design", "mutagen"}) {
		t.Errorf("thread tags = %v", show.Thread.Tags)
	}
	if show.Thread.LastActivity != show.Posts[2].Created || show.Thread.Author != "codex/019a2b3c@gpu" {
		t.Errorf("thread = %+v", show.Thread)
	}
	var ids []int64
	for _, p := range show.Posts {
		ids = append(ids, p.ID)
		if p.Thread != root {
			t.Errorf("post %d has thread %d", p.ID, p.Thread)
		}
	}
	if !slices.Equal(ids, []int64{root, r1, r2}) {
		t.Errorf("posts = %v", ids)
	}
	if show.Posts[0].ReplyTo != nil || *show.Posts[1].ReplyTo != root || *show.Posts[2].ReplyTo != r1 {
		t.Errorf("reply_to wrong: %+v", show.Posts)
	}
	if show.Posts[0].Body != "Proposal\n" || show.Posts[0].Title != "Star edges" || show.Posts[1].Title != "" {
		t.Errorf("root = %+v", show.Posts[0])
	}

	if _, _, err := s.Reply(ctx, claude, "b", 99, post.NewReply{Body: "x"}); post.KindOf(err) != post.NotFound {
		t.Errorf("reply to a missing post: %v", err)
	}
	if _, _, err := s.Reply(ctx, claude, "b", root, post.NewReply{Body: " \n"}); post.KindOf(err) != post.Invalid {
		t.Errorf("reply without a body: %v", err)
	}
	if _, _, err := s.Post(ctx, claude, "b", post.NewThread{Title: "", Body: "x"}); post.KindOf(err) != post.Invalid {
		t.Errorf("thread without a title: %v", err)
	}
}

func TestRefsCountOnlyExistingPosts(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	a := newThread(t, s, codex, "A", "first")
	b := newThread(t, s, codex, "B", "second")
	c := newThread(t, s, claude, "C", "See #2, #1, #2 again, PR #123, and myself #3. `#1`")
	p := must[post.Post](t)(s.Get(ctx, "b", c))
	if !slices.Equal(p.Refs, []int64{b, a}) {
		t.Fatalf("refs = %v, want [%d %d]", p.Refs, b, a)
	}
	show := must[post.Show](t)(s.Show(ctx, "b", a))
	if len(show.Backlinks) != 1 || show.Backlinks[0] != (post.Backlink{ID: c, To: a, Thread: c, Title: "C"}) {
		t.Errorf("backlinks = %+v", show.Backlinks)
	}

	// Editing recomputes them.
	body := "Only #1 now"
	p = must[post.Post](t)(s.Edit(ctx, claude, "b", c, post.Edit{Body: &body}))
	if !slices.Equal(p.Refs, []int64{a}) {
		t.Errorf("refs after edit = %v", p.Refs)
	}
	// Deleting the target removes them.
	must[post.Removed](t)(s.Delete(ctx, codex, "b", a))
	p = must[post.Post](t)(s.Get(ctx, "b", c))
	if len(p.Refs) != 0 {
		t.Errorf("refs after deleting #1 = %v", p.Refs)
	}
}

func TestDeleteRootRemovesThread(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	other := newThread(t, s, alice, "Other", "body")
	root := newThread(t, s, codex, "Doomed", "body", "x")
	r1 := reply(t, s, claude, root, "reply with #1", "y")
	r2 := reply(t, s, alice, r1, "reply to the reply")
	reply(t, s, alice, other, "links to #"+strconv.FormatInt(r1, 10))

	if _, err := s.Delete(ctx, claude, "b", root); post.KindOf(err) != post.Forbidden {
		t.Fatalf("another author's delete: %v", err)
	}
	removed := must[post.Removed](t)(s.Delete(ctx, codex, "b", root))
	if removed.ID != root || removed.Thread != root || !slices.Equal(removed.Removed, []int64{root, r1, r2}) {
		t.Fatalf("removed = %+v", removed)
	}
	for _, q := range []string{
		"SELECT count(*) FROM posts WHERE board = 'b' AND n IN (?, ?, ?)",
		"SELECT count(*) FROM tags WHERE post IN (SELECT pk FROM posts WHERE board = 'b' AND n IN (?, ?, ?))",
		"SELECT count(*) FROM refs x JOIN posts p ON p.pk IN (x.src, x.dst) WHERE p.board = 'b' AND (p.n IN (?, ?, ?) OR p.n IN (?, ?, ?))",
	} {
		args := []any{root, r1, r2}
		if strings.Count(q, "?") == 6 {
			args = append(args, args...)
		}
		if n := count(t, s, q, args...); n != 0 {
			t.Errorf("%s: %d rows left", q, n)
		}
	}
	if _, err := s.Show(ctx, "b", r2); post.KindOf(err) != post.NotFound {
		t.Errorf("show of a deleted reply: %v", err)
	}
	if _, err := s.Delete(ctx, codex, "b", root); post.KindOf(err) != post.NotFound {
		t.Errorf("deleting again: %v", err)
	}
	if ts := must[[]post.Thread](t)(s.List(ctx, "b", post.Filter{})); len(ts) != 1 || ts[0].ID != other {
		t.Errorf("list = %+v", ts)
	}
}

func TestDeleteReplyKeepsItsReplies(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	root := newThread(t, s, codex, "T", "")
	r1 := reply(t, s, claude, root, "one")
	r2 := reply(t, s, codex, r1, "two")

	removed := must[post.Removed](t)(s.Delete(ctx, alice, "b", r1)) // a person may delete anything
	if !slices.Equal(removed.Removed, []int64{r1}) || removed.Thread != root {
		t.Fatalf("removed = %+v", removed)
	}
	show := must[post.Show](t)(s.Show(ctx, "b", root))
	if len(show.Posts) != 2 || show.Posts[1].ID != r2 || show.Posts[1].ReplyTo != nil || show.Thread.Replies != 1 {
		t.Errorf("after deleting the reply: %+v", show)
	}
	// Ids are never given again.
	if r3 := reply(t, s, codex, root, "three"); r3 <= r2 {
		t.Errorf("new id %d after %d", r3, r2)
	}
}

func TestEdit(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	root := newThread(t, s, codex, "Old title", "old body", "a", "b")
	r := reply(t, s, claude, root, "a reply")
	before := must[post.Show](t)(s.Show(ctx, "b", root))

	title, body := "New title", "new body"
	p := must[post.Post](t)(s.Edit(ctx, codex, "b", root, post.Edit{Title: &title, Body: &body}))
	if p.Title != title || p.Body != "new body\n" || !slices.Equal(p.Tags, []string{"a", "b"}) || p.Edited == "" || p.Editor != "" {
		t.Errorf("after the author's edit: %+v", p)
	}
	none := []string{}
	p = must[post.Post](t)(s.Edit(ctx, alice, "b", root, post.Edit{Tags: &none}))
	if len(p.Tags) != 0 || p.Editor != "human/alice@laptop" {
		t.Errorf("after a person's edit: %+v", p)
	}
	after := must[post.Show](t)(s.Show(ctx, "b", root))
	if after.Thread.LastActivity != before.Thread.LastActivity || after.Thread.Title != title || len(after.Thread.Tags) != 0 {
		t.Errorf("thread after edits = %+v", after.Thread)
	}

	// An edit that changes nothing records nothing.
	same := "a reply"
	p = must[post.Post](t)(s.Edit(ctx, claude, "b", r, post.Edit{Body: &same}))
	if p.Edited != "" {
		t.Errorf("unchanged edit recorded %q", p.Edited)
	}

	empty, other := "", "x"
	for _, c := range []struct {
		who  post.Identity
		id   int64
		e    post.Edit
		kind post.Kind
	}{
		{codex, r, post.Edit{Body: &other}, post.Forbidden},
		{claude, r, post.Edit{Title: &other}, post.Invalid},
		{claude, r, post.Edit{Body: &empty}, post.Invalid},
		{codex, root, post.Edit{Title: &empty}, post.Invalid},
		{codex, root, post.Edit{}, post.Invalid},
		{codex, 99, post.Edit{Body: &other}, post.NotFound},
	} {
		if _, err := s.Edit(ctx, c.who, "b", c.id, c.e); post.KindOf(err) != c.kind {
			t.Errorf("Edit(%+v, %d): %v, want kind %d", c.who, c.id, err, c.kind)
		}
	}
	// A root's body may become empty.
	if p := must[post.Post](t)(s.Edit(ctx, codex, "b", root, post.Edit{Body: &empty})); p.Body != "" {
		t.Errorf("root body = %q", p.Body)
	}
}

func TestNonce(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	req := post.NewThread{Title: "T", Body: "b", Nonce: "n1"}
	first, created, err := s.Post(ctx, alice, "b", req)
	if err != nil || !created {
		t.Fatal(err)
	}
	again, created, err := s.Post(ctx, alice, "b", req)
	if err != nil || created || again != first {
		t.Errorf("repeat = %+v, created %v, %v", again, created, err)
	}
	req.Body = "changed"
	if _, created, _ := s.Post(ctx, alice, "b", req); !created {
		t.Error("a changed request with the same nonce published nothing")
	}
	if _, created, _ := s.Reply(ctx, alice, "b", first.ID, post.NewReply{Body: "b", Nonce: "n1"}); !created {
		t.Error("a reply with a thread's nonce published nothing")
	}
	if _, _, err := s.Post(ctx, alice, "b", post.NewThread{Title: "T", Nonce: "bad nonce"}); post.KindOf(err) != post.Invalid {
		t.Errorf("invalid nonce: %v", err)
	}
}

func TestUnread(t *testing.T) {
	s, c := open(t)
	ctx := context.Background()
	old := newThread(t, s, codex, "Old", "x")
	c.t = c.t.Add(3 * time.Hour)
	recent := reply(t, s, codex, old, "recent")

	// A new reader with a lookback sees what was created within it.
	u := must[post.Unread](t)(s.Unread(ctx, claude, "b", post.UnreadRequest{Since: 3600}))
	if !u.NewReader || len(u.Posts) != 1 || u.Posts[0].ID != recent {
		t.Fatalf("first unread = %+v", u)
	}
	if u = must[post.Unread](t)(s.Unread(ctx, claude, "b", post.UnreadRequest{})); u.NewReader || len(u.Posts) != 0 {
		t.Fatalf("second unread = %+v", u)
	}

	// Own posts are seen; others' are unread; edits aren't.
	mine := reply(t, s, claude, old, "mine")
	theirs := reply(t, s, codex, mine, "theirs")
	body := "edited"
	must[post.Post](t)(s.Edit(ctx, codex, "b", old, post.Edit{Body: &body}))
	u = must[post.Unread](t)(s.Unread(ctx, claude, "b", post.UnreadRequest{Peek: true}))
	if len(u.Posts) != 1 || u.Posts[0].ID != theirs {
		t.Fatalf("peek = %+v", u)
	}
	if u = must[post.Unread](t)(s.Unread(ctx, claude, "b", post.UnreadRequest{})); len(u.Posts) != 1 {
		t.Fatalf("after peek = %+v", u)
	}
	if u = must[post.Unread](t)(s.Unread(ctx, claude, "b", post.UnreadRequest{})); len(u.Posts) != 0 {
		t.Fatalf("after reading = %+v", u)
	}

	// A new reader without a lookback has seen everything; one that posts
	// first starts at its post.
	if u = must[post.Unread](t)(s.Unread(ctx, alice, "b", post.UnreadRequest{})); !u.NewReader || len(u.Posts) != 0 {
		t.Errorf("new reader = %+v", u)
	}
	poster := post.Identity{Harness: "codex", Session: "other", Node: "gpu"}
	reply(t, s, poster, old, "first words")
	later := reply(t, s, codex, old, "an answer")
	if u = must[post.Unread](t)(s.Unread(ctx, poster, "b", post.UnreadRequest{Since: 999999})); u.NewReader || len(u.Posts) != 1 || u.Posts[0].ID != later {
		t.Errorf("reader who posted first = %+v", u)
	}
}

func TestListSearchTags(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	a := newThread(t, s, codex, "Mutagen conflicts", "about stars", "mutagen")
	b := newThread(t, s, claude, "Web pages", "The STAR of the show", "web")
	reply(t, s, alice, a, "conflicts again", "design")
	c := newThread(t, s, claude, "冲突处理", "中文正文", "web")

	ids := func(ts []post.Thread) []int64 {
		var out []int64
		for _, t := range ts {
			out = append(out, t.ID)
		}
		return out
	}
	if got := ids(must[[]post.Thread](t)(s.List(ctx, "b", post.Filter{}))); !slices.Equal(got, []int64{c, a, b}) {
		t.Errorf("list = %v", got)
	}
	if got := ids(must[[]post.Thread](t)(s.List(ctx, "b", post.Filter{Tags: []string{"Web"}}))); !slices.Equal(got, []int64{c, b}) {
		t.Errorf("list -t web = %v", got)
	}
	if got := ids(must[[]post.Thread](t)(s.List(ctx, "b", post.Filter{Tags: []string{"mutagen", "design"}}))); !slices.Equal(got, []int64{a}) {
		t.Errorf("list -t mutagen -t design = %v", got)
	}
	if got := ids(must[[]post.Thread](t)(s.List(ctx, "b", post.Filter{Author: "claude-code/95d3"}))); !slices.Equal(got, []int64{c, b}) {
		t.Errorf("list --author = %v", got)
	}
	if got := ids(must[[]post.Thread](t)(s.List(ctx, "b", post.Filter{Limit: 1}))); !slices.Equal(got, []int64{c}) {
		t.Errorf("list -n 1 = %v", got)
	}
	if _, err := s.List(ctx, "b", post.Filter{Author: "Bad"}); post.KindOf(err) != post.Invalid {
		t.Errorf("invalid author: %v", err)
	}

	rs := must[[]post.Result](t)(s.Search(ctx, "b", post.Query{Terms: []string{"star"}}))
	if len(rs) != 2 || rs[0].ID != a || rs[1].ID != b || rs[1].Matches[0].Field != post.FieldBody || rs[1].Matches[0].Snippet != "The STAR of the show" {
		t.Errorf("search star = %+v", rs)
	}
	rs = must[[]post.Result](t)(s.Search(ctx, "b", post.Query{Terms: []string{"conflicts"}}))
	if len(rs) != 1 || len(rs[0].Matches) != 2 || rs[0].Matches[0].Field != post.FieldTitle {
		t.Errorf("search conflicts = %+v", rs)
	}
	rs = must[[]post.Result](t)(s.Search(ctx, "b", post.Query{Terms: []string{"conflicts"}, Field: post.FieldBody, Filter: post.Filter{Author: "human"}}))
	if len(rs) != 1 || len(rs[0].Matches) != 1 || rs[0].Matches[0].Field != post.FieldBody {
		t.Errorf("search --body --author human = %+v", rs)
	}
	if rs = must[[]post.Result](t)(s.Search(ctx, "b", post.Query{Terms: []string{"冲突"}})); len(rs) != 1 || rs[0].ID != c {
		t.Errorf("search 冲突 = %+v", rs)
	}
	if _, err := s.Search(ctx, "b", post.Query{Terms: []string{" "}}); post.KindOf(err) != post.Invalid {
		t.Errorf("search without terms: %v", err)
	}

	tags := must[[]post.TagCount](t)(s.Tags(ctx, "b"))
	want := []post.TagCount{{Tag: "web", Threads: 2}, {Tag: "design", Threads: 1}, {Tag: "mutagen", Threads: 1}}
	if !slices.Equal(tags, want) {
		t.Errorf("tags = %v", tags)
	}
	hs := must[[]post.HarnessCount](t)(s.Harnesses(ctx, "b"))
	if !slices.Equal(hs, []post.HarnessCount{{Harness: "claude-code", Posts: 2}, {Harness: "codex", Posts: 1}, {Harness: "human", Posts: 1}}) {
		t.Errorf("harnesses = %v", hs)
	}
}

func TestOpenLocksAndChecksVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.db")
	s, err := Open(path, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, time.Now); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Errorf("second Open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path, time.Now)
	if err != nil {
		t.Fatalf("Open after Close: %v", err)
	}
	_ = s.Close()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if _, err := Open(path, time.Now); err == nil || !strings.Contains(err.Error(), "later version") {
		t.Errorf("Open of a later schema: %v", err)
	}
}

func TestBoards(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	other := must[post.Board](t)(s.CreateBoard(ctx, codex, post.NewBoard{ID: "other", Title: " Other board "}))
	if other.ID != "other" || other.Title != "Other board" || other.Creator != "codex/019a2b3c@gpu" || other.Threads != 0 || other.LastActivity != "" {
		t.Errorf("created = %+v", other)
	}
	for _, c := range []struct {
		req  post.NewBoard
		kind post.Kind
	}{
		{post.NewBoard{ID: "other"}, post.Conflict},
		{post.NewBoard{ID: "Bad"}, post.Invalid},
		{post.NewBoard{ID: "x", Title: "two\nlines"}, post.Invalid},
	} {
		if _, err := s.CreateBoard(ctx, alice, c.req); post.KindOf(err) != c.kind {
			t.Errorf("CreateBoard(%+v) = %v, want kind %d", c.req, err, c.kind)
		}
	}

	// Each board numbers its posts from 1, and references stay within one.
	b1 := newThread(t, s, codex, "In b", "first in b")
	pub, _, err := s.Post(ctx, claude, "other", post.NewThread{Title: "In other", Body: "see #1 and #2"})
	if err != nil || pub.ID != 1 || b1 != 1 {
		t.Fatalf("first posts: b %d, other %+v, %v", b1, pub, err)
	}
	p := must[post.Post](t)(s.Get(ctx, "other", 1))
	if p.Board != "other" || p.Title != "In other" || len(p.Refs) != 0 {
		t.Errorf("other's #1 = %+v", p)
	}
	newThread(t, s, codex, "Second in b", "")
	body := "now #1"
	if p = must[post.Post](t)(s.Edit(ctx, claude, "other", 1, post.Edit{Body: &body})); len(p.Refs) != 0 {
		t.Errorf("a post refers to itself: %+v", p.Refs)
	}
	pub, _, _ = s.Post(ctx, claude, "other", post.NewThread{Title: "Second in other", Body: "see #1"})
	if p = must[post.Post](t)(s.Get(ctx, "other", pub.ID)); !slices.Equal(p.Refs, []int64{1}) {
		t.Errorf("refs = %v", p.Refs)
	}
	if show := must[post.Show](t)(s.Show(ctx, "other", 1)); len(show.Backlinks) != 1 || show.Backlinks[0].ID != pub.ID || show.Thread.Board != "other" {
		t.Errorf("show = %+v", show)
	}
	if show := must[post.Show](t)(s.Show(ctx, "b", 1)); len(show.Backlinks) != 0 {
		t.Errorf("b's #1 has backlinks from another board: %+v", show.Backlinks)
	}

	// Lists, tags, and cursors are the board's own.
	if ts := must[[]post.Thread](t)(s.List(ctx, "other", post.Filter{})); len(ts) != 2 || ts[0].Board != "other" {
		t.Errorf("list of other = %+v", ts)
	}
	if u := must[post.Unread](t)(s.Unread(ctx, alice, "other", post.UnreadRequest{Since: 999999})); !u.NewReader || len(u.Posts) != 2 {
		t.Errorf("unread of other = %+v", u)
	}
	if u := must[post.Unread](t)(s.Unread(ctx, alice, "b", post.UnreadRequest{})); !u.NewReader {
		t.Errorf("alice isn't new on b: %+v", u)
	}

	bs := must[[]post.Board](t)(s.Boards(ctx))
	if len(bs) != 2 || bs[0].ID != "other" || bs[0].Threads != 2 || bs[0].Posts != 2 || bs[1].ID != "b" || bs[1].Posts != 2 {
		t.Errorf("boards = %+v", bs)
	}
	empty := must[post.Board](t)(s.CreateBoard(ctx, alice, post.NewBoard{ID: "a-empty"}))
	if bs = must[[]post.Board](t)(s.Boards(ctx)); len(bs) != 3 || bs[2].ID != empty.ID || empty.Title != "a-empty" {
		t.Errorf("boards with an empty one = %+v", bs)
	}

	// Unknown boards are errors everywhere.
	for name, err := range map[string]error{
		"Post":   func() error { _, _, err := s.Post(ctx, codex, "nope", post.NewThread{Title: "T"}); return err }(),
		"Show":   func() error { _, err := s.Show(ctx, "nope", 1); return err }(),
		"List":   func() error { _, err := s.List(ctx, "nope", post.Filter{}); return err }(),
		"Tags":   func() error { _, err := s.Tags(ctx, "nope"); return err }(),
		"Unread": func() error { _, err := s.Unread(ctx, codex, "nope", post.UnreadRequest{}); return err }(),
		"Board":  func() error { _, err := s.Board(ctx, "nope"); return err }(),
	} {
		if post.KindOf(err) != post.NotFound || !strings.Contains(err.Error(), `no board "nope"`) {
			t.Errorf("%s on an unknown board: %v", name, err)
		}
	}
	if _, err := s.Show(ctx, "b", 99); err == nil || !strings.Contains(err.Error(), "no post #99 on board b") {
		t.Errorf("Show of a missing post: %v", err)
	}
}

func TestRefusesDevelopmentSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "board.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE posts (id INTEGER PRIMARY KEY); PRAGMA user_version = 1"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	if _, err := Open(path, time.Now); err == nil || !strings.Contains(err.Error(), "development build") {
		t.Errorf("Open of a version 1 database: %v", err)
	}
}

func TestNewestIDNotReused(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	newThread(t, s, codex, "One", "")
	two := newThread(t, s, codex, "Two", "")
	if _, err := s.Delete(ctx, codex, "b", two); err != nil {
		t.Fatal(err)
	}
	if three := newThread(t, s, codex, "Three", ""); three != two+1 {
		t.Errorf("after deleting #%d, the next post got #%d", two, three)
	}
	if b := must[post.Board](t)(s.Board(ctx, "b")); b.Posts != 2 {
		t.Errorf("board = %+v", b)
	}
}
