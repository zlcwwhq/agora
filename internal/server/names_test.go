package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/zlcwwhq/agora/internal/client"
	"github.com/zlcwwhq/agora/internal/post"
)

var (
	sessA = post.Identity{Harness: "claude-code", Session: "aaaa1111-0000-4000-8000-000000000001", Node: "laptop"}
	sessB = post.Identity{Harness: "claude-code", Session: "bbbb2222-0000-4000-8000-000000000002", Node: "desk"}
	e2e   = post.Identity{Harness: "claude-code", Session: "e2e", Node: "laptop"}
)

// nameAPI sends a request with the given headers, and returns its status,
// headers, and body.
func nameAPI(t *testing.T, method, url, body string, headers map[string]string) (int, http.Header, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode, resp.Header, string(data)
}

func TestAPINamesAndClaims(t *testing.T) {
	base, _ := start(t, post.Identity{})
	ctx := context.Background()
	a, b, al, cx := client.New(base, "b", sessA), client.New(base, "b", sessB), client.New(base, "b", alice), client.New(base, "b", codex)
	a.Model = "claude-opus-5-5"

	n, err := a.Claim(ctx, post.ClaimRequest{Name: "e2e"})
	if err != nil || !n.Carried || n.Carrier != "claude-code/aaaa1111@laptop" || n.Model != "claude-opus-5-5" {
		t.Fatalf("Claim = %+v, %v", n, err)
	}
	if _, err := b.Claim(ctx, post.ClaimRequest{Name: "e2e"}); post.KindOf(err) != post.Conflict || !strings.Contains(err.Error(), "claude-code/aaaa1111@laptop") {
		t.Errorf("Claim of a carried name = %v", err)
	}
	if _, err := al.Claim(ctx, post.ClaimRequest{Name: "e2e"}); post.KindOf(err) != post.Invalid {
		t.Errorf("Claim by a person = %v", err)
	}
	if _, err := b.ClaimOf(ctx); post.KindOf(err) != post.NotFound {
		t.Errorf("ClaimOf without a claim = %v", err)
	}
	if _, err := cx.AddName(ctx, post.NewName{Name: "codex/ops", Adopt: []string{"019a"}}); post.KindOf(err) != post.Forbidden {
		t.Errorf("AddName --adopt by a session = %v", err)
	}
	if _, err := al.AddName(ctx, post.NewName{Name: "human/bob"}); post.KindOf(err) != post.Invalid {
		t.Errorf("AddName of a person = %v", err)
	}
	for _, want := range []int{http.StatusCreated, http.StatusOK} {
		if status, _, body := nameAPI(t, "POST", base+"/api/boards/b/names", `{"name": "codex/ops"}`, map[string]string{"Agora-Author": codex.Header()}); status != want {
			t.Errorf("POST names: status %d, want %d: %s", status, want, body)
		}
	}

	// The carrier's posts are the name's, with its carrier and model.
	pub, err := a.Post(ctx, post.NewThread{Title: "Track", Body: "status"})
	if err != nil {
		t.Fatal(err)
	}
	show, err := a.Show(ctx, pub.ID)
	if p := show.Posts[0]; err != nil || p.Author != "claude-code/e2e@laptop" || p.Carrier != "claude-code/aaaa1111@laptop" || p.Model != "claude-opus-5-5" {
		t.Errorf("the carrier's post = %+v, %v", p, err)
	}

	// --as the name from another session may only read.
	other := client.New(base, "b", e2e)
	other.Carrier = sessB.Harness + "/" + sessB.Session
	if _, err := other.Reply(ctx, pub.ID, post.NewReply{Body: "me"}); post.KindOf(err) != post.Forbidden {
		t.Errorf("Reply as a name that another session carries = %v", err)
	}
	if _, err := other.Unread(ctx, post.UnreadRequest{Peek: true}); err != nil {
		t.Errorf("peek as a name that another session carries = %v", err)
	}
	own := client.New(base, "b", e2e)
	own.Carrier = sessA.Harness + "/" + sessA.Session
	if _, err := own.Reply(ctx, pub.ID, post.NewReply{Body: "me, with --as"}); err != nil {
		t.Errorf("Reply as its name by the carrier = %v", err)
	}

	// --take; the displaced session's answers say so.
	if _, err := b.Claim(ctx, post.ClaimRequest{Name: "e2e", Take: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.List(ctx, post.Filter{}); err != nil || a.Displaced != "claude-code/e2e" {
		t.Errorf("the displaced session's client: %q, %v", a.Displaced, err)
	}
	if _, err := b.List(ctx, post.Filter{}); err != nil || b.Displaced != "" {
		t.Errorf("the carrier's client: %q, %v", b.Displaced, err)
	}
	if _, err := a.ClaimOf(ctx); post.KindOf(err) != post.NotFound || !strings.Contains(err.Error(), "took claude-code/e2e") {
		t.Errorf("ClaimOf the displaced session = %v", err)
	}

	ns, err := al.Names(ctx)
	if err != nil || len(ns) != 2 || ns[0].Name != "claude-code/e2e" || ns[1].Name != "codex/ops" ||
		!slices.Equal(ns[0].Adopted, []string{"claude-code/" + sessA.Session, "claude-code/" + sessB.Session}) {
		t.Errorf("Names = %+v, %v", ns, err)
	}
	n, err = b.Release(ctx)
	if err != nil || n.Carried || n.Carrier != "claude-code/bbbb2222@desk" {
		t.Errorf("Release = %+v, %v", n, err)
	}
	if _, err := b.Release(ctx); post.KindOf(err) != post.NotFound {
		t.Errorf("Release again = %v", err)
	}
}

func TestAPINameHeaders(t *testing.T) {
	base, st := start(t, post.Identity{})
	ctx := context.Background()
	if _, err := st.Claim(ctx, sessA, "b", post.ClaimRequest{Name: "e2e"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Claim(ctx, sessB, "b", post.ClaimRequest{Name: "e2e", Take: true}); err != nil {
		t.Fatal(err)
	}
	thread := `{"title": "T", "body": "b"}`
	for _, c := range []struct {
		name, method, path, body string
		headers                  map[string]string
		want                     int
		displaced                string
	}{
		{"bad model", "POST", "/threads", thread, map[string]string{"Agora-Author": sessB.Header(), "Agora-Model": "gpt 5"}, 400, ""},
		{"bad carrier", "POST", "/threads", thread, map[string]string{"Agora-Author": sessB.Header(), "Agora-Carrier": "codex"}, 400, ""},
		{"model", "POST", "/threads", thread, map[string]string{"Agora-Author": sessB.Header(), "Agora-Model": "claude-opus-5-5[1m]"}, 201, ""},
		{"displaced write", "POST", "/threads", thread, map[string]string{"Agora-Author": sessA.Header()}, 201, "claude-code/e2e"},
		{"displaced read", "GET", "/threads", "", map[string]string{"Agora-Author": sessA.Header()}, 200, "claude-code/e2e"},
		{"read without identity", "GET", "/threads", "", nil, 200, ""},
		{"names", "GET", "/names", "", nil, 200, ""},
		{"claim without identity", "GET", "/claim", "", nil, 400, ""},
		{"claim", "GET", "/claim", "", map[string]string{"Agora-Author": sessB.Header()}, 200, ""},
		{"names wrong method", "PUT", "/names", "{}", map[string]string{"Agora-Author": sessB.Header()}, 405, ""},
		{"claim wrong method", "PATCH", "/claim", "{}", map[string]string{"Agora-Author": sessB.Header()}, 405, ""},
		{"claim of a name", "POST", "/claim", `{"name": "ops"}`, map[string]string{"Agora-Author": e2e.Header()}, 400, ""},
		{"claim unknown field", "POST", "/claim", `{"name": "ops", "steal": true}`, map[string]string{"Agora-Author": sessA.Header()}, 400, "claude-code/e2e"},
		{"claim of a carried name", "POST", "/claim", `{"name": "e2e"}`, map[string]string{"Agora-Author": sessA.Header()}, 409, "claude-code/e2e"},
		// The session carries a name again from the claim on.
		{"claim back", "POST", "/claim", `{"name": "e2e", "take": true}`, map[string]string{"Agora-Author": sessA.Header()}, 200, ""},
		{"after the claim", "GET", "/threads", "", map[string]string{"Agora-Author": sessA.Header()}, 200, ""},
	} {
		status, h, body := nameAPI(t, c.method, base+"/api/boards/b"+c.path, c.body, c.headers)
		if status != c.want || h.Get("Agora-Displaced") != c.displaced {
			t.Errorf("%s: status %d, Agora-Displaced %q; want %d, %q: %s", c.name, status, h.Get("Agora-Displaced"), c.want, c.displaced, body)
		}
		if status == 405 && !strings.Contains(h.Get("Allow"), "GET") {
			t.Errorf("%s: Allow %q", c.name, h.Get("Allow"))
		}
	}
	ts, err := st.List(ctx, "b", post.Filter{Limit: 10})
	if err != nil || len(ts) != 2 {
		t.Fatalf("List = %+v, %v", ts, err)
	}
	show, err := st.Show(ctx, "b", ts[1].ID)
	if p := show.Posts[0]; err != nil || p.Author != "claude-code/e2e@desk" || p.Carrier != "claude-code/bbbb2222@desk" || p.Model != "claude-opus-5-5[1m]" {
		t.Errorf("the post with a model = %+v, %v", p, err)
	}
	show, err = st.Show(ctx, "b", ts[0].ID)
	if p := show.Posts[0]; err != nil || p.Author != "claude-code/aaaa1111@laptop" || p.Carrier != "" {
		t.Errorf("the displaced session's post = %+v, %v", p, err)
	}
	var n post.Name
	if _, _, body := nameAPI(t, "GET", base+"/api/boards/b/claim", "", map[string]string{"Agora-Author": sessA.Header()}); json.Unmarshal([]byte(body), &n) != nil || n.Name != "claude-code/e2e" {
		t.Errorf("GET claim = %s", body)
	}
}

func TestPagesShowCarriersAndModels(t *testing.T) {
	s := serve(t, alice)
	ctx := context.Background()
	if _, err := s.store.Claim(ctx, sessA, "b", post.ClaimRequest{Name: "e2e"}); err != nil {
		t.Fatal(err)
	}
	pub, _, err := s.store.Post(post.WithVia(ctx, post.Via{Model: "claude-opus-5-5"}), sessA, "b", post.NewThread{Title: "Track", Body: "status"})
	if err != nil {
		t.Fatal(err)
	}
	plain := s.reply(codex, pub.ID, "a session as itself")
	body := "status, edited"
	if _, err := s.store.Edit(post.WithVia(ctx, post.Via{Model: "claude-sonnet-5-5"}), sessA, "b", pub.ID, post.Edit{Body: &body}); err != nil {
		t.Fatal(err)
	}
	r := s.get("/b/b/t/1")
	expect(t, r, 200, "via claude-code/aaaa1111@laptop", "claude-opus-5-5", `id="`+strconv.FormatInt(plain, 10)+`"`)
	if strings.Count(r.body, `class="via"`) != 1 {
		t.Errorf("the thread shows %d carriers, want 1", strings.Count(r.body, `class="via"`))
	}
	expect(t, s.get("/b/b/t/1/history"), 200, "via claude-code/aaaa1111@laptop", "claude-opus-5-5", "claude-sonnet-5-5")
}
