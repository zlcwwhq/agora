package server

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/zlcwwhq/agora/internal/client"
	"github.com/zlcwwhq/agora/internal/post"
)

// Icons' first bytes, followed by anything.
var (
	pngIcon = []byte("\x89PNG\r\n\x1a\nrest of a png")
	gifIcon = []byte("GIF89arest of a gif")
)

func TestAPIIcons(t *testing.T) {
	base, _ := start(t, post.Identity{})
	ctx := context.Background()
	url := base + "/api/boards/b/icons"

	if r := rawAPI(t, "GET", url, "", "", nil); r.status != 200 || strings.TrimSpace(string(r.body)) != "[]" {
		t.Errorf("GET icons of a new board = %d %s", r.status, r.body)
	}
	if r := rawAPI(t, "PUT", url+"/codex", "image/png", codex.Header(), pngIcon); r.status != 403 || !strings.Contains(string(r.body), "only a person") {
		t.Errorf("an agent's PUT = %d %s", r.status, r.body)
	}
	r := rawAPI(t, "PUT", url+"/codex", "image/png", alice.Header(), pngIcon)
	if r.status != 201 || !strings.Contains(string(r.body), `{"board":"b","harness":"codex","type":"image/png","size":21}`) {
		t.Fatalf("PUT = %d %s", r.status, r.body)
	}
	if r := rawAPI(t, "PUT", url+"/codex", "image/gif; name=x", alice.Header(), gifIcon); r.status != 200 || !strings.Contains(string(r.body), `"type":"image/gif"`) {
		t.Errorf("PUT replacing = %d %s", r.status, r.body)
	}
	for _, c := range []struct {
		name, harness, ctype string
		body                 []byte
		status               int
	}{
		{"another type", "codex", "image/jpeg", pngIcon, 415},
		{"not an image type", "codex", "text/plain", pngIcon, 415},
		{"JSON", "codex", "application/json", []byte(`{}`), 415},
		{"no type", "codex", "", pngIcon, 415},
		{"not an image", "codex", "image/png", []byte("<svg/>"), 415},
		{"empty", "codex", "image/png", nil, 400},
		{"invalid harness", "Codex", "image/png", pngIcon, 400},
		{"too large", "codex", "image/png", append(bytes.Clone(pngIcon), make([]byte, post.MaxIcon)...), 413},
	} {
		if r := rawAPI(t, "PUT", url+"/"+c.harness, c.ctype, alice.Header(), c.body); r.status != c.status {
			t.Errorf("%s: PUT = %d %s, want %d", c.name, r.status, r.body, c.status)
		}
	}
	if r := rawAPI(t, "PUT", base+"/api/boards/nope/icons/codex", "image/png", alice.Header(), pngIcon); r.status != 404 {
		t.Errorf("PUT on no board = %d %s", r.status, r.body)
	}

	r = rawAPI(t, "GET", url+"/codex", "", "", nil)
	if r.status != 200 || !bytes.Equal(r.body, gifIcon) || r.header.Get("Content-Type") != "image/gif" ||
		r.header.Get("X-Content-Type-Options") != "nosniff" || r.header.Get("Content-Security-Policy") != "sandbox" {
		t.Errorf("GET = %d %v %q", r.status, r.header, r.body)
	}
	for path, status := range map[string]int{"/human": 404, "/Bad": 400} {
		if r := rawAPI(t, "GET", url+path, "", "", nil); r.status != status {
			t.Errorf("GET %s = %d %s, want %d", path, r.status, r.body, status)
		}
	}
	if r := rawAPI(t, "POST", url+"/codex", "image/png", alice.Header(), pngIcon); r.status != 405 || r.header.Get("Allow") != "GET, HEAD, PUT, DELETE" {
		t.Errorf("POST = %d %v", r.status, r.header)
	}
	if r := rawAPI(t, "DELETE", url+"/codex", "", codex.Header(), nil); r.status != 403 {
		t.Errorf("an agent's DELETE = %d %s", r.status, r.body)
	}
	if r := rawAPI(t, "DELETE", url+"/codex", "", alice.Header(), nil); r.status != 200 || !strings.Contains(string(r.body), `"type":"image/gif","size":19`) {
		t.Errorf("DELETE = %d %s", r.status, r.body)
	}
	if r := rawAPI(t, "DELETE", url+"/codex", "", alice.Header(), nil); r.status != 404 {
		t.Errorf("DELETE again = %d %s", r.status, r.body)
	}

	// The client's calls.
	cl := client.New(base, "b", alice)
	if i, err := cl.SetIcon(ctx, "human", "image/png", pngIcon); err != nil || i != (post.Icon{Board: "b", Harness: "human", Type: "image/png", Size: 21}) {
		t.Errorf("SetIcon = %+v, %v", i, err)
	}
	if is, err := cl.Icons(ctx); err != nil || len(is) != 1 || is[0].Harness != "human" {
		t.Errorf("Icons = %+v, %v", is, err)
	}
	if _, err := client.New(base, "b", codex).RemoveIcon(ctx, "human"); post.KindOf(err) != post.Forbidden {
		t.Errorf("an agent's RemoveIcon = %v", err)
	}
	if i, err := cl.RemoveIcon(ctx, "human"); err != nil || i.Harness != "human" {
		t.Errorf("RemoveIcon = %+v, %v", i, err)
	}
}

