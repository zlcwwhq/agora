package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zlcwwhq/agora/internal/client"
	"github.com/zlcwwhq/agora/internal/post"
	"github.com/zlcwwhq/agora/internal/store"
)

var (
	alice = post.Identity{Harness: "human", Session: "alice", Node: "laptop"}
	codex = post.Identity{Harness: "codex", Session: "019a2b3c-0000", Node: "gpu"}
)

// start serves a board of its own and returns its address.
func start(t *testing.T, person post.Identity) (string, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "board.db"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	srv := httptest.NewServer(New(Options{Board: st, Person: person, Version: "test", Logf: t.Logf}))
	t.Cleanup(srv.Close)
	return srv.URL, st
}

func TestAPIRoundTrip(t *testing.T) {
	base, _ := start(t, post.Identity{})
	ctx := context.Background()
	cx, al := client.New(base, codex), client.New(base, alice)

	root, err := cx.Post(ctx, post.NewThread{Title: "Plan", Tags: []string{"design"}, Body: "first"})
	if err != nil || root.ID != 1 || root.Thread != 1 {
		t.Fatalf("Post = %+v, %v", root, err)
	}
	rep, err := al.Reply(ctx, root.ID, post.NewReply{Body: "see #1"})
	if err != nil || rep.Thread != root.ID {
		t.Fatalf("Reply = %+v, %v", rep, err)
	}
	none := []string{}
	body := "first, edited"
	p, err := al.Edit(ctx, root.ID, post.Edit{Tags: &none, Body: &body})
	if err != nil || len(p.Tags) != 0 || p.Editor != "human/alice@laptop" || p.Body != "first, edited\n" {
		t.Fatalf("Edit = %+v, %v", p, err)
	}
	if _, err := cx.Edit(ctx, rep.ID, post.Edit{Body: &body}); post.KindOf(err) != post.Forbidden || !strings.Contains(err.Error(), "human/alice@laptop's") {
		t.Errorf("Edit of another's post: %v", err)
	}
	show, err := cx.Show(ctx, rep.ID)
	if err != nil || len(show.Posts) != 2 || !slices.Equal(show.Posts[1].Refs, []int64{1}) {
		t.Fatalf("Show = %+v, %v", show, err)
	}
	u, err := cx.Unread(ctx, post.UnreadRequest{})
	if err != nil || u.NewReader || len(u.Posts) != 1 || u.Posts[0].ID != rep.ID {
		t.Errorf("Unread = %+v, %v", u, err)
	}
	ts, err := cx.List(ctx, post.Filter{Limit: 5})
	if err != nil || len(ts) != 1 || ts[0].Replies != 1 {
		t.Errorf("List = %+v, %v", ts, err)
	}
	rs, err := cx.Search(ctx, post.Query{Terms: []string{"EDITED"}, Field: post.FieldBody})
	if err != nil || len(rs) != 1 || rs[0].Matches[0].ID != root.ID {
		t.Errorf("Search = %+v, %v", rs, err)
	}
	removed, err := cx.Delete(ctx, root.ID)
	if err != nil || !slices.Equal(removed.Removed, []int64{root.ID, rep.ID}) {
		t.Errorf("Delete = %+v, %v", removed, err)
	}
	if _, err := cx.Show(ctx, root.ID); post.KindOf(err) != post.NotFound {
		t.Errorf("Show after Delete: %v", err)
	}
	if _, err := cx.List(ctx, post.Filter{Tags: []string{"a b"}}); post.KindOf(err) != post.Invalid {
		t.Errorf("List with an invalid tag: %v", err)
	}
}

