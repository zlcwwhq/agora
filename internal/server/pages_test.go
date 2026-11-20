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

// site is a board served with its pages.
type site struct {
	t     *testing.T
	base  string
	store *store.Store
	http  *http.Client
}

// serve serves a board of its own, with forms that act as person unless it
// is zero.
func serve(t *testing.T, person post.Identity) *site {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "board.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv := httptest.NewServer(New(Options{Board: st, Person: person, Version: "v2.0.0-test", Logf: t.Logf}))
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

func (s *site) thread(who post.Identity, title, body string, tags ...string) int64 {
	s.t.Helper()
	pub, _, err := s.store.Post(context.Background(), who, post.NewThread{Title: title, Body: body, Tags: tags})
	if err != nil {
		s.t.Fatal(err)
	}
	return pub.ID
}

func (s *site) reply(who post.Identity, to int64, body string) int64 {
	s.t.Helper()
	pub, _, err := s.store.Reply(context.Background(), who, to, post.NewReply{Body: body})
	if err != nil {
		s.t.Fatal(err)
	}
	return pub.ID
}

func (s *site) threads() []post.Thread {
	s.t.Helper()
	ts, err := s.store.List(context.Background(), post.Filter{Limit: 100})
	if err != nil {
		s.t.Fatal(err)
	}
	return ts
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

func TestIndexListsAndFilters(t *testing.T) {
	s := serve(t, post.Identity{})
	s.thread(codex, "Mutagen conflicts", "about stars", "mutagen")
	s.thread(claude, "Web pages", "The STAR of the show", "web")
	s.thread(alice, "冲突处理", "中文正文", "web")

	r := s.get("/")
	expect(t, r, 200, "Mutagen conflicts", "Web pages", "冲突处理", `href="/t/1"`, "3 posts in 3 threads")
	if strings.Index(r.body, "冲突处理") > strings.Index(r.body, "Mutagen conflicts") {
		t.Error("threads aren't newest first")
	}
	expect(t, s.get("/?t=web"), 200, "Web pages", "冲突处理", "Clear all")
	lacks(t, s.get("/?t=web"), `href="/t/1"`)
	r = s.get("/?author=codex")
	expect(t, r, 200, "Mutagen conflicts", "by codex")
	lacks(t, r, `href="/t/2"`)
	r = s.get("/?q=star")
	expect(t, r, 200, "<mark>star</mark>", "<mark>STAR</mark>", `href="/t/2#2"`)
	lacks(t, r, "冲突处理")
	expect(t, s.get("/?q=nothing-matches-this"), 200, "Nothing matches")
	expect(t, s.get("/?q=%232"), 200, "Go to #2")
	expect(t, s.get("/?n=1"), 200, "Show more threads")

	for _, bad := range []string{"/?t=a+b", "/?n=0", "/?in=everything", "/?author=Bad/x"} {
		expect(t, s.get(bad), 400)
	}
	expect(t, s.get("/tags"), 200, "web", "2 threads", "mutagen")
}

func TestEmptyBoard(t *testing.T) {
	s := serve(t, alice)
	expect(t, s.get("/"), 200, "No threads yet", `href="/new"`)
	expect(t, s.get("/tags"), 200, "No labels yet")
}

func TestThreadRendersMarkdownAndStoredRefs(t *testing.T) {
	s := serve(t, post.Identity{})
	first := s.thread(codex, "First", "the first")
	root := s.thread(claude, "Second", "**bold** and see #1, not #99, `#1` in code, <script>alert(1)</script>\n\n"+
		"| a | b |\n|---|---|\n| 1 | 2 |\n\n[bad](javascript:alert(1)) [good](https://example.com)\n", "design")
	rep := s.reply(alice, root, "a reply")
	s.reply(codex, rep, "a reply to the reply")
	_ = first

	r := s.get("/t/2")
	expect(t, r, 200,
		"<strong>bold</strong>", `<a href="/t/1#1" class="ref">#1</a>`, "not #99", "<code>#1</code>",
		"<table>", `href="https://example.com"`, "in reply to <b>#3</b>", "Original post",
		"agora reply 2 -f answer.md", "Participants")
	lacks(t, r, `href="/t/99`, "javascript:", "<script>alert")
	// Post 1 is linked from post 2.
	expect(t, s.get("/t/1"), 200, "Linked from", `href="/t/2#2"`, "Second")
	// A reply's address shows its thread.
	expect(t, s.get("/t/4"), 200, "Second", `id="4" class="post focus"`)
	expect(t, s.get("/t/99"), 404, "No post #99")
	expect(t, s.get("/t/x"), 400)
	expect(t, s.get("/t/012"), 400)
}

func TestReadOnlyPagesOfferNoForms(t *testing.T) {
	s := serve(t, post.Identity{})
	root := s.thread(codex, "T", "body")
	r := s.get("/t/1")
	expect(t, r, 200)
	lacks(t, r, `name="nonce"`, `/t/1/delete`, `/t/1/edit`, `href="/new"`, `id="reply"`)
	lacks(t, s.get("/"), `href="/new"`)
	expect(t, s.get("/new"), 404)
	expect(t, s.get("/t/1/edit"), 404)
	expect(t, s.post("/t/1/delete", nil), 404)
	expect(t, s.post("/new", url.Values{"title": {"x"}}), 404)
	r = s.post("/t/1", url.Values{"body": {"x"}})
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
	expect(t, s.send(http.MethodPost, "/new", "", fields), 403)
	expect(t, s.send(http.MethodPost, "/new", "http://evil.example", fields), 403)
	expect(t, s.send(http.MethodPost, "/new", "null", fields), 403)
	if ts := s.threads(); len(ts) != 0 {
		t.Fatalf("refused forms published %+v", ts)
	}
	r := s.post("/new", url.Values{"title": {"Mine"}, "body": {"hello"}, "tags": {"a, b"}})
	if r.status != http.StatusSeeOther || r.header.Get("Location") != "/t/1#1" {
		t.Fatalf("POST /new = %d %q", r.status, r.header.Get("Location"))
	}
	ts := s.threads()
	if len(ts) != 1 || ts[0].Title != "Mine" || ts[0].Author != "human/alice@laptop" || strings.Join(ts[0].Tags, ",") != "a,b" {
		t.Errorf("threads = %+v", ts)
	}
	// Deleting needs the origin too.
	expect(t, s.send(http.MethodPost, "/t/1/delete", "http://evil.example", nil), 403)
	if len(s.threads()) != 1 {
		t.Error("a refused delete deleted")
	}
}

var nonceField = regexp.MustCompile(`name="nonce" value="([0-9a-f]+)"`)

func TestFormSentTwicePostsOnce(t *testing.T) {
	s := serve(t, alice)
	m := nonceField.FindStringSubmatch(s.get("/new").body)
	if m == nil {
		t.Fatal("/new has no nonce")
	}
	fields := url.Values{"title": {"Once"}, "body": {"only once"}, "nonce": {m[1]}}
	first, second := s.post("/new", fields), s.post("/new", fields)
	if first.status != 303 || second.status != 303 || first.header.Get("Location") != second.header.Get("Location") {
		t.Fatalf("posts = %d %q, %d %q", first.status, first.header.Get("Location"), second.status, second.header.Get("Location"))
	}
	if ts := s.threads(); len(ts) != 1 {
		t.Errorf("threads = %+v", ts)
	}

	// The thread page's reply form gets a nonce of its own.
	m2 := nonceField.FindStringSubmatch(s.get("/t/1").body)
	if m2 == nil || m2[1] == m[1] {
		t.Fatalf("reply nonce = %v", m2)
	}
	reply := url.Values{"body": {"answer"}, "nonce": {m2[1]}}
	a, b := s.post("/t/1", reply), s.post("/t/1", reply)
	if a.status != 303 || a.header.Get("Location") != "/t/1#2" || b.header.Get("Location") != "/t/1#2" {
		t.Errorf("replies = %d %q, %q", a.status, a.header.Get("Location"), b.header.Get("Location"))
	}
	if ts := s.threads(); ts[0].Replies != 1 {
		t.Errorf("replies = %d", ts[0].Replies)
	}
}

func TestEditForms(t *testing.T) {
	s := serve(t, alice)
	root := s.thread(codex, "Old title", "old body", "x", "y")
	rep := s.reply(claude, root, "a reply")

	r := s.get("/t/1/edit")
	expect(t, r, 200, `name="title" value="Old title"`, "old body", `value="x y"`, "Written by <b>codex/019a2b3c@gpu</b>", `action="/t/1/edit"`)
	r = s.get("/t/2/edit")
	expect(t, r, 200, "Edit reply #2", "a reply", "in “Old title”")
	lacks(t, r, `name="title"`)

	// Every field is sent: empty tags remove them.
	r = s.post("/t/1/edit", url.Values{"title": {"New title"}, "tags": {""}, "body": {"new body"}})
	if r.status != 303 || r.header.Get("Location") != "/t/1#1" {
		t.Fatalf("POST edit = %d %q\n%.500s", r.status, r.header.Get("Location"), r.body)
	}
	p, err := s.store.Get(context.Background(), root)
	if err != nil || p.Title != "New title" || p.Body != "new body\n" || len(p.Tags) != 0 || p.Editor != "human/alice@laptop" {
		t.Fatalf("after edit: %+v, %v", p, err)
	}
	expect(t, s.get("/t/1"), 200, "New title", "edited just now", "by <b>human/alice@laptop</b>")

	// A reply can't lose its body; the form comes back with what was typed.
	r = s.post("/t/2/edit", url.Values{"tags": {"keep-me"}, "body": {"  "}})
	expect(t, r, 400, "A reply needs a body", `value="keep-me"`, `action="/t/2/edit"`)
	r = s.post("/t/2/edit", url.Values{"title": {"no"}, "tags": {""}, "body": {"x"}})
	expect(t, r, 400, "only a thread&#39;s first post has a title")
	if p, _ := s.store.Get(context.Background(), rep); p.Body != "a reply\n" {
		t.Errorf("refused edits changed the reply: %+v", p)
	}
	expect(t, s.get("/t/9/edit"), 404)
	expect(t, s.post("/t/9/edit", url.Values{"body": {"lost words"}, "tags": {""}}), 404, "lost words")
}

func TestDeleteForms(t *testing.T) {
	s := serve(t, alice)
	root := s.thread(codex, "Doomed", "body")
	rep := s.reply(claude, root, "reply one")
	s.reply(codex, rep, "reply two")

	r := s.get("/t/1")
	expect(t, r, 200, `action="/t/1/delete"`, "This deletes the whole thread, with its 2 replies, for good.", "This deletes this reply for good.")
	expect(t, s.get("/t/2/delete"), 405)

	r = s.post("/t/2/delete", nil)
	if r.status != 303 || r.header.Get("Location") != "/t/1#1" {
		t.Fatalf("deleting the reply = %d %q", r.status, r.header.Get("Location"))
	}
	r = s.get("/t/1")
	expect(t, r, 200, "reply two")
	lacks(t, r, "reply one", "in reply to")

	r = s.post("/t/1/delete", nil)
	if r.status != 303 || r.header.Get("Location") != "/" {
		t.Fatalf("deleting the root = %d %q", r.status, r.header.Get("Location"))
	}
	expect(t, s.get("/t/1"), 404)
	expect(t, s.get("/t/3"), 404)
	expect(t, s.post("/t/1/delete", nil), 404)
}

func TestRefusedReplies(t *testing.T) {
	s := serve(t, alice)
	root := s.thread(codex, "T", "body")

	r := s.post("/t/1", url.Values{"body": {" "}, "tags": {"typed-tag"}, "nonce": {"abc"}})
	expect(t, r, 400, "A reply needs a body", `id="reply"`, `value="typed-tag"`, `value="abc"`)
	r = s.post("/t/1", url.Values{"body": {"words"}, "tags": {"a#b"}})
	expect(t, r, 400, "Invalid tag", "words")
	r = s.post("/new", url.Values{"title": {""}, "body": {"my draft"}})
	expect(t, r, 400, "A thread needs a title", "my draft", `action="/new"`)

	if _, err := s.store.Delete(context.Background(), codex, root); err != nil {
		t.Fatal(err)
	}
	r = s.post("/t/1", url.Values{"body": {"typed while it went away"}})
	expect(t, r, 404, "No post #1", "typed while it went away", "Nothing was saved")

	r = s.post("/new", url.Values{"title": {"big"}, "body": {strings.Repeat("x", 1<<20)}})
	expect(t, r, 413)
	if ts := s.threads(); len(ts) != 0 {
		t.Errorf("threads = %+v", ts)
	}
}

func TestPageHeadersAndAddresses(t *testing.T) {
	s := serve(t, alice)
	s.thread(codex, "T", "body")
	for _, path := range []string{"/", "/t/1", "/tags", "/new", "/t/1/edit", "/nope", "/static/style.css"} {
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
	expect(t, s.get("/t/1/"), 404)
	r = s.send(http.MethodPut, "/", s.base, nil)
	expect(t, r, 405)
	if r.header.Get("Allow") != "GET, HEAD" {
		t.Errorf("Allow = %q", r.header.Get("Allow"))
	}
	r = s.send(http.MethodPut, "/t/1", s.base, nil)
	if r.status != 405 || r.header.Get("Allow") != "GET, HEAD, POST" {
		t.Errorf("PUT /t/1 = %d, Allow %q", r.status, r.header.Get("Allow"))
	}
	req, _ := http.NewRequest(http.MethodHead, s.base+"/t/1", nil)
	if r := s.do(req); r.status != 200 || r.body != "" {
		t.Errorf("HEAD = %d %q", r.status, r.body)
	}
	expect(t, s.get("/"), 200, "Posting as <b>human/alice@laptop</b>", "agora v2.0.0-test")
}
