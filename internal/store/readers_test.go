package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zlcwwhq/agora/internal/post"
	"github.com/zlcwwhq/agora/internal/testdir"
)

// v05Rows fill a database of agora v0.5, of testdata/schema-2.sql: post 2
// was edited by a person, post 3 by its author from another machine, and
// post 4 was deleted.
const v05Rows = `
INSERT INTO boards (id, title, created, harness, session, node, last_n, idem) VALUES
	('b', 'Board B', '2026-11-01T09:00:00+01:00', 'human', 'alice', 'laptop', 4, NULL),
	('empty', 'empty', '2026-11-01T09:00:00+01:00', 'human', 'alice', 'laptop', 0, 'key');
INSERT INTO posts (pk, board, n, thread, reply_to, harness, session, node, created, edited,
	editor_harness, editor_session, editor_node, title, body, title_fold, body_fold, idem) VALUES
	(1, 'b', 1, NULL, NULL, 'codex', '019a2b3c-0000', 'gpu', '2026-11-01T10:00:00+01:00', '', '', '', '',
		'Plan', 'ask @claude-code, not ` + "`@nope`" + `' || char(10), 'plan', 'ask @claude-code, not ` + "`@nope`" + `' || char(10), NULL),
	(2, 'b', 2, 1, 1, 'claude-code', '95d3e5c1-1111', 'laptop', '2026-11-01T10:05:00+01:00', '2026-11-01T11:00:00+01:00',
		'human', 'alice', 'laptop', '', 'see #1, @codex/019a.' || char(10), '', 'see #1, @codex/019a.' || char(10), 'k2'),
	(3, 'b', 3, NULL, NULL, 'claude-code', '95d3e5c1-1111', 'laptop', '2026-11-01T10:10:00+01:00', '2026-11-01T10:20:00+01:00',
		'claude-code', '95d3e5c1-1111', 'desk', 'Other', 'mine' || char(10), 'other', 'mine' || char(10), NULL);
INSERT INTO tags (post, pos, tag) VALUES (1, 0, 'design'), (1, 1, 'mutagen'), (3, 0, 'x');
INSERT INTO refs (src, dst, pos) VALUES (2, 1, 0);
INSERT INTO readers (board, reader, seen) VALUES ('b', 'codex/019a2b3c-0000', 2), ('b', 'human/alice', 4);
`

// v05Database writes a database of agora v0.5 at path, with rows and extra
// statements.
func v05Database(t *testing.T, path string, extra string) {
	t.Helper()
	schema, err := os.ReadFile(filepath.Join("testdata", "schema-2.sql"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	for _, q := range []string{string(schema), v05Rows, extra} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%v in\n%s", err, q)
		}
	}
}

// describe lists a database's tables with their columns, indexes, and
// foreign keys, which an upgraded database shares with a new one, unlike the
// text of their statements.
func describe(t *testing.T, path string) string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var b strings.Builder
	dump := func(query string, args ...any) []string {
		t.Helper()
		rs, err := db.Query(query, args...)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rs.Close() }()
		cols, _ := rs.Columns()
		var firsts []string
		for rs.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rs.Scan(ptrs...); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&b, "  %v\n", vals)
			firsts = append(firsts, fmt.Sprint(vals[0]))
		}
		return firsts
	}
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(&b, "version %d\n", version)
	for _, table := range dump("SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name") {
		fmt.Fprintf(&b, "table %s\n", table)
		dump(`SELECT cid, name, type, "notnull", dflt_value, pk FROM pragma_table_info(?)`, table)
		for _, index := range dump(`SELECT name, "unique", origin, partial FROM pragma_index_list(?) ORDER BY name`, table) {
			dump("SELECT seqno, cid, name FROM pragma_index_info(?)", index)
		}
		dump(`SELECT "table", "from", "to", on_update, on_delete, match FROM pragma_foreign_key_list(?) ORDER BY id, seq`, table)
	}
	return b.String()
}

