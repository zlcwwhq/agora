package server

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/zlcwwhq/agora/internal/client"
	"github.com/zlcwwhq/agora/internal/post"
)

func TestAPIReaders(t *testing.T) {
	base, _ := start(t, post.Identity{})
	ctx := context.Background()
	cx, al, cl := client.New(base, "b", codex), client.New(base, "b", alice), client.New(base, "b", claude)

	root, err := cx.Post(ctx, post.NewThread{Title: "Plan", Tags: []string{"a", "b"}, Body: "ask @claude-code/95d3"})
	if err != nil {
		t.Fatal(err)
	}
	p, err := al.Edit(ctx, root.ID, post.Edit{AddTags: &[]string{"c"}, RemoveTags: &[]string{"a"}})
	if err != nil || !slices.Equal(p.Tags, []string{"b", "c"}) || p.Versions != 2 || !slices.Equal(p.Mentions, []string{"claude-code/95d3"}) {
		t.Fatalf("Edit = %+v, %v", p, err)
	}
	h, err := cx.History(ctx, root.ID)
	if err != nil || h.ID != root.ID || len(h.Versions) != 2 || !slices.Equal(h.Versions[0].Tags, []string{"a", "b"}) ||
		h.Versions[1].Author != "human/alice@laptop" {
		t.Errorf("History = %+v, %v", h, err)
	}
	reacted, err := cl.React(ctx, root.ID, post.React{Reaction: "ack"})
	if err != nil || reacted.Thread != root.ID || len(reacted.Reactions) != 1 || reacted.Reactions[0].Authors[0] != "claude-code/95d3e5c1@laptop" {
		t.Errorf("React = %+v, %v", reacted, err)
	}
	if reacted, err = cl.React(ctx, root.ID, post.React{Reaction: "ack", Undo: true}); err != nil || len(reacted.Reactions) != 0 {
		t.Errorf("React --undo = %+v, %v", reacted, err)
	}

	// A new reader for which the post is, then an edit since it saw it.
	u, err := cl.Unread(ctx, post.UnreadRequest{ToMe: true, Since: 3600})
	if err != nil || !u.NewReader || len(u.Posts) != 1 || u.Posts[0].ID != root.ID {
		t.Fatalf("Unread --to-me = %+v, %v", u, err)
	}
	body := "ask @claude-code/95d3 again"
	if _, err := cx.Edit(ctx, root.ID, post.Edit{Body: &body}); err != nil {
		t.Fatal(err)
	}
	if u, err = cl.Unread(ctx, post.UnreadRequest{Thread: root.ID}); err != nil || len(u.Posts) != 0 || len(u.Edited) != 1 {
		t.Errorf("Unread --thread = %+v, %v", u, err)
	}
	if _, err := cl.Unread(ctx, post.UnreadRequest{Thread: 99}); post.KindOf(err) != post.NotFound {
		t.Errorf("Unread of a missing thread: %v", err)
	}
	if _, err := cx.History(ctx, 99); post.KindOf(err) != post.NotFound {
		t.Errorf("History of a missing post: %v", err)
	}

	// Empty lists are [], never null.
	rep, err := cx.Reply(ctx, root.ID, post.NewReply{Body: "plain"})
	if err != nil {
		t.Fatal(err)
	}
	raw := func(method, path, body string) (int, http.Header, string) {
		t.Helper()
		req, _ := http.NewRequest(method, base+path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set(AuthorHeader, alice.Header())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header, string(data)
	}
	_, _, show := raw("GET", "/api/boards/b/posts/2", "")
	for _, want := range []string{`"id":2,`, `"versions":1,`, `"mentions":[],"reactions":[]`, `"versions":3,`} {
		if !strings.Contains(show, want) {
			t.Errorf("show lacks %s: %s", want, show)
		}
	}
	if code, _, u := raw("POST", "/api/boards/b/unread", `{"peek": true}`); code != 200 || !strings.Contains(u, `"edited":[]}`) {
		t.Errorf("unread = %d %s", code, u)
	}
	_ = rep
	for _, c := range []struct {
		method, path, body string
		want               int
	}{
		{"PATCH", "/api/boards/b/posts/1", `{"tags": [], "add_tags": ["x"]}`, 400},
		{"PATCH", "/api/boards/b/posts/1", `{"tags": ["x"], "remove_tags": []}`, 400},
		{"PATCH", "/api/boards/b/posts/1", `{"add_tags": ["a b"]}`, 400},
		{"POST", "/api/boards/b/posts/1/reactions", `{"reaction": "two words"}`, 400},
		{"POST", "/api/boards/b/posts/1/reactions", `{"reaction": "ack", "extra": 1}`, 400},
		{"POST", "/api/boards/b/posts/9/reactions", `{"reaction": "ack"}`, 404},
		{"POST", "/api/boards/b/posts/x/reactions", `{"reaction": "ack"}`, 400},
		{"GET", "/api/boards/b/posts/9/history", "", 404},
		{"GET", "/api/boards/b/posts/01/history", "", 400},
		{"GET", "/api/boards/nope/posts/1/history", "", 404},
		{"POST", "/api/boards/b/unread", `{"thread": -1}`, 400},
		{"POST", "/api/boards/b/unread", `{"thread": 9}`, 404},
	} {
		if code, _, body := raw(c.method, c.path, c.body); code != c.want {
			t.Errorf("%s %s %s: %d, want %d: %s", c.method, c.path, c.body, code, c.want, body)
		}
	}
	for path, allow := range map[string]string{"/api/boards/b/posts/1/history": "GET, HEAD", "/api/boards/b/posts/1/reactions": "POST"} {
		if code, header, _ := raw("PUT", path, `{}`); code != 405 || header.Get("Allow") != allow {
			t.Errorf("PUT %s: %d, Allow %q", path, code, header.Get("Allow"))
		}
	}
	// Reacting writes, so it needs an identity, and the body JSON.
	req, _ := http.NewRequest("POST", base+"/api/boards/b/posts/1/reactions", strings.NewReader(`{"reaction": "ack"}`))
	req.Header.Set("Content-Type", "application/json")
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 400 {
		t.Errorf("reaction without an identity: %v, %v", resp, err)
	} else {
		_ = resp.Body.Close()
	}
	if code, _, _ := raw("POST", "/api/boards/b/posts/1/reactions", ""); code != 415 {
		t.Errorf("reaction without a body: %d", code)
	}
}

func TestThreadShowsWhatReadersSee(t *testing.T) {
	s := serve(t, post.Identity{})
	ctx := context.Background()
	root := s.thread(codex, "T", "ask @claude-code/95d3, not `@nope` or alice@laptop, [@codex](https://example.com)")
	rep := s.reply(alice, root, "plain")
	for _, who := range []post.Identity{claude, alice} {
		if _, err := s.store.React(ctx, who, "b", root, post.React{Reaction: "👀"}); err != nil {
			t.Fatal(err)
		}
	}
	body := "ask @claude-code/95d3, not `@nope` or alice@laptop, [@codex](https://example.com), then @human"
	if _, err := s.store.Edit(ctx, alice, "b", root, post.Edit{Body: &body}); err != nil {
		t.Fatal(err)
	}
	r := s.get("/b/b/t/1")
	expect(t, r, 200,
		`<a href="/b/b/?author=claude-code/95d3" class="mention">@claude-code/95d3</a>`,
		`<a href="/b/b/?author=human" class="mention">@human</a>`,
		"<code>@nope</code>", `<a href="https://example.com">@codex</a>`,
		`class="reaction"`, `<span class="r">👀</span><span class="n">2</span>`,
		"claude-code/95d3e5c1@laptop, human/alice@laptop",
		`href="/b/b/t/1/history"`, "by <b>human/alice@laptop</b>")
	lacks(t, r, `?author=nope`, `?author=laptop`, `/b/b/t/2/history`)
	_ = rep
}

func TestHistoryPage(t *testing.T) {
	s := serve(t, post.Identity{})
	ctx := context.Background()
	root := s.thread(codex, "Old title", "one\ntwo\nthree\nfour\nfive\nsix\nseven\n", "design")
	edit := func(who post.Identity, e post.Edit) {
		t.Helper()
		if _, err := s.store.Edit(ctx, who, "b", root, e); err != nil {
			t.Fatal(err)
		}
	}
	body := "one\ntwo\nthree\n4\nfive\nsix\nseven\n"
	edit(codex, post.Edit{Body: &body})
	title := "New title"
	edit(alice, post.Edit{Title: &title, AddTags: &[]string{"web"}})

	r := s.get("/b/b/t/1/history")
	expect(t, r, 200, "Versions of #1", "3 versions, newest first", `href="/b/b/t/1#1"`,
		"Version 3", "Version 2", "Version 1",
		`<span class="del"><span class="sign" aria-hidden="true">-</span><span class="sr">removed: </span>four</span>`,
		`<span class="add"><span class="sign" aria-hidden="true">&#43;</span><span class="sr">added: </span>4</span>`,
		`<span class="del"><span class="sign" aria-hidden="true">-</span><span class="sr">removed: </span>Old title</span>`,
		`<span class="sr">added: </span>web</span>`,
		"<h2>Labels</h2>", "<h2>Title</h2>", "<h2>Message</h2>", "<b>human</b>", "<b>codex</b>",
		// The first version shows the post as it was created.
		"one\ntwo\nthree\nfour\n")
	if i, j := strings.Index(r.body, "Version 3"), strings.Index(r.body, "Version 1"); i > j {
		t.Error("versions aren't newest first")
	}
	// Version 3 changed no line of the body, and version 2 only the body.
	v2 := r.body[strings.Index(r.body, `id="v2"`):strings.Index(r.body, `id="v1"`)]
	if strings.Contains(v2, "<h2>Title</h2>") || strings.Contains(v2, "<h2>Labels</h2>") || !strings.Contains(v2, `<span class="gap">⋯</span>`) {
		t.Errorf("version 2:\n%s", v2)
	}

	expect(t, s.get("/b/b/t/99/history"), 404, "No post #99")
	expect(t, s.get("/b/b/t/x/history"), 400)
	expect(t, s.get("/b/nope/t/1/history"), 404)
	r = s.send(http.MethodPost, "/b/b/t/1/history", s.base, nil)
	if r.status != 405 || r.header.Get("Allow") != "GET, HEAD" {
		t.Errorf("POST history = %d, Allow %q", r.status, r.header.Get("Allow"))
	}
	if r := s.get("/b/b/t/1/history"); r.header.Get("Content-Security-Policy") != security {
		t.Errorf("headers %v", r.header)
	}
	// A post that was never edited has one version.
	rep := s.reply(claude, root, "a reply")
	expect(t, s.get("/b/b/t/2/history"), 200, "1 version, newest first", "a reply")
	_ = rep
}

func TestDiffLines(t *testing.T) {
	show := func(ls []diffLine) string {
		var b strings.Builder
		for _, l := range ls {
			b.WriteString(string(l.Op) + l.Text + "|")
		}
		return b.String()
	}
	split := func(s string) []string { return strings.Fields(s) }
	for _, c := range []struct{ a, b, want string }{
		{"1 2 3 4 5 6 7 8 9", "1 2 3 4 x 6 7 8 9", "~| 3| 4|-5|+x| 6| 7|~|"},
		{"1 2", "1 2 3", " 1| 2|+3|"},
		{"a b c", "c b a", "-a|-b| c|+b|+a|"},
		{"", "new", "+new|"},
		{"same", "same", "~|"},
		{"1 2 3 4 5 6 7 8 9 10", "1 x 3 4 5 6 7 8 y 10", " 1|-2|+x| 3| 4|~| 7| 8|-9|+y| 10|"},
	} {
		if got := show(diffLines(split(c.a), split(c.b))); got != c.want {
			t.Errorf("diffLines(%q, %q) = %s, want %s", c.a, c.b, got, c.want)
		}
	}
	// Changes too large for the table remove and add every line between the
	// common start and end.
	big := make([]string, 2100)
	other := make([]string, 2100)
	for i := range big {
		big[i], other[i] = "a"+string(rune('0'+i%10)), "b"+string(rune('0'+i%10))
	}
	ls := diffLines(append([]string{"keep"}, big...), append([]string{"keep"}, other...))
	if len(ls) != 1+2*2100 || ls[0].Op != same || ls[1].Op != removed || ls[len(ls)-1].Op != added {
		t.Errorf("large diff: %d lines, first %+v", len(ls), ls[:2])
	}
}
