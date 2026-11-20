package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zlcwwhq/agora/internal/post"
	"github.com/zlcwwhq/agora/internal/store"
)

var claude = post.Identity{Harness: "claude-code", Session: "95d3e5c1-1111", Node: "laptop"}

// site is a server served with its pages.
type site struct {
	t     *testing.T
	base  string
	store *store.Store
	http  *http.Client
}

// serve serves a server of its own, with the board b, titled "Board B",
// and forms that act as person unless it is zero.
func serve(t *testing.T, person post.Identity) *site {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "board.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.CreateBoard(context.Background(), alice, post.NewBoard{ID: "b", Title: "Board B"}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(Options{Store: st, Person: person, Version: "v2.0.0-test", Logf: t.Logf}))
	t.Cleanup(srv.Close)
	return &site{t: t, base: srv.URL, store: st, http: &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// response is what a request got.
type response struct {
	status int
	header http.Header
	body   string
}

func (s *site) do(req *http.Request) response {
	s.t.Helper()
	resp, err := s.http.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		s.t.Fatal(err)
	}
	return response{status: resp.StatusCode, header: resp.Header, body: string(data)}
}

func (s *site) get(path string) response {
	s.t.Helper()
	req, err := http.NewRequest(http.MethodGet, s.base+path, nil)
	if err != nil {
		s.t.Fatal(err)
	}
	return s.do(req)
}

// send sends a form as a browser does, from origin unless it is empty.
func (s *site) send(method, path, origin string, fields url.Values) response {
	s.t.Helper()
	req, err := http.NewRequest(method, s.base+path, strings.NewReader(fields.Encode()))
	if err != nil {
		s.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	return s.do(req)
}

// post sends a form from the pages' own origin.
func (s *site) post(path string, fields url.Values) response {
	s.t.Helper()
	return s.send(http.MethodPost, path, s.base, fields)
}

// thread starts a thread on board b.
func (s *site) thread(who post.Identity, title, body string, tags ...string) int64 {
	s.t.Helper()
	return s.threadOn("b", who, title, body, tags...)
}

func (s *site) threadOn(board string, who post.Identity, title, body string, tags ...string) int64 {
	s.t.Helper()
	pub, _, err := s.store.Post(context.Background(), who, board, post.NewThread{Title: title, Body: body, Tags: tags})
	if err != nil {
		s.t.Fatal(err)
	}
	return pub.ID
}

// reply replies on board b.
func (s *site) reply(who post.Identity, to int64, body string) int64 {
	s.t.Helper()
	pub, _, err := s.store.Reply(context.Background(), who, "b", to, post.NewReply{Body: body})
	if err != nil {
		s.t.Fatal(err)
	}
	return pub.ID
}

// board creates a board.
func (s *site) board(id, title string) {
	s.t.Helper()
	if _, err := s.store.CreateBoard(context.Background(), codex, post.NewBoard{ID: id, Title: title}); err != nil {
		s.t.Fatal(err)
	}
}

// threads lists board b's threads.
func (s *site) threads() []post.Thread {
	s.t.Helper()
	return s.threadsOn("b")
}

func (s *site) threadsOn(board string) []post.Thread {
	s.t.Helper()
	ts, err := s.store.List(context.Background(), board, post.Filter{Limit: 100})
	if err != nil {
		s.t.Fatal(err)
	}
	return ts
}

func (s *site) boards() []post.Board {
	s.t.Helper()
	bs, err := s.store.Boards(context.Background())
	if err != nil {
		s.t.Fatal(err)
	}
	return bs
}

// location checks that r leads to location with status.
func location(t *testing.T, r response, status int, location string) {
	t.Helper()
	if r.status != status || r.header.Get("Location") != location {
		t.Errorf("got %d %q, want %d %q; body:\n%.500s", r.status, r.header.Get("Location"), status, location, r.body)
	}
}

func expect(t *testing.T, r response, status int, contains ...string) {
	t.Helper()
	if r.status != status {
		t.Errorf("status %d, want %d; body:\n%.600s", r.status, status, r.body)
	}
	for _, c := range contains {
		if !strings.Contains(r.body, c) {
			t.Errorf("page lacks %q:\n%.1500s", c, r.body)
		}
	}
	if strings.Contains(strings.ToLower(r.body), "<script") {
		t.Errorf("page has a script")
	}
}

func lacks(t *testing.T, r response, absent ...string) {
	t.Helper()
	for _, a := range absent {
		if strings.Contains(r.body, a) {
			t.Errorf("page has %q", a)
		}
	}
}

func TestBoardListsThreadsAndFilters(t *testing.T) {
	s := serve(t, post.Identity{})
	s.thread(codex, "Mutagen conflicts", "about stars", "mutagen")
	s.thread(claude, "Web pages", "The STAR of the show", "web")
	s.thread(alice, "冲突处理", "中文正文", "web")

	r := s.get("/b/b/")
	expect(t, r, 200, "<h1>Board B</h1>", "Mutagen conflicts", "Web pages", "冲突处理", `href="/b/b/t/1"`, "3 posts in 3 threads",
		`action="/b/b/"`, `class="board-name" href="/b/b/"`, "<title>Threads · Board B · agora</title>")
	if strings.Index(r.body, "冲突处理") > strings.Index(r.body, "Mutagen conflicts") {
		t.Error("threads aren't newest first")
	}
	expect(t, s.get("/b/b/?t=web"), 200, "Web pages", "冲突处理", `<a class="chip-clear" href="/b/b/">Clear all</a>`)
	lacks(t, s.get("/b/b/?t=web"), `href="/b/b/t/1"`)
	r = s.get("/b/b/?author=codex")
	expect(t, r, 200, "Mutagen conflicts", "by codex", `href="/b/b/?author=codex&amp;t=mutagen"`)
	lacks(t, r, `href="/b/b/t/2"`)
	r = s.get("/b/b/?q=star")
	expect(t, r, 200, "<mark>star</mark>", "<mark>STAR</mark>", `href="/b/b/t/2#2"`)
	lacks(t, r, "冲突处理")
	expect(t, s.get("/b/b/?q=nothing-matches-this"), 200, "Nothing matches", `<a href="/b/b/">See every thread</a>`)
	expect(t, s.get("/b/b/?q=%232"), 200, "Go to #2", `href="/b/b/t/2#2"`)
	expect(t, s.get("/b/b/?n=1"), 200, "Show more threads", `href="/b/b/?n=51"`)

	for _, bad := range []string{"/b/b/?t=a+b", "/b/b/?n=0", "/b/b/?in=everything", "/b/b/?author=Bad/x"} {
		expect(t, s.get(bad), 400, "Back to Board B")
	}
	r = s.get("/b/b/tags")
	expect(t, r, 200, "web", "2 threads", "mutagen", `href="/b/b/?t=web"`)
	lacks(t, r, `href="/?`)
}

func TestEmptyBoard(t *testing.T) {
	s := serve(t, alice)
	expect(t, s.get("/b/b/"), 200, "No threads yet", `href="/b/b/new"`, "agora post --board b")
	expect(t, s.get("/b/b/tags"), 200, "No labels yet", "agora post --board b -t TAG")
}

func TestBoardsPage(t *testing.T) {
	s := serve(t, post.Identity{})
	s.board("quiet", "")
	s.thread(codex, "T", "body")
	s.reply(claude, 1, "a reply")
	s.board("ideas", "Ideas & plans")
	s.threadOn("ideas", claude, "Idea", "body")

	r := s.get("/")
	expect(t, r, 200, "<h1>Boards</h1>", "3 boards on this server, with 3 posts",
		`<a href="/b/b/">Board B</a><span class="t-id">b</span>`,
		`<a href="/b/ideas/">Ideas &amp; plans</a><span class="t-id">ideas</span>`,
		`<a href="/b/quiet/">quiet</a>`, "no posts yet",
		`<span class="num">2</span><span class="lbl"> posts</span>`, "created by <span class=\"who\"><b>codex</b>",
		"<title>Boards · agora</title>")
	// By last activity, then boards without posts.
	if !(strings.Index(r.body, `href="/b/ideas/"`) < strings.Index(r.body, `href="/b/b/"`) &&
		strings.Index(r.body, `href="/b/b/"`) < strings.Index(r.body, `href="/b/quiet/"`)) {
		t.Error("boards aren't in the order of agora boards")
	}
	// The boards' page belongs to no board: it has no search.
	lacks(t, r, `role="search"`, `class="board-name"`, `name="id"`, `action="/"`)
	r = s.send(http.MethodPost, "/", s.base, url.Values{"id": {"x"}})
	expect(t, r, 405)
	if r.header.Get("Allow") != "GET, HEAD" {
		t.Errorf("Allow = %q", r.header.Get("Allow"))
	}
	if len(s.boards()) != 3 {
		t.Error("a read-only page created a board")
	}
}

func TestServerWithoutBoards(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "board.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv := httptest.NewServer(New(Options{Store: st, Person: alice}))
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	expect(t, response{status: resp.StatusCode, body: string(body)}, 200, "No boards yet", "agora boards create", `id="new-board"`)
}

func TestCreateBoardForm(t *testing.T) {
	s := serve(t, alice)
	r := s.get("/")
	expect(t, r, 200, `<form id="new-board" class="card box new-board" action="/" method="post">`, `name="id"`, `name="title"`, `href="/#new-board"`)

	for _, origin := range []string{"", "http://evil.example", "null"} {
		expect(t, s.send(http.MethodPost, "/", origin, url.Values{"id": {"evil"}}), 403)
	}
	location(t, s.post("/", url.Values{"id": {"ideas"}, "title": {"  Ideas  "}}), 303, "/b/ideas/")
	location(t, s.post("/", url.Values{"id": {"plain"}, "title": {""}}), 303, "/b/plain/")
	bs := s.boards()
	titles := map[string]string{}
	for _, b := range bs {
		titles[b.ID] = b.Title + " by " + b.Creator
	}
	if len(bs) != 3 || titles["ideas"] != "Ideas by human/alice@laptop" || titles["plain"] != "plain by human/alice@laptop" {
		t.Fatalf("boards = %+v", bs)
	}
	expect(t, s.get("/b/ideas/"), 200, "<h1>Ideas</h1>", "No threads yet")

	// A taken or invalid id gets the form again, with what was typed.
	r = s.post("/", url.Values{"id": {"ideas"}, "title": {"Second ideas"}})
	expect(t, r, 400, `Board &#34;ideas&#34; exists.`, `value="ideas"`, `value="Second ideas"`, `id="new-board"`, `href="/b/ideas/"`)
	r = s.post("/", url.Values{"id": {"Bad Id"}, "title": {"Mine"}})
	expect(t, r, 400, "Invalid board", `value="Bad Id"`, `value="Mine"`)
	expect(t, s.post("/", url.Values{"id": {"x"}, "title": {"two\nlines"}}), 400, "single line")
	if len(s.boards()) != 3 {
		t.Errorf("refused forms created boards: %+v", s.boards())
	}
}

func TestBoardsKeepTheirOwnPosts(t *testing.T) {
	s := serve(t, alice)
	s.board("other", "Other board")
	s.thread(codex, "On b", "first on b", "only-b")
	s.thread(codex, "Second on b", "see #1")
	s.threadOn("other", claude, "On other", "first on other", "only-other")
	s.threadOn("other", claude, "Second on other", "see #1, not #2")

	r := s.get("/b/other/")
	expect(t, r, 200, "On other", `href="/b/other/t/1"`, `href="/b/other/?t=only-other"`, "2 posts in 2 threads", "claude-code")
	lacks(t, r, "On b", "only-b", "codex")
	r = s.get("/b/other/t/2")
	expect(t, r, 200, `<a href="/b/other/t/1#1" class="ref">#1</a>`, "not #2", `action="/b/other/t/2#reply"`,
		`action="/b/other/t/2/delete"`, `href="/b/other/t/2/edit"`, `<a href="/b/other/">Threads</a>`)
	lacks(t, r, `/b/b/t/`)
	expect(t, s.get("/b/other/t/1"), 200, "On other", `href="/b/other/t/2#2"`)
	expect(t, s.get("/b/b/t/1"), 200, "On b", `href="/b/b/t/2#2"`)
	expect(t, s.get("/b/other/t/3"), 404, "No post #3 on board other", "Back to Other board")
	r = s.get("/b/other/?q=first")
	expect(t, r, 200, "<mark>first</mark> on other", `href="/b/other/t/1#1"`)
	lacks(t, r, "On b</a>", "on b")

	// Forms write to the board of their address.
	location(t, s.post("/b/other/new", url.Values{"title": {"Third"}, "body": {"x"}}), 303, "/b/other/t/3#3")
	location(t, s.post("/b/other/t/1", url.Values{"body": {"answer"}}), 303, "/b/other/t/1#4")
	if len(s.threadsOn("other")) != 3 || len(s.threads()) != 2 {
		t.Errorf("threads: other %+v, b %+v", s.threadsOn("other"), s.threads())
	}
	location(t, s.post("/b/other/t/4/delete", nil), 303, "/b/other/t/1#1")
	location(t, s.post("/b/other/t/3/delete", nil), 303, "/b/other/")
	if p, err := s.store.Get(context.Background(), "b", 1); err != nil || p.Title != "On b" {
		t.Errorf("b's #1 = %+v, %v", p, err)
	}
}

func TestBoardAddresses(t *testing.T) {
	s := serve(t, alice)
	s.thread(codex, "T", "body")

	r := s.get("/b/b")
	location(t, r, 301, "/b/b/")
	if r.header.Get("Content-Security-Policy") != security {
		t.Errorf("redirect headers %v", r.header)
	}
	location(t, s.get("/b/b?t=x&n=2"), 301, "/b/b/?t=x&n=2")
	location(t, s.get("/b/nope"), 301, "/b/nope/")
	expect(t, s.get("/b/Bad"), 400, "Invalid board")

	for _, path := range []string{"/b/Bad/", "/b/Bad/tags", "/b/Bad/t/1", "/b/Bad/new", "/b/-x/t/1/edit"} {
		expect(t, s.get(path), 400, "Invalid board", `href="/">See every board</a>`)
	}
	for _, path := range []string{"/b/nope/", "/b/nope/tags", "/b/nope/t/1", "/b/nope/new", "/b/nope/t/1/edit"} {
		r := s.get(path)
		expect(t, r, 404, "There is no board “nope” on this server.", `href="/">See every board</a>`)
		lacks(t, r, `role="search"`)
	}
	r = s.post("/b/nope/new", url.Values{"title": {"Lost title"}, "body": {"lost words"}})
	expect(t, r, 404, "There is no board “nope”", "Lost title", "lost words", "Nothing was saved")
	expect(t, s.post("/b/nope/t/1", url.Values{"body": {"a lost reply"}}), 404, "a lost reply")
	expect(t, s.post("/b/nope/t/1/delete", nil), 404)
	if len(s.boards()) != 1 {
		t.Error("an unknown board was created")
	}
	// agora 2.0's addresses, without a board, are gone.
	for _, path := range []string{"/t/1", "/tags", "/new", "/t/1/edit", "/b/", "/b", "/b/b/t", "/b/b/nope"} {
		expect(t, s.get(path), 404, "There is no such page.")
	}
	expect(t, s.post("/t/1/delete", nil), 404)
}

func TestThreadRendersMarkdownAndStoredRefs(t *testing.T) {
	s := serve(t, post.Identity{})
	first := s.thread(codex, "First", "the first")
	root := s.thread(claude, "Second", "**bold** and see #1, not #99, `#1` in code, <script>alert(1)</script>\n\n"+
		"| a | b |\n|---|---|\n| 1 | 2 |\n\n[bad](javascript:alert(1)) [good](https://example.com)\n", "design")
	rep := s.reply(alice, root, "a reply")
	s.reply(codex, rep, "a reply to the reply")
	_ = first

	r := s.get("/b/b/t/2")
	expect(t, r, 200,
		"<strong>bold</strong>", `<a href="/b/b/t/1#1" class="ref">#1</a>`, "not #99", "<code>#1</code>",
		"<table>", `href="https://example.com"`, "in reply to <b>#3</b>", "Original post",
		"agora reply 2 --board b -f answer.md", "Participants", `href="/b/b/?t=design"`)
	lacks(t, r, `/t/99`, "javascript:", "<script>alert")
	// Post 1 is linked from post 2.
	expect(t, s.get("/b/b/t/1"), 200, "Linked from", `href="/b/b/t/2#2"`, "Second")
	// A reply's address shows its thread.
	expect(t, s.get("/b/b/t/4"), 200, "Second", `id="4" class="post focus"`)
	expect(t, s.get("/b/b/t/99"), 404, "No post #99")
	expect(t, s.get("/b/b/t/x"), 400, "Back to Board B")
	expect(t, s.get("/b/b/t/012"), 400)
}

func TestReadOnlyPagesOfferNoForms(t *testing.T) {
	s := serve(t, post.Identity{})
	root := s.thread(codex, "T", "body")
	r := s.get("/b/b/t/1")
	expect(t, r, 200)
	lacks(t, r, `name="nonce"`, `/t/1/delete`, `/t/1/edit`, `/b/b/new`, `id="reply"`)
	lacks(t, s.get("/b/b/"), `/b/b/new`)
	lacks(t, s.get("/"), `new-board`)
	expect(t, s.get("/b/b/new"), 404)
	expect(t, s.get("/b/b/t/1/edit"), 404)
	expect(t, s.post("/b/b/t/1/delete", nil), 404)
	expect(t, s.post("/b/b/new", url.Values{"title": {"x"}}), 404)
	r = s.post("/b/b/t/1", url.Values{"body": {"x"}})
	expect(t, r, 405)
	if r.header.Get("Allow") != "GET, HEAD" {
		t.Errorf("Allow = %q", r.header.Get("Allow"))
	}
	if ts := s.threads(); len(ts) != 1 || ts[0].ID != root || ts[0].Replies != 0 {
		t.Errorf("threads = %+v", ts)
	}
}

func TestFormsNeedTheirOrigin(t *testing.T) {
	s := serve(t, alice)
	fields := url.Values{"title": {"From elsewhere"}, "body": {"x"}}
	expect(t, s.send(http.MethodPost, "/b/b/new", "", fields), 403)
	expect(t, s.send(http.MethodPost, "/b/b/new", "http://evil.example", fields), 403)
	expect(t, s.send(http.MethodPost, "/b/b/new", "null", fields), 403)
	if ts := s.threads(); len(ts) != 0 {
		t.Fatalf("refused forms published %+v", ts)
	}
	location(t, s.post("/b/b/new", url.Values{"title": {"Mine"}, "body": {"hello"}, "tags": {"a, b"}}), 303, "/b/b/t/1#1")
	ts := s.threads()
	if len(ts) != 1 || ts[0].Title != "Mine" || ts[0].Author != "human/alice@laptop" || strings.Join(ts[0].Tags, ",") != "a,b" {
		t.Errorf("threads = %+v", ts)
	}
	// Deleting needs the origin too.
	expect(t, s.send(http.MethodPost, "/b/b/t/1/delete", "http://evil.example", nil), 403)
	if len(s.threads()) != 1 {
		t.Error("a refused delete deleted")
	}
}

var nonceField = regexp.MustCompile(`name="nonce" value="([0-9a-f]+)"`)

func TestFormSentTwicePostsOnce(t *testing.T) {
	s := serve(t, alice)
	m := nonceField.FindStringSubmatch(s.get("/b/b/new").body)
	if m == nil {
		t.Fatal("/b/b/new has no nonce")
	}
	fields := url.Values{"title": {"Once"}, "body": {"only once"}, "nonce": {m[1]}}
	first, second := s.post("/b/b/new", fields), s.post("/b/b/new", fields)
	location(t, first, 303, "/b/b/t/1#1")
	location(t, second, 303, "/b/b/t/1#1")
	if ts := s.threads(); len(ts) != 1 {
		t.Errorf("threads = %+v", ts)
	}

	// The thread page's reply form gets a nonce of its own.
	m2 := nonceField.FindStringSubmatch(s.get("/b/b/t/1").body)
	if m2 == nil || m2[1] == m[1] {
		t.Fatalf("reply nonce = %v", m2)
	}
	reply := url.Values{"body": {"answer"}, "nonce": {m2[1]}}
	location(t, s.post("/b/b/t/1", reply), 303, "/b/b/t/1#2")
	location(t, s.post("/b/b/t/1", reply), 303, "/b/b/t/1#2")
	if ts := s.threads(); ts[0].Replies != 1 {
		t.Errorf("replies = %d", ts[0].Replies)
	}
}

func TestEditForms(t *testing.T) {
	s := serve(t, alice)
	root := s.thread(codex, "Old title", "old body", "x", "y")
	rep := s.reply(claude, root, "a reply")

	r := s.get("/b/b/t/1/edit")
	expect(t, r, 200, `name="title" value="Old title"`, "old body", `value="x y"`, "Written by <b>codex/019a2b3c@gpu</b>", `action="/b/b/t/1/edit"`,
		`<a class="btn" href="/b/b/t/1#1">Cancel</a>`)
	r = s.get("/b/b/t/2/edit")
	expect(t, r, 200, "Edit reply #2", "a reply", "in “Old title”", `<a href="/b/b/t/1#2">#1</a>`)
	lacks(t, r, `name="title"`)

	// Every field is sent: empty tags remove them.
	location(t, s.post("/b/b/t/1/edit", url.Values{"title": {"New title"}, "tags": {""}, "body": {"new body"}}), 303, "/b/b/t/1#1")
	p, err := s.store.Get(context.Background(), "b", root)
	if err != nil || p.Title != "New title" || p.Body != "new body\n" || len(p.Tags) != 0 || p.Editor != "human/alice@laptop" {
		t.Fatalf("after edit: %+v, %v", p, err)
	}
	expect(t, s.get("/b/b/t/1"), 200, "New title", "edited just now", "by <b>human/alice@laptop</b>")

	// A reply can't lose its body; the form comes back with what was typed.
	r = s.post("/b/b/t/2/edit", url.Values{"tags": {"keep-me"}, "body": {"  "}})
	expect(t, r, 400, "A reply needs a body", `value="keep-me"`, `action="/b/b/t/2/edit"`)
	r = s.post("/b/b/t/2/edit", url.Values{"title": {"no"}, "tags": {""}, "body": {"x"}})
	expect(t, r, 400, "only a thread&#39;s first post has a title")
	if p, _ := s.store.Get(context.Background(), "b", rep); p.Body != "a reply\n" {
		t.Errorf("refused edits changed the reply: %+v", p)
	}
	expect(t, s.get("/b/b/t/9/edit"), 404)
	expect(t, s.post("/b/b/t/9/edit", url.Values{"body": {"lost words"}, "tags": {""}}), 404, "lost words")
}

func TestDeleteForms(t *testing.T) {
	s := serve(t, alice)
	root := s.thread(codex, "Doomed", "body")
	rep := s.reply(claude, root, "reply one")
	s.reply(codex, rep, "reply two")

	r := s.get("/b/b/t/1")
	expect(t, r, 200, `action="/b/b/t/1/delete"`, "This deletes the whole thread, with its 2 replies, for good.", "This deletes this reply for good.")
	expect(t, s.get("/b/b/t/2/delete"), 405)
	// A reply's address replies to it, and offers the thread instead.
	expect(t, s.get("/b/b/t/2"), 200, `action="/b/b/t/2#reply"`, "Reply to #2", `<a class="small" href="/b/b/t/1#reply">`)

	location(t, s.post("/b/b/t/2/delete", nil), 303, "/b/b/t/1#1")
	r = s.get("/b/b/t/1")
	expect(t, r, 200, "reply two")
	lacks(t, r, "reply one", "in reply to")

	location(t, s.post("/b/b/t/1/delete", nil), 303, "/b/b/")
	expect(t, s.get("/b/b/t/1"), 404)
	expect(t, s.get("/b/b/t/3"), 404)
	expect(t, s.post("/b/b/t/1/delete", nil), 404)
}

func TestRefusedReplies(t *testing.T) {
	s := serve(t, alice)
	root := s.thread(codex, "T", "body")

	r := s.post("/b/b/t/1", url.Values{"body": {" "}, "tags": {"typed-tag"}, "nonce": {"abc"}})
	expect(t, r, 400, "A reply needs a body", `id="reply"`, `value="typed-tag"`, `value="abc"`)
	r = s.post("/b/b/t/1", url.Values{"body": {"words"}, "tags": {"a#b"}})
	expect(t, r, 400, "Invalid tag", "words")
	r = s.post("/b/b/new", url.Values{"title": {""}, "body": {"my draft"}})
	expect(t, r, 400, "A thread needs a title", "my draft", `action="/b/b/new"`)

	if _, err := s.store.Delete(context.Background(), codex, "b", root); err != nil {
		t.Fatal(err)
	}
	r = s.post("/b/b/t/1", url.Values{"body": {"typed while it went away"}})
	expect(t, r, 404, "No post #1", "typed while it went away", "Nothing was saved")

	r = s.post("/b/b/new", url.Values{"title": {"big"}, "body": {strings.Repeat("x", 1<<20)}})
	expect(t, r, 413)
	expect(t, s.post("/", url.Values{"id": {"big"}, "title": {strings.Repeat("x", 1<<20)}}), 413)
	if ts := s.threads(); len(ts) != 0 {
		t.Errorf("threads = %+v", ts)
	}
	if len(s.boards()) != 1 {
		t.Error("a refused form created a board")
	}
}

func TestPageHeadersAndAddresses(t *testing.T) {
	s := serve(t, alice)
	s.thread(codex, "T", "body")
	for _, path := range []string{"/", "/b/b/", "/b/b/t/1", "/b/b/tags", "/b/b/new", "/b/b/t/1/edit", "/b/nope/", "/b/Bad/", "/nope", "/static/style.css"} {
		r := s.get(path)
		if r.header.Get("Content-Security-Policy") != security ||
			r.header.Get("X-Content-Type-Options") != "nosniff" ||
			r.header.Get("Referrer-Policy") != "same-origin" {
			t.Errorf("%s: headers %v", path, r.header)
		}
	}
	r := s.get("/static/style.css")
	if r.status != 200 || !strings.HasPrefix(r.header.Get("Content-Type"), "text/css") {
		t.Errorf("style.css: %d %q", r.status, r.header.Get("Content-Type"))
	}
	expect(t, s.get("/static/favicon.svg"), 200)
	expect(t, s.get("/static/"), 404)
	expect(t, s.get("/nope"), 404, "There is no such page.")
	expect(t, s.get("/b/b/t/1/"), 404)
	r = s.send(http.MethodPut, "/", s.base, nil)
	if r.status != 405 || r.header.Get("Allow") != "GET, HEAD, POST" {
		t.Errorf("PUT / = %d, Allow %q", r.status, r.header.Get("Allow"))
	}
	r = s.send(http.MethodPut, "/b/b/", s.base, nil)
	if r.status != 405 || r.header.Get("Allow") != "GET, HEAD" {
		t.Errorf("PUT /b/b/ = %d, Allow %q", r.status, r.header.Get("Allow"))
	}
	r = s.send(http.MethodPut, "/b/b/t/1", s.base, nil)
	if r.status != 405 || r.header.Get("Allow") != "GET, HEAD, POST" {
		t.Errorf("PUT /b/b/t/1 = %d, Allow %q", r.status, r.header.Get("Allow"))
	}
	req, _ := http.NewRequest(http.MethodHead, s.base+"/b/b/t/1", nil)
	if r := s.do(req); r.status != 200 || r.body != "" {
		t.Errorf("HEAD = %d %q", r.status, r.body)
	}
	expect(t, s.get("/"), 200, "Posting as <b>human/alice@laptop</b>", "agora v2.0.0-test")
}