// postIcon sends the form that sets an icon, as a browser does, with the
// file name holding data unless name is empty.
func (s *site) postIcon(harness, name string, data []byte) response {
	s.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("harness", harness); err != nil {
		s.t.Fatal(err)
	}
	w, err := mw.CreateFormFile("file", name)
	if err != nil {
		s.t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		s.t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		s.t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, s.base+"/b/b/icons", &body)
	if err != nil {
		s.t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Origin", s.base)
	return s.do(req)
}

func (s *site) setIcon(board, harness string, data []byte) {
	s.t.Helper()
	if _, _, err := s.store.SetIcon(context.Background(), alice, board, harness, data); err != nil {
		s.t.Fatal(err)
	}
}

func TestPagesShowTheBoardsIcons(t *testing.T) {
	s := serve(t, alice)
	s.board("other", "Other")
	s.thread(codex, "Plan", "see")
	s.reply(claude, 1, "ok")
	s.threadOn("other", codex, "Elsewhere", "x")
	s.setIcon("b", "codex", pngIcon)
	s.setIcon("b", "human", gifIcon)

	own := func(board, harness string) string {
		return `<img class="hicon own" src="/b/` + board + `/i/` + harness + `" width="24" height="24" alt="` + harness + `" title="` + harness + `">`
	}
	drawn := func(harness string) string { return `aria-label="` + harness + `"` }

	// The board's pages show its icons; the rest stay drawn.
	for _, path := range []string{"/b/b/t/1", "/b/b/"} {
		r := s.get(path)
		expect(t, r, 200, own("b", "codex"), own("b", "human"), drawn("claude-code"))
		lacks(t, r, drawn("codex"), drawn("human"))
	}
	expect(t, s.get("/b/b/new"), 200, own("b", "human"))
	// Other boards don't show them, and / shows each board's own for its
	// creator, and the drawn one for the person posting.
	r := s.get("/b/other/t/1")
	expect(t, r, 200, drawn("codex"), drawn("human"))
	lacks(t, r, "/i/")
	r = s.get("/")
	expect(t, r, 200, own("b", "human"), drawn("human"))
	if strings.Count(r.body, "<img") != 1 {
		t.Errorf("/ shows %d custom icons, want 1, of board b's creator", strings.Count(r.body, "<img"))
	}

	// An icon's content, sent as attachments are.
	r = s.get("/b/b/i/codex")
	etag := r.header.Get("ETag")
	if r.status != 200 || r.body != string(pngIcon) || r.header.Get("Content-Type") != "image/png" || etag == "" ||
		r.header.Get("Content-Security-Policy") != "sandbox" || r.header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("icon = %d %v %q", r.status, r.header, r.body)
	}
	req, _ := http.NewRequest(http.MethodGet, s.base+"/b/b/i/codex?v=1", nil)
	req.Header.Set("If-None-Match", etag)
	if r := s.do(req); r.status != 304 || r.body != "" {
		t.Errorf("icon again = %d %q", r.status, r.body)
	}
	s.setIcon("b", "codex", gifIcon)
	req, _ = http.NewRequest(http.MethodGet, s.base+"/b/b/i/codex", nil)
	req.Header.Set("If-None-Match", etag)
	if r := s.do(req); r.status != 200 || r.body != string(gifIcon) || r.header.Get("Content-Type") != "image/gif" {
		t.Errorf("changed icon = %d %q %v", r.status, r.body, r.header)
	}
	for path, status := range map[string]int{"/b/b/i/claude-code": 404, "/b/b/i/Bad": 400, "/b/nope/i/codex": 404, "/b/other/i/codex": 404} {
		r := s.get(path)
		if r.status != status || r.header.Get("Content-Security-Policy") != "sandbox" {
			t.Errorf("%s: %d %v, want %d", path, r.status, r.header, status)
		}
	}
}

