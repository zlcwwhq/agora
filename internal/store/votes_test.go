package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zlcwwhq/agora/internal/post"
	"github.com/zlcwwhq/agora/internal/testdir"
)

func TestVotes(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	root := newThread(t, s, codex, "T", "body")
	r := reply(t, s, claude, root, "a reply")
	must[post.Unread](t)(s.Unread(ctx, alice, "b", post.UnreadRequest{}))
	before := must[post.Show](t)(s.Show(ctx, "b", root))
	if p := before.Posts[1]; p.Score != 0 || p.Votes.Up == nil || p.Votes.Down == nil || len(p.Votes.Up)+len(p.Votes.Down) != 0 {
		t.Fatalf("votes without any = %d, %#v", p.Score, p.Votes)
	}

	vote := func(who post.Identity, id int64, v string) post.Voted {
		t.Helper()
		return must[post.Voted](t)(s.Vote(ctx, who, "b", id, post.Vote{Vote: v}))
	}
	same := func(got post.Voted, score int, up, down []string) bool {
		return got.Score == score && slices.Equal(got.Votes.Up, up) && slices.Equal(got.Votes.Down, down)
	}
	const (
		cl = "claude-code/95d3e5c1@laptop"
		al = "human/alice@laptop"
		cx = "codex/019a2b3c@gpu"
	)
	vote(alice, r, post.VoteUp)
	vote(codex, r, post.VoteDown)
	// Its own post, too, and again: nothing changes.
	vote(claude, r, post.VoteUp)
	got := vote(alice, r, post.VoteUp)
	if got.ID != r || got.Thread != root || !same(got, 1, []string{al, cl}, []string{cx}) {
		t.Fatalf("voted = %+v", got)
	}
	// A changed vote follows those given that way before.
	got = vote(codex, r, post.VoteUp)
	if !same(got, 3, []string{al, cl, cx}, []string{}) {
		t.Errorf("after codex changes its vote = %+v", got)
	}
	got = vote(alice, r, post.VoteDown)
	if !same(got, 1, []string{cl, cx}, []string{al}) {
		t.Errorf("after alice changes her vote = %+v", got)
	}
	// Taking a vote back, also one never given.
	vote(claude, r, post.VoteNone)
	got = vote(claude, r, post.VoteNone)
	if !same(got, 0, []string{cx}, []string{al}) {
		t.Errorf("after taking a vote back = %+v", got)
	}

	// Votes aren't reactions, and voting isn't an edit, nor activity.
	p := must[post.Post](t)(s.Get(ctx, "b", r))
	if p.Score != 0 || !slices.Equal(p.Votes.Up, []string{cx}) || len(p.Reactions) != 0 || p.Edited != "" || p.Versions != 1 {
		t.Errorf("post = %+v", p)
	}
	after := must[post.Show](t)(s.Show(ctx, "b", root))
	if after.Thread.LastActivity != before.Thread.LastActivity {
		t.Errorf("last activity moved: %v → %v", before.Thread.LastActivity, after.Thread.LastActivity)
	}
	if u := must[post.Unread](t)(s.Unread(ctx, alice, "b", post.UnreadRequest{})); len(u.Posts)+len(u.Edited) != 0 {
		t.Errorf("unread after votes = %+v", u)
	}

	// A thread's score is its root's, in every list of threads.
	vote(alice, root, post.VoteUp)
	vote(claude, root, post.VoteUp)
	vote(codex, root, post.VoteDown)
	if th := must[post.Show](t)(s.Show(ctx, "b", r)).Thread; th.Score != 1 {
		t.Errorf("shown thread's score = %d, want 1", th.Score)
	}
	if ts := must[[]post.Thread](t)(s.List(ctx, "b", post.Filter{})); len(ts) != 1 || ts[0].Score != 1 {
		t.Errorf("listed threads = %+v", ts)
	}
	if rs := must[[]post.Result](t)(s.Search(ctx, "b", post.Query{Terms: []string{"reply"}})); len(rs) != 1 || rs[0].Score != 1 {
		t.Errorf("found threads = %+v", rs)
	}

	// Anyone may vote in a locked thread.
	yes := true
	must[post.Thread](t)(s.ChangeThread(ctx, alice, "b", root, post.ThreadChange{Locked: &yes}))
	if got := vote(codex, root, post.VoteUp); got.Score != 3 {
		t.Errorf("vote in a locked thread = %+v", got)
	}

	for _, c := range []struct {
		id   int64
		vote string
		kind post.Kind
	}{
		{r, "", post.Invalid},
		{r, "Up", post.Invalid},
		{r, "+1", post.Invalid},
		{99, post.VoteUp, post.NotFound},
	} {
		if _, err := s.Vote(ctx, codex, "b", c.id, post.Vote{Vote: c.vote}); post.KindOf(err) != c.kind {
			t.Errorf("Vote(%d, %q) = %v, want kind %d", c.id, c.vote, err, c.kind)
		}
	}
	if _, err := s.Vote(ctx, codex, "nope", r, post.Vote{Vote: post.VoteUp}); post.KindOf(err) != post.NotFound {
		t.Errorf("Vote on an unknown board: %v", err)
	}

	// Deleting a post removes its votes.
	must[post.Removed](t)(s.Delete(ctx, alice, "b", root))
	if n := count(t, s, "SELECT count(*) FROM votes"); n != 0 {
		t.Errorf("%d votes left after deleting the thread", n)
	}
}

