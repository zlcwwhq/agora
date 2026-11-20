package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zlcwwhq/agora/internal/client"
	"github.com/zlcwwhq/agora/internal/post"
	"github.com/zlcwwhq/agora/internal/store"
	"github.com/zlcwwhq/agora/internal/testdir"
)

// rawResponse is what a request of rawAPI got.
type rawResponse struct {
	status int
	header http.Header
	body   []byte
}

// rawAPI sends a request with a body of the type ctype, if it isn't empty,
// as author, if it isn't empty.
func rawAPI(t *testing.T, method, url, ctype, author string, body []byte) rawResponse {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if author != "" {
		req.Header.Set(AuthorHeader, author)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return rawResponse{resp.StatusCode, resp.Header, data}
}

func TestAPIThreads(t *testing.T) {
	base, _ := start(t, post.Identity{})
	ctx := context.Background()
	cx, cl, al := client.New(base, "b", codex), client.New(base, "b", claude), client.New(base, "b", alice)
	root, err := cx.Post(ctx, post.NewThread{Title: "Question", Body: "how?"})
	if err != nil {
		t.Fatal(err)
	}
	rep, err := cl.Reply(ctx, root.ID, post.NewReply{Body: "so"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := cl.Post(ctx, post.NewThread{Title: "Other", Body: "x"})
	if err != nil {
		t.Fatal(err)
	}

	resolved := post.StateResolved
	yes := true
	if _, err := cl.ChangeThread(ctx, rep.ID, post.ThreadChange{State: &resolved}); post.KindOf(err) != post.Forbidden {
		t.Errorf("another author resolves: %v", err)
	}
	th, err := cx.ChangeThread(ctx, rep.ID, post.ThreadChange{State: &resolved})
	if err != nil || th.ID != root.ID || th.State != post.StateResolved || th.Pinned {
		t.Fatalf("ChangeThread = %+v, %v", th, err)
	}
	if _, err := cx.ChangeThread(ctx, root.ID, post.ThreadChange{Pinned: &yes}); post.KindOf(err) != post.Forbidden {
		t.Errorf("an agent pins: %v", err)
	}
	th, err = al.ChangeThread(ctx, root.ID, post.ThreadChange{Pinned: &yes, Locked: &yes})
	if err != nil || !th.Pinned || !th.Locked || th.State != post.StateResolved {
		t.Fatalf("a person's ChangeThread = %+v, %v", th, err)
	}

	// A locked thread refuses agents, but not people.
	if _, err := cl.Reply(ctx, rep.ID, post.NewReply{Body: "more"}); post.KindOf(err) != post.Forbidden || !strings.Contains(err.Error(), "locked") {
		t.Errorf("a reply in a locked thread: %v", err)
	}
	if _, err := cl.Attach(ctx, rep.ID, "x.txt", "text/plain", []byte("x")); post.KindOf(err) != post.Forbidden {
		t.Errorf("an attachment in a locked thread: %v", err)
	}
	if _, err := al.Reply(ctx, rep.ID, post.NewReply{Body: "a person's"}); err != nil {
		t.Errorf("a person's reply in a locked thread: %v", err)
	}

	// Pinned first in lists, but not in searches; filtered by state; in
	// pages.
	ts, err := cx.List(ctx, post.Filter{})
	if err != nil || len(ts) != 2 || ts[0].ID != root.ID || ts[1].ID != other.ID {
		t.Errorf("List = %+v, %v", ts, err)
	}
	ts, err = cx.List(ctx, post.Filter{State: post.StateOpen})
	if err != nil || len(ts) != 1 || ts[0].ID != other.ID {
		t.Errorf("List open = %+v, %v", ts, err)
	}
	ts, err = cx.List(ctx, post.Filter{Skip: 1, Limit: 1})
	if err != nil || len(ts) != 1 || ts[0].ID != other.ID {
		t.Errorf("List skip 1 = %+v, %v", ts, err)
	}
	rs, err := cx.Search(ctx, post.Query{Terms: []string{"x"}, Filter: post.Filter{State: post.StateResolved}})
	if err != nil || len(rs) != 0 {
		t.Errorf("Search resolved = %+v, %v", rs, err)
	}

	for _, c := range []struct {
		name, method, path, ctype, author, body string
		want                                    int
	}{
		{"empty change", "PATCH", "/api/boards/b/threads/1", "application/json", codex.Header(), `{}`, 400},
		{"invalid state", "PATCH", "/api/boards/b/threads/1", "application/json", alice.Header(), `{"state": "closed"}`, 400},
		{"unknown field", "PATCH", "/api/boards/b/threads/1", "application/json", alice.Header(), `{"sticky": true}`, 400},
		{"change without author", "PATCH", "/api/boards/b/threads/1", "application/json", "", `{"pinned": true}`, 400},
		{"change as a form", "PATCH", "/api/boards/b/threads/1", "application/x-www-form-urlencoded", alice.Header(), `pinned=true`, 415},
		{"change a missing post", "PATCH", "/api/boards/b/threads/9", "application/json", alice.Header(), `{"pinned": true}`, 404},
		{"get a thread", "GET", "/api/boards/b/threads/1", "", "", "", 405},
		{"invalid skip", "GET", "/api/boards/b/threads?skip=-1", "", "", "", 400},
		{"invalid state filter", "GET", "/api/boards/b/threads?state=closed", "", "", "", 400},
		{"search with an invalid skip", "GET", "/api/boards/b/search?q=x&skip=x", "", "", "", 400},
		{"skip past the end", "GET", "/api/boards/b/threads?skip=5", "", "", "", 200},
	} {
		r := rawAPI(t, c.method, base+c.path, c.ctype, c.author, []byte(c.body))
		if r.status != c.want {
			t.Errorf("%s: status %d, want %d: %s", c.name, r.status, c.want, r.body)
		}
	}
	if r := rawAPI(t, "GET", base+"/api/boards/b/threads/1", "", "", nil); r.header.Get("Allow") != "PATCH" {
		t.Errorf("Allow = %q", r.header.Get("Allow"))
	}
}

func TestAPIAttachments(t *testing.T) {
	base, _ := start(t, post.Identity{})
	ctx := context.Background()
	cx, cl := client.New(base, "b", codex), client.New(base, "b", claude)
	root, err := cx.Post(ctx, post.NewThread{Title: "Logs", Body: "attached"})
	if err != nil {
		t.Fatal(err)
	}
	url := base + "/api/boards/b/posts/1/attachments/"

	// More than a JSON body may have, with its media type.
	log := bytes.Repeat([]byte("log line\n"), 200_000)
	r := rawAPI(t, "PUT", url+"build.log", "text/plain; charset=utf-8", codex.Header(), log)
	if r.status != 201 || !strings.Contains(string(r.body), `"type":"text/plain; charset=utf-8"`) || !strings.Contains(string(r.body), `"size":1800000`) {
		t.Fatalf("PUT = %d %s", r.status, r.body)
	}
	if r := rawAPI(t, "PUT", url+"build.log", "text/plain; charset=utf-8", codex.Header(), []byte("short\n")); r.status != 200 {
		t.Errorf("PUT replacing = %d %s", r.status, r.body)
	}
	if r := rawAPI(t, "PUT", url+"blob", "", codex.Header(), []byte{1, 2}); r.status != 201 || !strings.Contains(string(r.body), `"application/octet-stream"`) {
		t.Errorf("PUT without a type = %d %s", r.status, r.body)
	}
	a, err := cx.Attach(ctx, root.ID, "截图 #1.html", "text/html", []byte("<script>alert(1)</script>"))
	if err != nil || a.Name != "截图 #1.html" || a.Type != "text/html" {
		t.Errorf("Attach = %+v, %v", a, err)
	}

	r = rawAPI(t, "GET", url+"build.log", "", "", nil)
	if r.status != 200 || string(r.body) != "short\n" || r.header.Get("Content-Type") != "text/plain; charset=utf-8" ||
		r.header.Get("X-Content-Type-Options") != "nosniff" || r.header.Get("Content-Security-Policy") != "sandbox" {
		t.Errorf("GET = %d %v %q", r.status, r.header, r.body)
	}
	var buf bytes.Buffer
	if n, err := cx.Download(ctx, root.ID, "截图 #1.html", &buf); err != nil || n != 25 || buf.String() != "<script>alert(1)</script>" {
		t.Errorf("Download = %d, %q, %v", n, buf.String(), err)
	}
	show, err := cx.Show(ctx, root.ID)
	if err != nil || len(show.Posts[0].Attachments) != 3 || show.Posts[0].Attachments[0].Name != "blob" ||
		show.Posts[0].Attachments[1] != (post.Attachment{Name: "build.log", Type: "text/plain; charset=utf-8", Size: 6}) {
		t.Errorf("Show = %+v, %v", show.Posts, err)
	}
	if show.Posts[0].Edited != "" {
		t.Errorf("attaching edited the post")
	}

	if _, err := cl.Attach(ctx, root.ID, "x", "text/plain", []byte("x")); post.KindOf(err) != post.Forbidden {
		t.Errorf("another author attaches: %v", err)
	}
	if _, err := cl.Detach(ctx, root.ID, "blob"); post.KindOf(err) != post.Forbidden {
		t.Errorf("another author detaches: %v", err)
	}
	a, err = cx.Detach(ctx, root.ID, "blob")
	if err != nil || a != (post.Attachment{Name: "blob", Type: "application/octet-stream", Size: 2}) {
		t.Errorf("Detach = %+v, %v", a, err)
	}
	if _, err := cx.Download(ctx, root.ID, "blob", io.Discard); post.KindOf(err) != post.NotFound {
		t.Errorf("Download of a detached one: %v", err)
	}

	for _, c := range []struct {
		name, method, path, ctype, author string
		body                              []byte
		want                              int
	}{
		{"too large", "PUT", "/1/attachments/big", "", codex.Header(), make([]byte, post.MaxAttachment+1), 413},
		{"without author", "PUT", "/1/attachments/x", "", "", []byte("x"), 400},
		{"invalid name", "PUT", "/1/attachments/a%2Fb", "", codex.Header(), []byte("x"), 400},
		{"control character", "GET", "/1/attachments/a%0Ab", "", "", nil, 400},
		{"invalid type", "PUT", "/1/attachments/x", "text/", codex.Header(), []byte("x"), 400},
		{"missing post", "GET", "/9/attachments/x", "", "", nil, 404},
		{"missing attachment", "GET", "/1/attachments/nope", "", "", nil, 404},
		{"detach a missing one", "DELETE", "/1/attachments/nope", "", codex.Header(), nil, 404},
		{"post to it", "POST", "/1/attachments/x", "application/json", codex.Header(), []byte("{}"), 405},
	} {
		r := rawAPI(t, c.method, base+"/api/boards/b/posts"+c.path, c.ctype, c.author, c.body)
		if r.status != c.want {
			t.Errorf("%s: status %d, want %d: %.200s", c.name, r.status, c.want, r.body)
		}
	}
	if r := rawAPI(t, "PUT", url+"max", "", codex.Header(), make([]byte, post.MaxAttachment)); r.status != 201 {
		t.Errorf("an attachment of 10 MiB: %d %s", r.status, r.body)
	}
	// Bodies other than attachments keep their bound.
	if r := rawAPI(t, "PATCH", base+"/api/boards/b/posts/1", "application/json", codex.Header(), []byte(`{"body": "`+strings.Repeat("x", 1<<20)+`"}`)); r.status != 413 ||
		!strings.Contains(string(r.body), "larger than 1 MiB") {
		t.Errorf("a large edit: %d %s", r.status, r.body)
	}
}

func TestAPIMove(t *testing.T) {
	base, _ := start(t, post.Identity{})
	ctx := context.Background()
	cx, al := client.New(base, "b", codex), client.New(base, "b", alice)
	a, _ := cx.Post(ctx, post.NewThread{Title: "A", Body: "a"})
	r2, _ := cx.Reply(ctx, a.ID, post.NewReply{Body: "two"})
	r3, _ := cx.Reply(ctx, r2.ID, post.NewReply{Body: "three"})
	b, _ := cx.Post(ctx, post.NewThread{Title: "B", Body: "b"})

	if _, err := cx.Move(ctx, r2.ID, post.Move{To: b.ID}); post.KindOf(err) != post.Forbidden {
		t.Errorf("an agent moves: %v", err)
	}
	moved, err := al.Move(ctx, r2.ID, post.Move{To: b.ID})
	if err != nil || moved.Thread != b.ID || !slices.Equal(moved.Moved, []int64{r2.ID, r3.ID}) {
		t.Fatalf("Move = %+v, %v", moved, err)
	}
	title := "C"
	moved, err = al.Move(ctx, r3.ID, post.Move{Title: &title})
	if err != nil || moved.Thread != r3.ID || !slices.Equal(moved.Moved, []int64{r3.ID}) {
		t.Fatalf("Move to a new thread = %+v, %v", moved, err)
	}
	for _, c := range []struct{ name, body string }{
		{"both", `{"to": 4, "title": "x"}`},
		{"neither", `{}`},
		{"to itself", `{"to": 2}`},
		{"unknown field", `{"into": 4}`},
	} {
		if r := rawAPI(t, "POST", base+"/api/boards/b/posts/2/move", "application/json", alice.Header(), []byte(c.body)); r.status != 400 {
			t.Errorf("move %s: %d %s", c.name, r.status, r.body)
		}
	}
	if r := rawAPI(t, "POST", base+"/api/boards/b/posts/2/move", "application/json", alice.Header(), []byte(`{"to": 9}`)); r.status != 404 {
		t.Errorf("move to a missing post: %d %s", r.status, r.body)
	}
}

func TestAPIBackup(t *testing.T) {
	base, _ := start(t, post.Identity{})
	ctx := context.Background()
	cx := client.New(base, "b", codex)
	if _, err := cx.Post(ctx, post.NewThread{Title: "Saved", Body: "x"}); err != nil {
		t.Fatal(err)
	}
	if r := rawAPI(t, "GET", base+"/api/backup", "", "", nil); r.status != 400 {
		t.Errorf("a backup without an author: %d", r.status)
	}
	if r := rawAPI(t, "POST", base+"/api/backup", "application/json", codex.Header(), []byte("{}")); r.status != 405 || r.header.Get("Allow") != "GET, HEAD" {
		t.Errorf("POST /api/backup: %d %v", r.status, r.header)
	}
	r := rawAPI(t, "GET", base+"/api/backup", "", codex.Header(), nil)
	if r.status != 200 || r.header.Get("Content-Type") != "application/vnd.sqlite3" {
		t.Fatalf("GET /api/backup = %d %v", r.status, r.header)
	}
	path := filepath.Join(testdir.New(t), "copy.db")
	var buf bytes.Buffer
	n, err := client.New(base, "", codex).Backup(ctx, &buf)
	if err != nil || n != int64(buf.Len()) || n != int64(len(r.body)) {
		t.Fatalf("Backup = %d, %v", n, err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(path, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if ts, err := st.List(ctx, "b", post.Filter{}); err != nil || len(ts) != 1 || ts[0].Title != "Saved" {
		t.Errorf("the backup's threads = %+v, %v", ts, err)
	}
}
