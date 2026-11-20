package server

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/zlcwwhq/agora/internal/post"
)

// formFile is a file that a form sends.
type formFile struct {
	name string
	data []byte
}

// postFiles sends a form as multipart/form-data, as browsers send the forms
// that carry files, from the pages' own origin.
func (s *site) postFiles(path string, fields url.Values, files ...formFile) response {
	s.t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for k, vs := range fields {
		for _, v := range vs {
			if err := mw.WriteField(k, v); err != nil {
				s.t.Fatal(err)
			}
		}
	}
	for _, f := range files {
		w, err := mw.CreateFormFile("files", f.name)
		if err != nil {
			s.t.Fatal(err)
		}
		if _, err := w.Write(f.data); err != nil {
			s.t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		s.t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, s.base+path, &body)
	if err != nil {
		s.t.Fatal(err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Origin", s.base)
	return s.do(req)
}

func (s *site) changeThread(who post.Identity, id int64, c post.ThreadChange) {
	s.t.Helper()
	if _, err := s.store.ChangeThread(context.Background(), who, "b", id, c); err != nil {
		s.t.Fatal(err)
	}
}

func (s *site) attach(who post.Identity, id int64, name, typ string, data []byte) {
	s.t.Helper()
	if _, _, err := s.store.Attach(context.Background(), who, "b", id, name, typ, data); err != nil {
		s.t.Fatal(err)
	}
}

func (s *site) attachments(id int64) []post.Attachment {
	s.t.Helper()
	p, err := s.store.Get(context.Background(), "b", id)
	if err != nil {
		s.t.Fatal(err)
	}
	return p.Attachments
}

func TestBoardPageStatesAndPages(t *testing.T) {
	s := serve(t, post.Identity{})
	for _, title := range []string{"First", "Second", "Third"} {
		s.thread(codex, title, "body")
	}
	yes, resolved := true, post.StateResolved
	s.changeThread(alice, 1, post.ThreadChange{Pinned: &yes, Locked: &yes})
	s.changeThread(codex, 2, post.ThreadChange{State: &resolved})

	r := s.get("/b/b/")
	expect(t, r, 200, "3 posts in 3 threads", `<span class="state pinned"`, `<span class="state locked"`, `<span class="state resolved"`,
		`href="/b/b/?state=open"`, `href="/b/b/?state=resolved"`)
	lacks(t, r, "Next page", "Previous page")
	// Pinned first, then by last activity.
	if i1, i3, i2 := strings.Index(r.body, ">First<"), strings.Index(r.body, ">Third<"), strings.Index(r.body, ">Second<"); !(i1 < i3 && i3 < i2) {
		t.Errorf("order: First %d, Third %d, Second %d", i1, i3, i2)
	}

	r = s.get("/b/b/?state=resolved")
	expect(t, r, 200, ">Second<", `<a class="chip" href="/b/b/" title="Show every thread"><span>resolved</span>`)
	lacks(t, r, ">First<", ">Third<")
	expect(t, s.get("/b/b/?q=body&state=open"), 200, ">First<", ">Third<", `name="state" value="open"`)
	lacks(t, s.get("/b/b/?q=body&state=open"), ">Second<")

	// Pages link to each other.
	r = s.get("/b/b/?n=1")
	expect(t, r, 200, ">First<", `<a class="btn" href="/b/b/?n=1&amp;skip=1" rel="next">`)
	lacks(t, r, `rel="prev"`)
	r = s.get("/b/b/?n=1&skip=1")
	expect(t, r, 200, ">Third<", `<a class="btn" href="/b/b/?n=1" rel="prev">`, `<a class="btn" href="/b/b/?n=1&amp;skip=2" rel="next">`)
	r = s.get("/b/b/?n=1&skip=2")
	expect(t, r, 200, ">Second<", `<a class="btn" href="/b/b/?n=1&amp;skip=1" rel="prev">`)
	lacks(t, r, `rel="next"`)
	r = s.get("/b/b/?n=2&skip=1&t=x")
	expect(t, r, 200, "No more threads", `<a class="btn" href="/b/b/?n=2&amp;t=x" rel="prev">`)
	// A page past the end, as after deletions, still leads back.
	r = s.get("/b/b/?skip=5")
	expect(t, r, 200, "No more threads", `<a class="btn" href="/b/b/" rel="prev">`)
	lacks(t, r, "No threads yet", `rel="next"`)
	// Searches ignore pins.
	expect(t, s.get("/b/b/?q=body&n=2&skip=1"), 200, ">Second<", ">First<", `href="/b/b/?n=2&amp;q=body" rel="prev"`)

	for _, bad := range []string{"/b/b/?skip=-1", "/b/b/?skip=x", "/b/b/?state=closed", "/b/b/?q=x&state=Open"} {
		expect(t, s.get(bad), 400, "Back to Board B")
	}
}

func TestThreadPageStatesAndButtons(t *testing.T) {
	s := serve(t, alice)
	s.thread(codex, "Question", "how?")
	s.reply(claude, 1, "so")
	r := s.get("/b/b/t/2")
	expect(t, r, 200, `<span class="state open"`, `action="/b/b/t/1/thread"`, `name="change" value="resolve"`, `name="change" value="pin"`, `name="change" value="lock"`)
	lacks(t, r, "This thread is locked")

	for _, change := range []string{"resolve", "pin", "lock"} {
		location(t, s.post("/b/b/t/2/thread", url.Values{"change": {change}}), 303, "/b/b/t/1#1")
	}
	ts := s.threads()
	if ts[0].State != post.StateResolved || !ts[0].Pinned || !ts[0].Locked {
		t.Fatalf("thread = %+v", ts[0])
	}
	r = s.get("/b/b/t/1")
	expect(t, r, 200, `<span class="state resolved"`, `<span class="state pinned"`, `<span class="state locked"`, "This thread is locked",
		`name="change" value="reopen"`, `name="change" value="unpin"`, `name="change" value="unlock"`)
	for _, change := range []string{"reopen", "unpin", "unlock"} {
		location(t, s.post("/b/b/t/1/thread", url.Values{"change": {change}}), 303, "/b/b/t/1#1")
	}
	if ts := s.threads(); ts[0].State != post.StateOpen || ts[0].Pinned || ts[0].Locked {
		t.Errorf("thread after undoing = %+v", ts[0])
	}
	expect(t, s.post("/b/b/t/1/thread", url.Values{"change": {"close"}}), 400, `not &#34;close&#34;`)
	expect(t, s.post("/b/b/t/9/thread", url.Values{"change": {"pin"}}), 404)
	expect(t, s.send(http.MethodPost, "/b/b/t/1/thread", "http://evil.example", url.Values{"change": {"pin"}}), 403)
	expect(t, s.get("/b/b/t/1/thread"), 405)
	if s.threads()[0].Pinned {
		t.Error("a refused form pinned the thread")
	}

	// Pages that only read show the state, without buttons.
	ro := serve(t, post.Identity{})
	resolved := post.StateResolved
	ro.thread(codex, "Question", "how?")
	ro.changeThread(codex, 1, post.ThreadChange{State: &resolved})
	r = ro.get("/b/b/t/1")
	expect(t, r, 200, `<span class="state resolved"`)
	lacks(t, r, `/t/1/thread`, `name="change"`)
	expect(t, ro.post("/b/b/t/1/thread", url.Values{"change": {"pin"}}), 404)
	expect(t, ro.post("/b/b/t/1/move", url.Values{"to": {"1"}}), 404)
}

func TestAttachmentPages(t *testing.T) {
	s := serve(t, post.Identity{})
	s.thread(codex, "Files", "see")
	png := []byte("\x89PNG\r\n\x1a\nfake")
	s.attach(codex, 1, "shot.png", "image/png", png)
	s.attach(codex, 1, "notes.txt", "text/plain; charset=iso-8859-1", []byte("plain"))
	s.attach(codex, 1, "page.html", "text/html; charset=utf-8", []byte("<script>alert(1)</script>"))
	s.attach(codex, 1, "报告 #1.pdf", "application/pdf", []byte("%PDF"))

	r := s.get("/b/b/t/1")
	expect(t, r, 200, `<img src="/b/b/a/1/shot.png" alt="shot.png"`, `href="/b/b/a/1/notes.txt"`, `href="/b/b/a/1/page.html"`,
		`href="/b/b/a/1/%E6%8A%A5%E5%91%8A%20%231.pdf"`, "报告 #1.pdf", "12 B")
	lacks(t, r, `<img src="/b/b/a/1/page.html"`, `<img src="/b/b/a/1/notes.txt"`)

	for _, c := range []struct {
		path, ctype, disposition, body string
	}{
		{"/b/b/a/1/shot.png", "image/png", `inline; filename=shot.png`, string(png)},
		{"/b/b/a/1/notes.txt", "text/plain; charset=utf-8", `inline; filename=notes.txt`, "plain"},
		{"/b/b/a/1/page.html", "application/octet-stream", `attachment; filename=page.html`, "<script>alert(1)</script>"},
		{"/b/b/a/1/%E6%8A%A5%E5%91%8A%20%231.pdf", "application/octet-stream", `attachment; filename*=utf-8''%E6%8A%A5%E5%91%8A%20#1.pdf`, "%PDF"},
	} {
		r := s.get(c.path)
		if r.status != 200 || r.body != c.body || r.header.Get("Content-Type") != c.ctype || r.header.Get("Content-Disposition") != c.disposition {
			t.Errorf("%s: %d %q, Content-Type %q, Content-Disposition %q", c.path, r.status, r.body, r.header.Get("Content-Type"), r.header.Get("Content-Disposition"))
		}
	}
	for path, status := range map[string]int{
		"/b/b/a/1/shot.png": 200, "/b/b/a/1/nope.png": 404, "/b/b/a/9/shot.png": 404, "/b/b/a/01/shot.png": 400,
		"/b/b/a/1/a%5Cb": 400, "/b/nope/a/1/shot.png": 404, "/b/Bad/a/1/shot.png": 400,
	} {
		r := s.get(path)
		if r.status != status || r.header.Get("Content-Security-Policy") != "sandbox" || r.header.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("%s: %d, want %d; headers %v", path, r.status, status, r.header)
		}
	}
	r = s.send(http.MethodPost, "/b/b/a/1/shot.png", s.base, nil)
	if r.status != 405 || r.header.Get("Content-Security-Policy") != "sandbox" {
		t.Errorf("POST to an attachment: %d %v", r.status, r.header)
	}
	req, _ := http.NewRequest(http.MethodHead, s.base+"/b/b/a/1/shot.png", nil)
	if r := s.do(req); r.status != 200 || r.body != "" || r.header.Get("Content-Type") != "image/png" {
		t.Errorf("HEAD = %d %q %v", r.status, r.body, r.header)
	}
}

func attachedNames(as []post.Attachment) []string {
	out := []string{}
	for _, a := range as {
		out = append(out, a.Name)
	}
	return out
}

func TestFormsWithFiles(t *testing.T) {
	s := serve(t, alice)
	r := s.get("/b/b/new")
	expect(t, r, 200, `enctype="multipart/form-data"`, `type="file" name="files" multiple`)
	m := nonceField.FindStringSubmatch(r.body)
	fields := url.Values{"title": {"With files"}, "body": {"see"}, "tags": {"logs"}, "nonce": {m[1]}}
	files := []formFile{{"build.log", []byte("log\n")}, {"shot.png", []byte("png")}}
	location(t, s.postFiles("/b/b/new", fields, files...), 303, "/b/b/t/1#1")
	// Sent twice, it posts once, and attaches the same again.
	location(t, s.postFiles("/b/b/new", fields, files...), 303, "/b/b/t/1#1")
	as := s.attachments(1)
	if len(s.threads()) != 1 || !slices.Equal(attachedNames(as), []string{"build.log", "shot.png"}) || as[1].Type != "image/png" || as[0].Size != 4 {
		t.Fatalf("threads %+v, attachments %+v", s.threads(), as)
	}

	r = s.get("/b/b/t/1")
	expect(t, r, 200, `enctype="multipart/form-data"`, `id="reply-files"`)
	location(t, s.postFiles("/b/b/t/1", url.Values{"body": {"a reply"}}, formFile{"notes", []byte("n")}), 303, "/b/b/t/1#2")
	if as := s.attachments(2); len(as) != 1 || as[0] != (post.Attachment{Name: "notes", Type: "application/octet-stream", Size: 1}) {
		t.Errorf("the reply's attachments = %+v", as)
	}
	// A file field without a file attaches nothing.
	location(t, s.postFiles("/b/b/t/1", url.Values{"body": {"no files"}}, formFile{"", nil}), 303, "/b/b/t/1#3")
	if as := s.attachments(3); len(as) != 0 {
		t.Errorf("an empty file field attached %+v", as)
	}

	// The edit form detaches what is checked, then attaches, without
	// editing; and it moves the post.
	r = s.get("/b/b/t/1/edit")
	expect(t, r, 200, `enctype="multipart/form-data"`, `name="detach" value="build.log"`, `name="detach" value="shot.png"`,
		`action="/b/b/t/1/move"`, `name="to"`)
	lacks(t, r, `id="m-title"`)
	expect(t, s.get("/b/b/t/2/edit"), 200, `action="/b/b/t/2/move"`, `id="m-title"`)
	same := url.Values{"title": {"With files"}, "body": {"see"}, "tags": {"logs"}, "detach": {"build.log", "shot.png"}}
	location(t, s.postFiles("/b/b/t/1/edit", same, formFile{"shot.png", []byte("new png")}), 303, "/b/b/t/1#1")
	p, _ := s.store.Get(context.Background(), "b", 1)
	if !slices.Equal(attachedNames(p.Attachments), []string{"shot.png"}) || p.Attachments[0].Size != 7 || p.Edited != "" {
		t.Errorf("after the edit form: %+v", p)
	}
	expect(t, s.postFiles("/b/b/t/1/edit", url.Values{"title": {"x"}, "body": {""}, "tags": {""}, "detach": {"gone.txt"}}), 404, "has no attachment")

	// Refused files write nothing.
	before := len(s.threads())
	expect(t, s.postFiles("/b/b/new", url.Values{"title": {"x"}, "body": {"kept words"}}, formFile{"a.txt", []byte("a")}, formFile{"a.txt", []byte("b")}), 400, "Two files are named a.txt", "kept words")
	expect(t, s.postFiles("/b/b/new", url.Values{"title": {"x"}, "body": {"x"}}, formFile{"bad\x01name", []byte("a")}), 400)
	expect(t, s.postFiles("/b/b/new", url.Values{"title": {"x"}, "body": {"x"}}, formFile{"big.bin", make([]byte, post.MaxAttachment+1)}), 413, "larger than 10 MiB")
	big := []formFile{}
	for _, n := range []string{"1", "2", "3", "4"} {
		big = append(big, formFile{n, make([]byte, 9<<20)})
	}
	expect(t, s.postFiles("/b/b/new", url.Values{"title": {"x"}, "body": {"x"}}, big...), 413, "larger than 32 MiB")
	expect(t, s.postFiles("/b/b/new", url.Values{"title": {"x"}, "body": {strings.Repeat("x", 1<<20)}}), 413, "larger than 1 MiB")
	// Forms without files keep the bound of other requests.
	expect(t, s.postFiles("/b/b/t/1/delete", url.Values{"x": {strings.Repeat("x", 1<<20)}}), 413)
	if len(s.threads()) != before {
		t.Errorf("refused forms posted: %+v", s.threads())
	}
	if _, err := s.store.Get(context.Background(), "b", 1); err != nil {
		t.Errorf("a refused delete deleted: %v", err)
	}
	// Up to 10 MiB each, in a form of up to 32 MiB.
	location(t, s.postFiles("/b/b/t/1", url.Values{"body": {"big ones"}}, formFile{"a.bin", make([]byte, post.MaxAttachment)}, formFile{"b.bin", make([]byte, 9<<20)}), 303, "/b/b/t/1#4")
	if as := s.attachments(4); len(as) != 2 || as[0].Size != post.MaxAttachment {
		t.Errorf("large attachments = %+v", as)
	}
}

func TestMoveForm(t *testing.T) {
	s := serve(t, alice)
	s.thread(codex, "A", "a")
	s.reply(codex, 1, "two")
	s.reply(claude, 2, "three")
	s.thread(codex, "B", "b")

	location(t, s.post("/b/b/t/2/move", url.Values{"to": {"#4"}}), 303, "/b/b/t/4#2")
	show, err := s.store.Show(context.Background(), "b", 4)
	if err != nil || len(show.Posts) != 3 {
		t.Fatalf("thread 4 = %+v, %v", show, err)
	}
	location(t, s.post("/b/b/t/3/move", url.Values{"title": {"C"}}), 303, "/b/b/t/3#3")
	if ts := s.threads(); len(ts) != 3 || ts[1].ID != 3 || ts[1].Title != "C" {
		t.Errorf("threads = %+v", ts)
	}

	r := s.post("/b/b/t/2/move", url.Values{"to": {"2"}})
	expect(t, r, 400, "moves with #2", `id="move"`, `name="to" value="2"`, `action="/b/b/t/2/edit"`)
	expect(t, s.post("/b/b/t/2/move", url.Values{"to": {"x"}}), 400, "Invalid post id")
	expect(t, s.post("/b/b/t/2/move", url.Values{"to": {"4"}, "title": {"D"}}), 400, "exactly one")
	expect(t, s.post("/b/b/t/2/move", url.Values{}), 400, "exactly one")
	expect(t, s.post("/b/b/t/1/move", url.Values{"title": {"D"}}), 400, "already a thread")
	expect(t, s.post("/b/b/t/2/move", url.Values{"to": {"9"}}), 404, "No post #9")
	expect(t, s.post("/b/b/t/9/move", url.Values{"to": {"1"}}), 404)
	expect(t, s.get("/b/b/t/2/move"), 405)
}