func TestVotesOfNames(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	root := newThread(t, s, alice, "Work", "for @claude-code/e2e")
	claimAs(t, s, ctx, sessA, "e2e", false)
	got := must[post.Voted](t)(s.Vote(ctx, sessA, "b", root, post.Vote{Vote: post.VoteUp}))
	if !slices.Equal(got.Votes.Up, []string{"claude-code/e2e@laptop"}) {
		t.Errorf("the carrier's vote = %+v", got)
	}
	// Another session may not vote as the name that sessA carries.
	if _, err := s.Vote(via(sessB.Session, ""), e2e, "b", root, post.Vote{Vote: post.VoteDown}); post.KindOf(err) != post.Forbidden {
		t.Errorf("Vote as a name that another session carries = %v", err)
	}
	// What the name gave is what its carrier gave.
	g := must[map[int64]post.Given](t)(s.Given(ctx, sessA, "b", root))
	if g[root].Vote != post.VoteUp {
		t.Errorf("Given(carrier) = %+v", g)
	}
}

func TestGiven(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	root := newThread(t, s, codex, "T", "body")
	r := reply(t, s, claude, root, "a reply")
	r2 := reply(t, s, codex, root, "another")
	other := newThread(t, s, codex, "Other", "body")
	for _, x := range []struct {
		who      post.Identity
		id       int64
		reaction string
	}{
		{claude, r, "hooray"},
		{alice, r, "ack"},
		{alice, r, "🎉"},
		{codex, r, "laugh"},
		{alice, r, "👀"}, // ack is eyes: nothing changes
		{alice, r, "laugh"},
		{alice, other, "ack"},
	} {
		must[post.Reacted](t)(s.React(ctx, x.who, "b", x.id, post.React{Reaction: x.reaction}))
	}
	must[post.Voted](t)(s.Vote(ctx, alice, "b", root, post.Vote{Vote: post.VoteDown}))
	// From another machine, the same person.
	elsewhere := post.Identity{Harness: alice.Harness, Session: alice.Session, Node: "desk"}
	must[post.Voted](t)(s.Vote(ctx, elsewhere, "b", r, post.Vote{Vote: post.VoteUp}))
	must[post.Voted](t)(s.Vote(ctx, claude, "b", r2, post.Vote{Vote: post.VoteUp}))

	got := must[map[int64]post.Given](t)(s.Given(ctx, alice, "b", r2))
	want := map[int64]post.Given{
		root: {Vote: post.VoteDown},
		// By name, in the order in which alice gave them.
		r: {Vote: post.VoteUp, Reactions: []string{"eyes", "hooray", "laugh"}},
	}
	if len(got) != len(want) {
		t.Fatalf("Given = %+v, want %+v", got, want)
	}
	for id, w := range want {
		if g := got[id]; g.Vote != w.Vote || !slices.Equal(g.Reactions, w.Reactions) {
			t.Errorf("Given[%d] = %+v, want %+v", id, g, w)
		}
	}
	if _, err := s.Given(ctx, alice, "b", 99); post.KindOf(err) != post.NotFound {
		t.Errorf("Given of a post that doesn't exist = %v", err)
	}
}