func TestAPINonce(t *testing.T) {
	base, _ := start(t, post.Identity{})
	send := func() int {
		req, _ := http.NewRequest("POST", base+"/api/threads", strings.NewReader(`{"title": "T", "body": "b", "nonce": "abc"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Agora-Author", codex.Header())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	if first, second := send(), send(); first != 201 || second != 200 {
		t.Errorf("statuses %d, %d; want 201, 200", first, second)
	}
}

func TestAPIRefusesCrossSiteRequests(t *testing.T) {
	base, st := start(t, post.Identity{})
	if _, _, err := st.Post(context.Background(), codex, post.NewThread{Title: "T", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, method, path, host, ctype, author, body string
		want                                          int
	}{
		{"form post", "POST", "/api/threads", "", "application/x-www-form-urlencoded", codex.Header(), "title=T&body=b", 415},
		{"text post", "POST", "/api/threads", "", "text/plain", codex.Header(), `{"title": "T", "body": "b"}`, 415},
		{"no author", "POST", "/api/threads", "", "application/json", "", `{"title": "T", "body": "b"}`, 400},
		{"bad author", "DELETE", "/api/posts/1", "", "", "codex@gpu", "", 400},
		{"delete without author", "DELETE", "/api/posts/1", "", "", "", "", 400},
		{"rebinding", "GET", "/api/threads", "evil.example:7777", "", "", "", 403},
		{"rebinding page", "GET", "/", "evil.example", "", "", "", 403},
		{"localhost", "GET", "/api/threads", "localhost:7777", "", "", "", 200},
		{"dot localhost", "GET", "/api/tags", "agora.localhost", "", "", "", 200},
		{"ipv6", "GET", "/api/tags", "[::1]:7777", "", "", "", 200},
		{"unknown field", "POST", "/api/threads", "", "application/json", codex.Header(), `{"title": "T", "bdy": "b"}`, 400},
		{"wrong method", "PUT", "/api/threads", "", "application/json", codex.Header(), `{}`, 405},
		{"unknown path", "GET", "/api/nope", "", "", "", "", 404},
		{"bad id", "GET", "/api/posts/01", "", "", "", "", 400},
		{"missing post", "GET", "/api/posts/9", "", "", "", "", 404},
		{"too large", "POST", "/api/threads", "", "application/json", codex.Header(), `{"title": "T", "body": "` + strings.Repeat("x", 1<<20) + `"}`, 413},
		{"empty edit", "PATCH", "/api/posts/1", "", "application/json", codex.Header(), `{}`, 400},
	}
	for _, c := range cases {
		req, err := http.NewRequest(c.method, base+c.path, strings.NewReader(c.body))
		if err != nil {
			t.Fatal(err)
		}
		if c.host != "" {
			req.Host = c.host
		}
		if c.ctype != "" {
			req.Header.Set("Content-Type", c.ctype)
		}
		if c.author != "" {
			req.Header.Set("Agora-Author", c.author)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("%s: status %d, want %d: %s", c.name, resp.StatusCode, c.want, data)
			continue
		}
		if strings.HasPrefix(c.path, "/api/") && c.want >= 400 {
			var e struct{ Error string }
			if json.Unmarshal(data, &e) != nil || e.Error == "" {
				t.Errorf("%s: body %q isn't an API error", c.name, data)
			}
		}
	}
	// Nothing above published.
	ts, err := st.List(context.Background(), post.Filter{})
	if err != nil || len(ts) != 1 {
		t.Errorf("threads after refused requests = %+v, %v", ts, err)
	}
}

func TestUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := srv.URL
	srv.Close()
	_, err := client.New(base, codex).Tags(context.Background())
	if _, ok := err.(*client.UnreachableError); !ok || !strings.Contains(err.Error(), "agora serve") {
		t.Errorf("Tags of a closed server: %T %v", err, err)
	}
}

func TestAllowedHost(t *testing.T) {
	for host, want := range map[string]bool{
		"127.0.0.1:7777": true, "localhost": true, "LOCALHOST:1": true, "x.localhost:80": true,
		"[::1]:7777": true, "100.64.0.1": true, "::1": true,
		"example.com": false, "localhost.example.com": false, "127.0.0.1.nip.io": false, "": false,
	} {
		if got := AllowedHost(host); got != want {
			t.Errorf("AllowedHost(%q) = %v, want %v", host, got, want)
		}
	}
}