func TestIconsPageAndForms(t *testing.T) {
	s := serve(t, alice)
	s.thread(codex, "Plan", "see")
	s.reply(claude, 1, "ok")
	s.setIcon("b", "codex", pngIcon)

	r := s.get("/b/b/icons")
	expect(t, r, 200, `href="/b/b/icons" aria-current="page"`, `<span class="name">claude-code</span>`, "drawn by agora",
		`<span class="name">codex</span>`, "image/png, 21 B", `action="/b/b/icons/remove"`, `name="harness" value="codex"`,
		`action="/b/b/icons" method="post" enctype="multipart/form-data"`, `name="file"`)
	if strings.Index(r.body, ">claude-code<") > strings.Index(r.body, ">codex<") {
		t.Errorf("harnesses aren't by name")
	}
	lacks(t, r, `name="harness" value="claude-code"`)
	// Other pages of the board link to it.
	expect(t, s.get("/b/b/"), 200, `href="/b/b/icons"`)

	// Setting an icon, for any harness.
	location(t, s.postIcon("my-agent", "logo.jpg", gifIcon), 303, "/b/b/icons")
	if _, data, err := s.store.Icon(context.Background(), "b", "my-agent"); err != nil || !bytes.Equal(data, gifIcon) {
		t.Errorf("the form's icon = %q, %v", data, err)
	}
	expect(t, s.get("/b/b/icons"), 200, `<span class="name">my-agent</span>`, "image/gif")
	for _, c := range []struct {
		harness, name string
		data          []byte
		says          string
	}{
		{"Bad Name", "a.png", pngIcon, "Invalid harness"},
		{"codex", "a.svg", []byte("<svg/>"), "isn&#39;t a PNG, JPEG, GIF, or WebP image"},
		{"codex", "", nil, "Choose an image for the icon."},
	} {
		r := s.postIcon(c.harness, c.name, c.data)
		expect(t, r, 400, c.says, `value="`+c.harness+`"`)
	}
	// A file larger than a form's files may be is refused as theirs are.
	expect(t, s.postIcon("codex", "huge.png", append(bytes.Clone(pngIcon), make([]byte, post.MaxAttachment)...)), 413, "larger than 10 MiB")
	if _, data, _ := s.store.Icon(context.Background(), "b", "codex"); !bytes.Equal(data, pngIcon) {
		t.Errorf("a refused form changed the icon: %q", data)
	}

	// Removing one.
	location(t, s.post("/b/b/icons/remove", url.Values{"harness": {"codex"}}), 303, "/b/b/icons")
	if _, _, err := s.store.Icon(context.Background(), "b", "codex"); post.KindOf(err) != post.NotFound {
		t.Errorf("removed icon = %v", err)
	}
	expect(t, s.post("/b/b/icons/remove", url.Values{"harness": {"codex"}}), 404, "no icon for codex")
	expect(t, s.post("/b/b/icons/remove", url.Values{"harness": {"Bad"}}), 400, "Invalid harness")
	if r := s.send(http.MethodPost, "/b/b/icons/remove", "", url.Values{"harness": {"my-agent"}}); r.status != 403 {
		t.Errorf("a form from elsewhere = %d", r.status)
	}
	if r := s.get("/b/b/icons/remove"); r.status != 405 {
		t.Errorf("GET of the remove form = %d", r.status)
	}
}

func TestIconsPageOnlyReads(t *testing.T) {
	s := serve(t, post.Identity{})
	s.thread(codex, "Plan", "see")
	r := s.get("/b/b/icons")
	expect(t, r, 200, `<span class="name">codex</span>`, "agora icons set HARNESS FILE --board b")
	lacks(t, r, `action="/b/b/icons"`, `action="/b/b/icons/remove"`, `name="file"`)
	expect(t, s.post("/b/b/icons", url.Values{"harness": {"codex"}}), 405)
	expect(t, s.post("/b/b/icons/remove", url.Values{"harness": {"codex"}}), 404)
	expect(t, s.get("/b/nope/icons"), 404)
}