// v07Rows fill a database of agora v0.7.2, of testdata/schema-5.sql: a
// thread with reactions as any text: GitHub's names and emoji, ❤ without its
// variation selector, ack, agora-skill's dispatcher's dispatched, pairs that
// become one reaction of one author, and text that is none of GitHub's.
const v07Rows = `
INSERT INTO boards (id, title, created, harness, session, node, last_n, idem, last_edit) VALUES
	('b', 'Board B', '2026-11-20T09:00:00+01:00', 'human', 'alice', 'laptop', 2, NULL, 0);
INSERT INTO posts (pk, board, n, thread, reply_to, harness, session, node, created, title, body, title_fold, body_fold, carrier, model) VALUES
	(1, 'b', 1, NULL, NULL, 'codex', 'ops', 'gpu', '2026-11-20T10:00:00+01:00',
		'Plan', 'a plan' || char(10), 'plan', 'a plan' || char(10), '019a2b3c-0000', 'gpt-5.5-codex'),
	(2, 'b', 2, 1, 1, 'claude-code', '95d3e5c1-1111', 'laptop', '2026-11-20T10:05:00+01:00',
		'', 'done' || char(10), '', 'done' || char(10), '', 'claude-opus-5-5');
INSERT INTO reactions (post, reaction, harness, session, node, created) VALUES
	(1, '🚀', 'claude-code', '95d3e5c1-1111', 'laptop', '2026-11-20T10:06:00+01:00'),
	(1, '+1', 'codex', '019a2b3c-0000', 'gpu', '2026-11-20T10:06:01+01:00'),
	(1, 'ack', 'human', 'alice', 'laptop', '2026-11-20T10:06:02+01:00'),
	(1, '👀', 'human', 'alice', 'laptop', '2026-11-20T10:06:03+01:00'),
	(1, char(10084, 65039), 'claude-code', '95d3e5c1-1111', 'laptop', '2026-11-20T10:06:04+01:00'),
	(1, 'dispatched', 'claude-code', '95d3e5c1-1111', 'laptop', '2026-11-20T10:06:04+01:00'),
	(2, char(10084), 'codex', '019a2b3c-0000', 'gpu', '2026-11-20T10:06:05+01:00'),
	(2, 'lgtm', 'human', 'alice', 'laptop', '2026-11-20T10:06:06+01:00'),
	(2, 'eyes', 'claude-code', '95d3e5c1-1111', 'laptop', '2026-11-20T10:06:07+01:00'),
	(2, '👀', 'claude-code', '95d3e5c1-1111', 'laptop', '2026-11-20T10:06:08+01:00'),
	(2, '✅', 'codex', '019a2b3c-0000', 'gpu', '2026-11-20T10:06:09+01:00'),
	(2, 'dispatched', 'codex', '019a2b3c-0000', 'gpu', '2026-11-20T10:06:10+01:00');
`

func TestUpgradeFromV07(t *testing.T) {
	dir := testdir.New(t)
	old, fresh := filepath.Join(dir, "old.db"), filepath.Join(dir, "fresh.db")
	schema, err := os.ReadFile(filepath.Join("testdata", "schema-5.sql"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", old)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{string(schema), v07Rows} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%v in\n%s", err, q)
		}
	}
	_ = db.Close()
	c := &clock{t: time.Date(2026, 11, 25, 9, 0, 0, 0, time.FixedZone("CET", 3600))}
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
	if !strings.Contains(got, "version 6\n") || !strings.Contains(got, "table votes\n") {
		t.Errorf("the upgraded database lacks votes:\n%s", got)
	}

	// The upgraded database keeps what agora v0.7.2 wrote, without votes,
	// and takes them.
	s, err := Open(old, c.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	show := must[post.Show](t)(s.Show(ctx, "b", 1))
	if len(show.Posts) != 2 || show.Posts[0].Carrier != "codex/019a2b3c@gpu" || show.Posts[0].Score != 0 || show.Thread.Score != 0 {
		t.Errorf("show = %+v", show)
	}
	// Reactions become GitHub's, by name, dispatched rocket, and the others
	// go.
	const (
		cl = "claude-code/95d3e5c1@laptop"
		cx = "codex/019a2b3c@gpu"
		al = "human/alice@laptop"
	)
	for i, want := range [][]post.Reaction{
		{{Reaction: "rocket", Authors: []string{cl}}, {Reaction: "+1", Authors: []string{cx}},
			{Reaction: "eyes", Authors: []string{al}}, {Reaction: "heart", Authors: []string{cl}}},
		{{Reaction: "heart", Authors: []string{cx}}, {Reaction: "eyes", Authors: []string{cl}},
			{Reaction: "rocket", Authors: []string{cx}}},
	} {
		if got := show.Posts[i].Reactions; !slices.EqualFunc(got, want, func(a, b post.Reaction) bool {
			return a.Reaction == b.Reaction && slices.Equal(a.Authors, b.Authors)
		}) {
			t.Errorf("post %d's reactions = %+v, want %+v", i+1, got, want)
		}
	}
	if v := must[post.Voted](t)(s.Vote(ctx, alice, "b", 2, post.Vote{Vote: post.VoteDown})); v.Score != -1 {
		t.Errorf("Vote after the upgrade = %+v", v)
	}
}
