package server

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/zlcwwhq/agora/internal/client"
	"github.com/zlcwwhq/agora/internal/post"
)

func TestAPIVotes(t *testing.T) {
	base, _ := start(t, post.Identity{})
	ctx := context.Background()
	cx, al := client.New(base, "b", codex), client.New(base, "b", alice)
	root, err := cx.Post(ctx, post.NewThread{Title: "Plan", Body: "a plan"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := al.Vote(ctx, root.ID, post.Vote{Vote: post.VoteDown}); err != nil {
		t.Fatal(err)
	}
	voted, err := cx.Vote(ctx, root.ID, post.Vote{Vote: post.VoteUp})
	if err != nil || voted.ID != root.ID || voted.Thread != root.ID || voted.Score != 0 ||
		!slices.Equal(voted.Votes.Up, []string{"codex/019a2b3c@gpu"}) || !slices.Equal(voted.Votes.Down, []string{"human/alice@laptop"}) {
		t.Errorf("Vote = %+v, %v", voted, err)
	}
	if voted, err = al.Vote(ctx, root.ID, post.Vote{Vote: post.VoteNone}); err != nil || voted.Score != 1 || len(voted.Votes.Down) != 0 {
		t.Errorf("Vote none = %+v, %v", voted, err)
	}

	raw := func(method, path, author, body string) (int, http.Header, string) {
		t.Helper()
		req, _ := http.NewRequest(method, base+path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if author != "" {
			req.Header.Set(AuthorHeader, author)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		data, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header, string(data)
	}
	// The shapes carry them, as cli.md writes them.
	_, _, show := raw("GET", "/api/boards/b/posts/1", "", "")
	for _, want := range []string{`"locked":false,"score":1}`, `"reactions":[],"score":1,"votes":{"up":["codex/019a2b3c@gpu"],"down":[]},`} {
		if !strings.Contains(show, want) {
			t.Errorf("show lacks %s: %s", want, show)
		}
	}
	if _, _, list := raw("GET", "/api/boards/b/threads", "", ""); !strings.Contains(list, `"score":1}`) {
		t.Errorf("threads lack the score: %s", list)
	}
	if code, _, body := raw("POST", "/api/boards/b/posts/1/votes", alice.Header(), `{"vote": "down"}`); code != 200 ||
		!strings.Contains(body, `{"id":1,"thread":1,"score":0,"votes":{"up":["codex/019a2b3c@gpu"],"down":["human/alice@laptop"]}}`) {
		t.Errorf("POST votes = %d %s", code, body)
	}

	for _, c := range []struct {
		method, path, author, body string
		want                       int
	}{
		{"POST", "/api/boards/b/posts/1/votes", alice.Header(), `{"vote": "sideways"}`, 400},
		{"POST", "/api/boards/b/posts/1/votes", alice.Header(), `{}`, 400},
		{"POST", "/api/boards/b/posts/1/votes", alice.Header(), `{"vote": "up", "undo": true}`, 400},
		{"POST", "/api/boards/b/posts/9/votes", alice.Header(), `{"vote": "up"}`, 404},
		{"POST", "/api/boards/b/posts/x/votes", alice.Header(), `{"vote": "up"}`, 400},
		{"POST", "/api/boards/nope/posts/1/votes", alice.Header(), `{"vote": "up"}`, 404},
		// Voting writes, so it needs an identity.
		{"POST", "/api/boards/b/posts/1/votes", "", `{"vote": "up"}`, 400},
	} {
		if code, _, body := raw(c.method, c.path, c.author, c.body); code != c.want {
			t.Errorf("%s %s %s: %d, want %d: %s", c.method, c.path, c.body, code, c.want, body)
		}
	}
	if code, header, _ := raw("GET", "/api/boards/b/posts/1/votes", alice.Header(), ""); code != 405 || header.Get("Allow") != "POST" {
		t.Errorf("GET votes: %d, Allow %q", code, header.Get("Allow"))
	}
}

// given returns the authors of a post's reaction, and its votes.
func (s *site) given(id int64, reaction string) ([]string, post.Votes, int) {
	s.t.Helper()
	p, err := s.store.Get(context.Background(), "b", id)
	if err != nil {
		s.t.Fatal(err)
	}
	var authors []string
	for _, r := range p.Reactions {
		if r.Reaction == reaction {
			authors = r.Authors
		}
	}
	return authors, p.Votes, p.Score
}

func TestPagesReact(t *testing.T) {
	s := serve(t, alice)
	ctx := context.Background()
	root := s.thread(codex, "Plan", "a plan")
	r := s.reply(claude, root, "done")
	if _, err := s.store.React(ctx, claude, "b", r, post.React{Reaction: "ack"}); err != nil {
		t.Fatal(err)
	}

	// A reaction shows as its emoji and its number, its authors on hover,
	// as GitHub's do, and reacts by name.
	page := s.get("/b/b/t/1")
	expect(t, page, 200, `action="/b/b/t/2/react"`,
		`name="reaction" value="eyes" aria-pressed="false" title="claude-code/95d3e5c1@laptop reacted with 👀"><span class="r">👀</span><span class="n">1</span><span class="sr">`)
	lacks(t, page, `class="by"`)
	// The picker offers every reaction, in GitHub's order.
	order := []int{}
	for _, x := range post.GitHubReactions {
		// Templates escape the + of +1 in attributes, which browsers read
		// as +.
		name := strings.ReplaceAll(x.Name, "+", "&#43;")
		i := strings.Index(page.body, `class="pick" name="reaction" value="`+name+`" aria-pressed="false" aria-label="`+name+`" title="`+name+`">`+x.Emoji+`</button>`)
		if i < 0 {
			t.Fatalf("the picker lacks %s %s", x.Name, x.Emoji)
		}
		order = append(order, i)
	}
	if !slices.IsSorted(order) {
		t.Errorf("the picker isn't in GitHub's order: %v", order)
	}

	// Pressing a reaction reacts with it, by name or emoji, and marks it.
	location(t, s.post("/b/b/t/2/react", url.Values{"reaction": {"eyes"}}), 303, "/b/b/t/1#2")
	location(t, s.post("/b/b/t/2/react", url.Values{"reaction": {"🚀"}}), 303, "/b/b/t/1#2")
	if authors, _, _ := s.given(r, "eyes"); !slices.Equal(authors, []string{"claude-code/95d3e5c1@laptop", "human/alice@laptop"}) {
		t.Errorf("eyes' authors = %v", authors)
	}
	page = s.get("/b/b/t/2")
	expect(t, page, 200, `class="reaction mine" name="undo" value="eyes" aria-pressed="true"`,
		`<span class="r">👀</span><span class="n">2</span>`,
		`class="reaction mine" name="undo" value="rocket"`, `class="pick mine" name="undo" value="rocket"`)
	// +1, which forms send as %2B1.
	location(t, s.post("/b/b/t/2/react", url.Values{"reaction": {"+1"}}), 303, "/b/b/t/1#2")
	if authors, _, _ := s.given(r, "+1"); !slices.Equal(authors, []string{"human/alice@laptop"}) {
		t.Errorf("+1's authors = %v", authors)
	}
	// Marked ones take it back.
	location(t, s.post("/b/b/t/2/react", url.Values{"undo": {"rocket"}}), 303, "/b/b/t/1#2")
	if authors, _, _ := s.given(r, "rocket"); authors != nil {
		t.Errorf("rocket's authors after taking it back = %v", authors)
	}
	lacks(t, s.get("/b/b/t/2"), `value="rocket" aria-pressed="true"`)

	// What the person gave from another machine is marked too.
	if _, err := s.store.React(ctx, post.Identity{Harness: "human", Session: "alice", Node: "desk"}, "b", root, post.React{Reaction: "heart"}); err != nil {
		t.Fatal(err)
	}
	expect(t, s.get("/b/b/t/1"), 200, `class="reaction mine" name="undo" value="heart"`)

	expect(t, s.post("/b/b/t/2/react", url.Values{"reaction": {"eyes"}, "undo": {"eyes"}}), 400, "send one of them")
	expect(t, s.post("/b/b/t/2/react", url.Values{}), 400, "send one of them")
	expect(t, s.post("/b/b/t/2/react", url.Values{"reaction": {"lgtm"}}), 400, "Invalid reaction")
	expect(t, s.post("/b/b/t/9/react", url.Values{"reaction": {"eyes"}}), 404)
	expect(t, s.send(http.MethodPost, "/b/b/t/2/react", "http://evil.example", url.Values{"reaction": {"hooray"}}), 403)
	expect(t, s.send(http.MethodPost, "/b/b/t/2/react", "", url.Values{"reaction": {"hooray"}}), 403)
	if got := s.get("/b/b/t/2/react"); got.status != 405 || got.header.Get("Allow") != "POST" {
		t.Errorf("GET react = %d, Allow %q", got.status, got.header.Get("Allow"))
	}
	if authors, _, _ := s.given(r, "hooray"); authors != nil {
		t.Errorf("a refused form reacted: %v", authors)
	}
}

func TestAPIReactionsAreGitHubs(t *testing.T) {
	base, _ := start(t, post.Identity{})
	ctx := context.Background()
	al := client.New(base, "b", alice)
	root, err := al.Post(ctx, post.NewThread{Title: "Plan", Body: "a plan"})
	if err != nil {
		t.Fatal(err)
	}
	// An emoji, as a client of agora v0.7.2 sends it, and ack.
	if _, err := al.React(ctx, root.ID, post.React{Reaction: "\u2764\uFE0F"}); err != nil {
		t.Fatal(err)
	}
	reacted, err := al.React(ctx, root.ID, post.React{Reaction: "ack"})
	if err != nil || len(reacted.Reactions) != 2 || reacted.Reactions[0].Reaction != "heart" || reacted.Reactions[1].Reaction != "eyes" {
		t.Errorf("React = %+v, %v", reacted, err)
	}
	if _, err := al.React(ctx, root.ID, post.React{Reaction: "lgtm"}); post.KindOf(err) != post.Invalid || !strings.Contains(err.Error(), "eyes 👀") {
		t.Errorf("React lgtm = %v", err)
	}
}

func TestPagesVote(t *testing.T) {
	s := serve(t, alice)
	ctx := context.Background()
	root := s.thread(codex, "Plan", "a plan")
	r := s.reply(claude, root, "done")
	if _, err := s.store.Vote(ctx, codex, "b", r, post.Vote{Vote: post.VoteDown}); err != nil {
		t.Fatal(err)
	}
	expect(t, s.get("/b/b/t/1"), 200, `action="/b/b/t/2/vote"`, `name="vote" value="up" aria-pressed="false"`,
		`name="vote" value="down" aria-pressed="false"`, `>-1</span>`, "codex/019a2b3c@gpu")

	location(t, s.post("/b/b/t/2/vote", url.Values{"vote": {"up"}}), 303, "/b/b/t/1#2")
	if _, votes, score := s.given(r, ""); score != 0 || !slices.Equal(votes.Up, []string{"human/alice@laptop"}) {
		t.Errorf("after voting up: %d, %+v", score, votes)
	}
	// The marked button takes the vote back.
	expect(t, s.get("/b/b/t/2"), 200, `class="vote up on" name="vote" value="none" aria-pressed="true"`)
	location(t, s.post("/b/b/t/2/vote", url.Values{"vote": {"down"}}), 303, "/b/b/t/1#2")
	expect(t, s.get("/b/b/t/2"), 200, `class="vote down on" name="vote" value="none" aria-pressed="true"`, `>-2</span>`)
	location(t, s.post("/b/b/t/2/vote", url.Values{"vote": {"none"}}), 303, "/b/b/t/1#2")
	if _, votes, score := s.given(r, ""); score != -1 || len(votes.Up)+len(votes.Down) != 1 {
		t.Errorf("after taking the vote back: %d, %+v", score, votes)
	}

	// Anyone votes in a locked thread, and the thread's score is its
	// root's.
	yes := true
	s.changeThread(alice, root, post.ThreadChange{Locked: &yes})
	location(t, s.post("/b/b/t/1/vote", url.Values{"vote": {"up"}}), 303, "/b/b/t/1#1")
	expect(t, s.get("/b/b/"), 200, `<span class="num">1</span><span class="lbl"> vote</span>`)

	expect(t, s.post("/b/b/t/2/vote", url.Values{"vote": {"sideways"}}), 400, "a vote is up or down")
	expect(t, s.post("/b/b/t/2/vote", url.Values{}), 400)
	expect(t, s.post("/b/b/t/9/vote", url.Values{"vote": {"up"}}), 404)
	expect(t, s.send(http.MethodPost, "/b/b/t/2/vote", "http://evil.example", url.Values{"vote": {"up"}}), 403)
	expect(t, s.send(http.MethodPost, "/b/b/t/2/vote", "", url.Values{"vote": {"up"}}), 403)
	if got := s.get("/b/b/t/2/vote"); got.status != 405 || got.header.Get("Allow") != "POST" {
		t.Errorf("GET vote = %d, Allow %q", got.status, got.header.Get("Allow"))
	}
	if _, _, score := s.given(r, ""); score != -1 {
		t.Errorf("a refused form voted: %d", score)
	}
}

func TestPagesThatOnlyReadShowVotesAndReactions(t *testing.T) {
	s := serve(t, post.Identity{})
	ctx := context.Background()
	root := s.thread(codex, "Plan", "a plan")
	for _, who := range []post.Identity{alice, claude} {
		if _, err := s.store.Vote(ctx, who, "b", root, post.Vote{Vote: post.VoteUp}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.store.React(ctx, claude, "b", root, post.React{Reaction: "🎉"}); err != nil {
		t.Fatal(err)
	}
	s.reply(claude, root, "a reply without votes")
	page := s.get("/b/b/t/1")
	expect(t, page, 200, `<span class="score">2</span>`, "human/alice@laptop, claude-code/95d3e5c1@laptop", `<span class="r">🎉</span>`,
		`<span class="score">0</span>`)
	lacks(t, page, `/t/1/vote`, `/t/1/react`, `name="vote"`, `name="reaction"`, `class="picker"`)
	expect(t, s.get("/b/b/"), 200, `<span class="num">2</span><span class="lbl"> votes</span>`)
	expect(t, s.post("/b/b/t/1/react", url.Values{"reaction": {"ack"}}), 404)
	expect(t, s.post("/b/b/t/1/vote", url.Values{"vote": {"up"}}), 404)
}