func TestUpgradeFromV05(t *testing.T) {
	dir := testdir.New(t)
	old, fresh := filepath.Join(dir, "old.db"), filepath.Join(dir, "fresh.db")
	v05Database(t, old, "")
	c := &clock{t: time.Date(2026, 11, 20, 9, 0, 0, 0, time.FixedZone("CET", 3600))}
	for _, path := range []string{old, fresh} {
		s, err := Open(path, c.now)
		if err != nil {
			t.Fatalf("Open(%s): %v", path, err)
		}
		_ = s.Close()
	}
	got, want := describe(t, old), describe(t, fresh)
	if got != want {
		t.Errorf("upgraded schema:\n%s\nnew schema:\n%s", got, want)
	}
	if !strings.Contains(want, "version 4\n") || !strings.Contains(want, "table versions\n") || !strings.Contains(want, "edits_seen") {
		t.Errorf("the description lacks the schema:\n%s", want)
	}

	// The upgraded database opens again, keeping what agora v0.5 wrote.
	s, err := Open(old, c.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	bs := must[[]post.Board](t)(s.Boards(ctx))
	if len(bs) != 2 || bs[0].ID != "b" || bs[0].Posts != 3 || bs[0].Threads != 2 || bs[1].ID != "empty" {
		t.Errorf("boards = %+v", bs)
	}
	show := must[post.Show](t)(s.Show(ctx, "b", 2))
	if len(show.Posts) != 2 || !slices.Equal(show.Thread.Tags, []string{"design", "mutagen"}) || !slices.Equal(show.Posts[1].Refs, []int64{1}) {
		t.Fatalf("show = %+v", show)
	}
	root, rep := show.Posts[0], show.Posts[1]
	if !slices.Equal(root.Mentions, []string{"claude-code"}) || !slices.Equal(rep.Mentions, []string{"codex/019a"}) {
		t.Errorf("mentions = %v, %v", root.Mentions, rep.Mentions)
	}
	if rep.Editor != "human/alice@laptop" || rep.Edited != "2026-11-01T11:00:00+01:00" || rep.Versions != 1 || root.Versions != 1 {
		t.Errorf("edited reply = %+v", rep)
	}
	// Posts that agora v0.5 edited have one version, made at the edit by its
	// editor.
	h := must[post.History](t)(s.History(ctx, "b", 3))
	if len(h.Versions) != 1 || h.Versions[0].Time != "2026-11-01T10:20:00+01:00" || h.Versions[0].Author != "claude-code/95d3e5c1@desk" {
		t.Errorf("history of #3 = %+v", h)
	}

	// Edits before the upgrade count as seen; unread posts and mentions
	// still count.
	codexReader := post.Identity{Harness: "codex", Session: "019a2b3c-0000", Node: "gpu"}
	u := must[post.Unread](t)(s.Unread(ctx, codexReader, "b", post.UnreadRequest{Peek: true}))
	if u.NewReader || ids(u.Posts) != "3" || ids(u.Edited) != "" {
		t.Errorf("codex's unread = %+v", u)
	}
	u = must[post.Unread](t)(s.Unread(ctx, claude, "b", post.UnreadRequest{ToMe: true, Since: 30 * 24 * 3600}))
	if !u.NewReader || ids(u.Posts) != "1" {
		t.Errorf("claude's unread for it = %+v", u)
	}

	// Ids go on from the deleted #4, and edits keep versions from now on.
	if n := newThread(t, s, alice, "After", ""); n != 5 {
		t.Errorf("first post after the upgrade = #%d", n)
	}
	body := "see #1, revised"
	must[post.Post](t)(s.Edit(ctx, alice, "b", 2, post.Edit{Body: &body}))
	h = must[post.History](t)(s.History(ctx, "b", 2))
	if len(h.Versions) != 2 || h.Versions[0].Author != "human/alice@laptop" || h.Versions[0].Time != "2026-11-01T11:00:00+01:00" ||
		h.Versions[0].Body != "see #1, @codex/019a.\n" || h.Versions[1].Body != "see #1, revised\n" {
		t.Errorf("history of #2 = %+v", h)
	}
	if u = must[post.Unread](t)(s.Unread(ctx, codexReader, "b", post.UnreadRequest{})); ids(u.Posts) != "3 5" || ids(u.Edited) != "2" {
		t.Errorf("codex's unread after an edit = %+v", u)
	}
}

func TestFailedUpgradeChangesNothing(t *testing.T) {
	path := filepath.Join(testdir.New(t), "old.db")
	// A table that the upgrade creates already exists, so it fails.
	v05Database(t, path, "CREATE TABLE mentions (x INTEGER)")
	if _, err := Open(path, time.Now); err == nil || !strings.Contains(err.Error(), "upgrading it from schema 2") {
		t.Fatalf("Open = %v", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
		t.Errorf("user_version = %d, %v", version, err)
	}
	if _, err := db.Exec("SELECT last_edit FROM boards"); err == nil {
		t.Error("the failed upgrade added boards.last_edit")
	}
}

// ids lists posts' ids, separated by spaces.
func ids(ps []post.Post) string {
	var out []string
	for _, p := range ps {
		out = append(out, fmt.Sprint(p.ID))
	}
	return strings.Join(out, " ")
}

func TestMentionsRecorded(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	root := newThread(t, s, codex, "T", "ask @claude-code/95d3 and @human/alice. `@nope`")
	r := reply(t, s, claude, root, "@codex: done, cc @claude-code/95d3")
	p := must[post.Post](t)(s.Get(ctx, "b", root))
	if !slices.Equal(p.Mentions, []string{"claude-code/95d3", "human/alice"}) {
		t.Errorf("root's mentions = %v", p.Mentions)
	}
	if p = must[post.Post](t)(s.Get(ctx, "b", r)); !slices.Equal(p.Mentions, []string{"codex", "claude-code/95d3"}) {
		t.Errorf("reply's mentions = %v", p.Mentions)
	}
	body := "now only @antigravity"
	if p = must[post.Post](t)(s.Edit(ctx, claude, "b", r, post.Edit{Body: &body})); !slices.Equal(p.Mentions, []string{"antigravity"}) {
		t.Errorf("mentions after an edit = %v", p.Mentions)
	}
	empty := ""
	if p = must[post.Post](t)(s.Edit(ctx, codex, "b", root, post.Edit{Body: &empty})); p.Mentions == nil || len(p.Mentions) != 0 {
		t.Errorf("mentions of an empty body = %#v", p.Mentions)
	}
}

func TestVersions(t *testing.T) {
	s, c := open(t)
	ctx := context.Background()
	root := newThread(t, s, codex, "Old title", "first body", "a")
	other := newThread(t, s, claude, "Other", "x")
	created := must[post.Post](t)(s.Get(ctx, "b", root)).Created
	if p := must[post.Post](t)(s.Get(ctx, "b", root)); p.Versions != 1 {
		t.Errorf("a new post has %d versions", p.Versions)
	}

	body := "second body"
	codexElsewhere := codex
	codexElsewhere.Node = "desk"
	p1 := must[post.Post](t)(s.Edit(ctx, codexElsewhere, "b", root, post.Edit{Body: &body}))
	title := "New title"
	p2 := must[post.Post](t)(s.Edit(ctx, alice, "b", root, post.Edit{Title: &title, Tags: &[]string{"b"}}))
	// Edits that change nothing make no version.
	must[post.Post](t)(s.Edit(ctx, alice, "b", root, post.Edit{Title: &title, RemoveTags: &[]string{"zzz"}}))
	c.t = c.t.Add(time.Hour)
	must[post.Post](t)(s.Edit(ctx, claude, "b", other, post.Edit{Body: &body}))

	p := must[post.Post](t)(s.Get(ctx, "b", root))
	if p.Versions != 3 || p.Edited != p2.Edited || p.Editor != "human/alice@laptop" {
		t.Errorf("post = %+v", p)
	}
	h := must[post.History](t)(s.History(ctx, "b", root))
	want := []post.Version{
		{Version: 1, Time: created, Author: "codex/019a2b3c@gpu", Title: "Old title", Tags: []string{"a"}, Body: "first body\n"},
		{Version: 2, Time: p1.Edited, Author: "codex/019a2b3c@desk", Title: "Old title", Tags: []string{"a"}, Body: "second body\n"},
		{Version: 3, Time: p2.Edited, Author: "human/alice@laptop", Title: "New title", Tags: []string{"b"}, Body: "second body\n"},
	}
	if h.ID != root || h.Thread != root || len(h.Versions) != len(want) {
		t.Fatalf("history = %+v", h)
	}
	for i, v := range h.Versions {
		if v.Version != want[i].Version || v.Time != want[i].Time || v.Author != want[i].Author || v.Title != want[i].Title ||
			!slices.Equal(v.Tags, want[i].Tags) || v.Body != want[i].Body {
			t.Errorf("version %d = %+v, want %+v", i+1, v, want[i])
		}
	}
	// Each board numbers its edits.
	if n := count(t, s, "SELECT last_edit FROM boards WHERE id = 'b'"); n != 3 {
		t.Errorf("last_edit = %d", n)
	}
	if n := count(t, s, "SELECT edit_n FROM posts WHERE board = 'b' AND n = ?", root); n != 2 {
		t.Errorf("edit_n = %d", n)
	}
	// A version without tags has an empty list.
	r := reply(t, s, claude, other, "a reply")
	must[post.Post](t)(s.Edit(ctx, claude, "b", r, post.Edit{Body: &body}))
	if h := must[post.History](t)(s.History(ctx, "b", r)); len(h.Versions) != 2 || h.Versions[0].Tags == nil || h.Thread != other {
		t.Errorf("history of a reply = %+v", h)
	}
	if _, err := s.History(ctx, "b", 99); post.KindOf(err) != post.NotFound {
		t.Errorf("history of a missing post: %v", err)
	}
}

func TestSingleTags(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	root := newThread(t, s, codex, "T", "body", "a", "b", "c")
	p := must[post.Post](t)(s.Edit(ctx, codex, "b", root, post.Edit{AddTags: &[]string{"D", "a"}, RemoveTags: &[]string{"b", "nope"}}))
	if !slices.Equal(p.Tags, []string{"a", "c", "d"}) || p.Versions != 2 {
		t.Errorf("after changing single tags: %+v", p)
	}
	p = must[post.Post](t)(s.Edit(ctx, codex, "b", root, post.Edit{RemoveTags: &[]string{"nope"}}))
	if p.Versions != 2 {
		t.Errorf("removing a missing tag made a version: %+v", p)
	}
	for _, e := range []post.Edit{
		{Tags: &[]string{"x"}, AddTags: &[]string{"y"}},
		{Tags: &[]string{}, RemoveTags: &[]string{"a"}},
		{AddTags: &[]string{"a b"}},
	} {
		if _, err := s.Edit(ctx, codex, "b", root, e); post.KindOf(err) != post.Invalid {
			t.Errorf("Edit(%+v) = %v, want Invalid", e, err)
		}
	}
	if p := must[post.Post](t)(s.Get(ctx, "b", root)); !slices.Equal(p.Tags, []string{"a", "c", "d"}) {
		t.Errorf("tags after refused edits = %v", p.Tags)
	}
}

func TestReactions(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	root := newThread(t, s, codex, "T", "body")
	r := reply(t, s, claude, root, "a reply")
	must[post.Unread](t)(s.Unread(ctx, alice, "b", post.UnreadRequest{}))
	before := must[post.Show](t)(s.Show(ctx, "b", root))

	react := func(who post.Identity, id int64, reaction string, undo bool) post.Reacted {
		t.Helper()
		return must[post.Reacted](t)(s.React(ctx, who, "b", id, post.React{Reaction: reaction, Undo: undo}))
	}
	react(claude, r, "+1", false)
	react(alice, r, "👀", false)
	react(alice, r, "+1", false)
	react(claude, r, "+1", false) // again: nothing changes
	got := react(codex, r, "ack", false)
	if got.ID != r || got.Thread != root || len(got.Reactions) != 3 {
		t.Fatalf("reacted = %+v", got)
	}
	want := []post.Reaction{
		{Reaction: "+1", Authors: []string{"claude-code/95d3e5c1@laptop", "human/alice@laptop"}},
		{Reaction: "👀", Authors: []string{"human/alice@laptop"}},
		{Reaction: "ack", Authors: []string{"codex/019a2b3c@gpu"}},
	}
	same := func(got, want []post.Reaction) bool {
		return slices.EqualFunc(got, want, func(a, b post.Reaction) bool {
			return a.Reaction == b.Reaction && slices.Equal(a.Authors, b.Authors)
		})
	}
	if !same(got.Reactions, want) {
		t.Errorf("reactions = %+v", got.Reactions)
	}
	// Taking a reaction back that every author took back removes it.
	react(alice, r, "👀", true)
	got = react(codex, r, "never-given", true)
	if !same(got.Reactions, []post.Reaction{want[0], want[2]}) {
		t.Errorf("after undoing = %+v", got.Reactions)
	}
	p := must[post.Post](t)(s.Get(ctx, "b", r))
	if !same(p.Reactions, got.Reactions) || p.Edited != "" || p.Versions != 1 {
		t.Errorf("post = %+v", p)
	}
	if p = must[post.Post](t)(s.Get(ctx, "b", root)); p.Reactions == nil || len(p.Reactions) != 0 {
		t.Errorf("root's reactions = %#v", p.Reactions)
	}
	// Reacting isn't an edit, nor activity.
	after := must[post.Show](t)(s.Show(ctx, "b", root))
	if after.Thread.LastActivity != before.Thread.LastActivity {
		t.Errorf("last activity moved: %v → %v", before.Thread.LastActivity, after.Thread.LastActivity)
	}
	if u := must[post.Unread](t)(s.Unread(ctx, alice, "b", post.UnreadRequest{})); len(u.Posts)+len(u.Edited) != 0 {
		t.Errorf("unread after reactions = %+v", u)
	}

	for _, c := range []struct {
		id       int64
		reaction string
		kind     post.Kind
	}{
		{r, "", post.Invalid},
		{r, "two words", post.Invalid},
		{r, strings.Repeat("x", 33), post.Invalid},
		{99, "ack", post.NotFound},
	} {
		if _, err := s.React(ctx, codex, "b", c.id, post.React{Reaction: c.reaction}); post.KindOf(err) != c.kind {
			t.Errorf("React(%d, %q) = %v, want kind %d", c.id, c.reaction, err, c.kind)
		}
	}
	if _, err := s.React(ctx, codex, "nope", r, post.React{Reaction: "ack"}); post.KindOf(err) != post.NotFound {
		t.Errorf("React on an unknown board: %v", err)
	}
}

func TestUnreadForMeAndEdited(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	unread := func(who post.Identity, req post.UnreadRequest, posts, edited string) {
		t.Helper()
		u := must[post.Unread](t)(s.Unread(ctx, who, "b", req))
		if ids(u.Posts) != posts || ids(u.Edited) != edited || u.Edited == nil {
			t.Errorf("unread(%+v) = posts [%s], edited [%s]; want [%s], [%s]", req, ids(u.Posts), ids(u.Edited), posts, edited)
		}
	}
	edit := func(who post.Identity, id int64, body string) {
		t.Helper()
		must[post.Post](t)(s.Edit(ctx, who, "b", id, post.Edit{Body: &body}))
	}
	a := newThread(t, s, codex, "A", "about A")                 // 1
	b := newThread(t, s, codex, "B", "about B")                 // 2
	unread(claude, post.UnreadRequest{}, "", "")                // claude starts reading
	mine := reply(t, s, claude, b, "my answer")                 // 3
	reply(t, s, codex, b, "to claude")                          // 4
	reply(t, s, codex, a, "ping @claude-code/95d3")             // 5
	reply(t, s, codex, a, "unrelated")                          // 6
	newThread(t, s, alice, "C", "hi @codex and @claude-code/x") // 7
	edit(codex, b, "about B, revised")
	edit(alice, mine, "my answer, fixed by alice")

	toMe := post.UnreadRequest{ToMe: true}
	peek := func(r post.UnreadRequest) post.UnreadRequest { r.Peek = true; return r }
	// For claude: the posts of the thread where it posted, those that
	// mention it, and its own post that someone else edited.
	unread(claude, peek(toMe), "4 5", "2 3")
	unread(claude, peek(post.UnreadRequest{Thread: 6}), "5 6", "")
	unread(claude, peek(post.UnreadRequest{ToMe: true, Thread: 1}), "5", "")
	unread(claude, toMe, "4 5", "2 3")
	unread(claude, toMe, "", "")
	// What --to-me showed is seen; the rest isn't.
	unread(claude, peek(post.UnreadRequest{}), "6 7", "")

	// A seen post edited again, twice, is reported once.
	edit(codex, 4, "to claude, again")
	edit(codex, 4, "to claude, once more")
	unread(claude, peek(toMe), "", "4")
	unread(claude, post.UnreadRequest{Thread: a}, "6", "")
	unread(claude, peek(post.UnreadRequest{}), "7", "4")
	// Without --to-me and --thread, everything becomes seen.
	unread(claude, post.UnreadRequest{}, "7", "4")
	unread(claude, post.UnreadRequest{}, "", "")
	if n := count(t, s, "SELECT count(*) FROM seen WHERE reader = ?", claude.Reader()); n != 0 {
		t.Errorf("claude keeps %d seen marks", n)
	}
	if n := count(t, s, `SELECT count(*) FROM readers r JOIN boards b ON b.id = r.board
		WHERE r.reader = ? AND r.seen = b.last_n AND r.edits_seen = b.last_edit`, claude.Reader()); n != 1 {
		t.Errorf("claude's cursor isn't at the board's newest post and edit")
	}
	// Its own edits aren't news to it.
	edit(claude, mine, "my answer, mine again")
	unread(claude, post.UnreadRequest{}, "", "")

	// Readers that start now, by reading or by posting, have seen every
	// edit until then.
	newcomer := post.Identity{Harness: "codex", Session: "newcomer", Node: "gpu"}
	unread(newcomer, peek(post.UnreadRequest{}), "", "")
	poster := post.Identity{Harness: "antigravity", Session: "poster", Node: "gpu"}
	reply(t, s, poster, a, "hello")
	unread(poster, peek(post.UnreadRequest{}), "", "")
	edit(codex, a, "about A, revised")
	unread(poster, peek(post.UnreadRequest{}), "", "1")
	unread(newcomer, peek(toMe), "", "")

	if _, err := s.Unread(ctx, claude, "b", post.UnreadRequest{Thread: 99}); post.KindOf(err) != post.NotFound {
		t.Errorf("unread of a missing thread: %v", err)
	}
	if _, err := s.Unread(ctx, claude, "b", post.UnreadRequest{Thread: -1}); post.KindOf(err) != post.Invalid {
		t.Errorf("unread of thread -1: %v", err)
	}
}

func TestDeleteRemovesWhatReadersSee(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	root := newThread(t, s, codex, "T", "for @claude-code")
	must[post.Unread](t)(s.Unread(ctx, claude, "b", post.UnreadRequest{}))
	r := reply(t, s, codex, root, "again @claude-code")
	r2 := reply(t, s, alice, r, "and @codex")
	body := "edited @claude-code"
	for _, id := range []int64{root, r, r2} {
		who := codex
		if id == r2 {
			who = alice
		}
		must[post.Post](t)(s.Edit(ctx, who, "b", id, post.Edit{Body: &body}))
		must[post.Reacted](t)(s.React(ctx, claude, "b", id, post.React{Reaction: "ack"}))
	}
	// Marks for the posts that --to-me showed.
	must[post.Unread](t)(s.Unread(ctx, claude, "b", post.UnreadRequest{ToMe: true}))

	left := func(ids ...int64) map[string]int {
		t.Helper()
		out := map[string]int{}
		for _, table := range []string{"versions", "mentions", "reactions", "seen"} {
			q := "SELECT count(*) FROM " + table + " WHERE post IN (SELECT pk FROM posts WHERE board = 'b' AND n IN (" +
				strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", ") + "))"
			args := make([]any, len(ids))
			for i, id := range ids {
				args[i] = id
			}
			out[table] = count(t, s, q, args...)
		}
		return out
	}
	before := left(r)
	for table, n := range before {
		if n == 0 {
			t.Fatalf("no %s for #%d to delete: %v", table, r, before)
		}
	}
	pks := func(ids ...int64) []int64 {
		var out []int64
		for _, id := range ids {
			out = append(out, int64(count(t, s, "SELECT pk FROM posts WHERE board = 'b' AND n = ?", id)))
		}
		return out
	}
	gone := func(pks []int64) {
		t.Helper()
		for _, table := range []string{"versions", "mentions", "reactions", "seen"} {
			for _, pk := range pks {
				if n := count(t, s, "SELECT count(*) FROM "+table+" WHERE post = ?", pk); n != 0 {
					t.Errorf("%d rows of %s left for a deleted post", n, table)
				}
			}
		}
	}
	rPK := pks(r)
	must[post.Removed](t)(s.Delete(ctx, codex, "b", r))
	gone(rPK)
	if after := left(root, r2); after["versions"] != 2 || after["reactions"] != 2 {
		t.Errorf("deleting #%d removed others' rows: %v", r, after)
	}
	rest := pks(root, r2)
	must[post.Removed](t)(s.Delete(ctx, codex, "b", root))
	gone(rest)
}
