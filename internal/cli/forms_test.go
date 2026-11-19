package cli

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// browser sends forms as a browser does, without following redirects, so
// that tests see them.
var browser = &http.Client{
	Timeout:       30 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// submit posts a form's fields to page, with origin as its Origin if given,
// and host as its Host.
func submit(t *testing.T, page string, fields url.Values, origin, host string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, page, strings.NewReader(fields.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if host != "" {
		req.Host = host
	}
	resp, err := browser.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(body)
}

var tokenField = regexp.MustCompile(`name="token" value="([0-9a-f]+)"`)

// token returns the token of a page's form.
func token(t *testing.T, body string) string {
	t.Helper()
	m := tokenField.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("the page has no form token:\n%s", body)
	}
	return m[1]
}

// postFiles counts the board's post files.
func postFiles(t *testing.T, m *machine) int {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(m.root, "posts", "*", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	return len(files)
}

func TestServeForms(t *testing.T) {
	m := newMachine(t, "wsl")
	root := m.post("--title", "Agents start here", "-m", "A question for people.")
	s, stop := m.startServe("--as", "human/alice")
	if s.Author != "human/alice@wsl" {
		t.Fatalf("serve --as human/alice posts as %q", s.Author)
	}
	base := strings.TrimSuffix(s.URL, "/")
	origin := base

	// Pages offer the forms and say whom they post as.
	if body := page(t, base+"/"); !strings.Contains(body, `href="/new"`) || !strings.Contains(body, "posting as <b>human/alice@wsl</b>") {
		t.Fatalf("/ doesn't offer the form to start a thread:\n%s", body)
	}
	resp, newPage := get(t, http.MethodGet, base+"/new")
	tok := token(t, newPage)
	// Browsers send a form's POST from a page served with no-referrer with
	// "Origin: null", which the forms refuse.
	if policy := resp.Header.Get("Referrer-Policy"); resp.StatusCode != http.StatusOK || policy != "same-origin" {
		t.Fatalf("GET /new: status %d, Referrer-Policy %q", resp.StatusCode, policy)
	}

	// A new thread is the post that agora post makes from the same input.
	body := "Line one\r\nline two, after #" + root
	resp, _ = submit(t, base+"/new", url.Values{"token": {tok}, "title": {"  From a person  "},
		"tags": {"People, design  web"}, "body": {body}}, origin, "")
	loc := resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther || !regexp.MustCompile(`^/t/(wsl-\d+)#wsl-\d+$`).MatchString(loc) {
		t.Fatalf("POST /new: status %d, Location %q", resp.StatusCode, loc)
	}
	thread := strings.TrimPrefix(strings.Split(loc, "#")[0], "/t/")
	same := m.post("--as", "human/alice", "--title", "From a person", "-t", "People", "-t", "design", "-t", "web", "-m", body)
	var a, b struct{ Posts []postJSON }
	m.json(&a, "show", thread)
	m.json(&b, "show", same)
	got, want := a.Posts[0], b.Posts[0]
	if got.Author != "human/alice@wsl" || got.Harness != "human" || got.Title != want.Title || got.Body != want.Body ||
		!reflect.DeepEqual(got.Tags, want.Tags) || !reflect.DeepEqual(got.Refs, []string{root}) {
		t.Fatalf("the form's thread %+v, want it as agora post makes it, %+v", got, want)
	}

	// Each post links to the form that replies to it, which posts a reply.
	threadPage := page(t, base+"/t/"+root)
	for _, want := range []string{`href="/t/` + root + `#reply"`, `action="/t/` + root + `#reply"`, `class="hicon codex"`} {
		if !strings.Contains(threadPage, want) {
			t.Errorf("/t/%s has no %s", root, want)
		}
	}
	resp, _ = submit(t, base+"/t/"+root, url.Values{"token": {token(t, threadPage)}, "body": {"An answer from alice."}, "tags": {"answer"}}, origin, "")
	loc = resp.Header.Get("Location")
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(loc, "/t/wsl-") {
		t.Fatalf("POST /t/%s: status %d, Location %q", root, resp.StatusCode, loc)
	}
	reply := strings.TrimPrefix(strings.Split(loc, "#")[0], "/t/")
	var shown struct{ Posts []postJSON }
	m.json(&shown, "show", reply)
	last := shown.Posts[len(shown.Posts)-1]
	if last.ID != reply || last.Thread != root || last.Author != "human/alice@wsl" || last.Body != "An answer from alice.\n" ||
		!reflect.DeepEqual(last.Tags, []string{"answer"}) {
		t.Fatalf("the form's reply: %+v", last)
	}
	if body := page(t, base+loc); !strings.Contains(body, `class="hicon human"`) {
		t.Errorf("%s doesn't show the person's icon", loc)
	}

	// A thread whose root hasn't arrived offers no form to reply to the
	// root, only to the posts that are here.
	mac := newMachine(t, "mac")
	far := mac.post("--title", "From mac")
	farReply := mac.reply(far, "-m", "Its reply arrived first.")
	copyPosts(t, mac, m, farReply)
	if body := page(t, base+"/t/"+far); strings.Contains(body, `id="reply"`) || !strings.Contains(body, `href="/t/`+farReply+`#reply"`) {
		t.Errorf("/t/%s, whose root hasn't arrived, offers to reply to it", far)
	}
	if body := page(t, base+"/t/"+farReply); !strings.Contains(body, `action="/t/`+farReply+`#reply"`) {
		t.Errorf("/t/%s doesn't offer to reply to it", farReply)
	}

	// Posting started alice's cursor, as agora post does: her own posts are
	// seen, and a post from someone else since is unread.
	other := m.post("--title", "From an agent, after alice posted")
	if isNew, ids := unreadIDs(t, m, "--as", "human/alice"); isNew || !reflect.DeepEqual(ids, []string{farReply, other}) {
		t.Errorf("alice's unread: new reader %v, %v, want only %s and %s", isNew, ids, farReply, other)
	}

	// A post that agora post or reply would reject gets the form again, with
	// what was typed and why.
	before := postFiles(t, m)
	for _, c := range []struct {
		path   string
		fields url.Values
		want   []string
	}{
		{"/new", url.Values{"title": {"   "}, "body": {"Keep this text"}}, []string{"Keep this text", "A thread needs a title."}},
		{"/new", url.Values{"title": {"Tags"}, "tags": {"a#b"}}, []string{`value="Tags"`, `value="a#b"`, "Invalid tag"}},
		{"/t/" + root, url.Values{"body": {"  \r\n "}, "tags": {"keep-tag"}}, []string{`id="reply"`, `value="keep-tag"`, "A reply needs a body."}},
	} {
		c.fields.Set("token", tok)
		resp, body := submit(t, base+c.path, c.fields, origin, "")
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("POST %s %v: status %d, want 400", c.path, c.fields, resp.StatusCode)
		}
		for _, want := range c.want {
			if !strings.Contains(body, want) {
				t.Errorf("POST %s %v: the page has no %s", c.path, c.fields, want)
			}
		}
	}

	// Requests that didn't come from the pages, or that the pages can't take.
	good := url.Values{"token": {tok}, "title": {"Should not be posted"}}
	for _, c := range []struct {
		name, path   string
		fields       url.Values
		origin, host string
		status       int
	}{
		{"no token", "/new", url.Values{"title": {"x"}}, origin, "", http.StatusForbidden},
		{"another token", "/new", url.Values{"token": {strings.Repeat("0", len(tok))}, "title": {"x"}}, origin, "", http.StatusForbidden},
		{"another origin", "/new", good, "http://evil.example", "", http.StatusForbidden},
		{"a hidden origin", "/new", good, "null", "", http.StatusForbidden},
		{"another host", "/new", good, "", "evil.example", http.StatusForbidden},
		{"a post that isn't here", "/t/wsl-999", url.Values{"token": {tok}, "body": {"x"}}, origin, "", http.StatusNotFound},
		{"not an id", "/t/not-an-id", url.Values{"token": {tok}, "body": {"x"}}, origin, "", http.StatusBadRequest},
		{"too large", "/new", url.Values{"token": {tok}, "title": {"big"}, "body": {strings.Repeat("x", 1<<20)}}, origin, "", http.StatusRequestEntityTooLarge},
		{"a page without a form", "/tags", good, origin, "", http.StatusMethodNotAllowed},
	} {
		if resp, _ := submit(t, base+c.path, c.fields, c.origin, c.host); resp.StatusCode != c.status {
			t.Errorf("%s: POST %s: status %d, want %d", c.name, c.path, resp.StatusCode, c.status)
		}
	}
	if after := postFiles(t, m); after != before {
		t.Errorf("refused requests posted %d posts", after-before)
	}
	req, err := http.NewRequest(http.MethodPut, base+"/new", bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp, err := browser.Do(req); err != nil || resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != "GET, HEAD, POST" {
		t.Errorf("PUT /new: %v %v", resp, err)
	} else {
		_ = resp.Body.Close()
	}

	// A browser that sends no Origin still posts with the token.
	if resp, _ := submit(t, base+"/new", url.Values{"token": {tok}, "title": {"Without an origin"}}, "", ""); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("POST /new without an Origin: status %d", resp.StatusCode)
	}
	if code, stderr := stop(); code != 0 {
		t.Errorf("serve stopped with status %d\n%s", code, stderr)
	}
}

func TestServeFormsNeedAPerson(t *testing.T) {
	m := newMachine(t, "wsl")
	m.post("--title", "t")
	for _, c := range []struct {
		name string
		env  map[string]string
		args []string
	}{
		{"an agent's AGORA_AUTHOR", nil, nil},
		{"--as an agent", nil, []string{"--as", "codex/x"}},
		{"a person only detected", map[string]string{"AGORA_AUTHOR": "", "CLAUDE_CODE_SESSION_ID": "95d3e5c1"}, nil},
	} {
		saved := map[string]string{}
		for k, v := range c.env {
			saved[k] = m.env[k]
			m.env[k] = v
		}
		s, stop := m.startServe(c.args...)
		base := strings.TrimSuffix(s.URL, "/")
		if resp, _ := get(t, http.MethodGet, base+"/new"); s.Author != "" || resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: author %q, GET /new status %d", c.name, s.Author, resp.StatusCode)
		}
		if resp, _ := submit(t, base+"/new", url.Values{"title": {"x"}}, base, ""); resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s: POST /new status %d", c.name, resp.StatusCode)
		}
		if body := page(t, base+"/"); strings.Contains(body, `href="/new"`) || strings.Contains(body, `name="token"`) {
			t.Errorf("%s: / offers a form", c.name)
		}
		stop()
		for k, v := range saved {
			m.env[k] = v
		}
	}

	// An identity that isn't one is incorrect usage, as for other commands.
	for _, c := range []struct{ flag, env string }{{"nobody", ""}, {"", "Not/valid!"}} {
		saved := m.env["AGORA_AUTHOR"]
		args := []string{"serve", "--addr", "127.0.0.1:0", "--json"}
		if c.flag != "" {
			args = append(args, "--as", c.flag)
		} else {
			m.env["AGORA_AUTHOR"] = c.env
		}
		if out, errOut, code := m.run(args...); code != 2 || out != "" || !strings.Contains(errOut, "invalid identity") {
			t.Errorf("serve with identity %q / AGORA_AUTHOR %q: status %d, stdout %q, stderr %q", c.flag, c.env, code, out, errOut)
		}
		m.env["AGORA_AUTHOR"] = saved
	}

	// A person on a machine without a node name for the board only reads,
	// and serve says why.
	reader := newMachine(t, "")
	reader.env["AGORA_DIR"] = m.root
	s, stop := reader.startServe("--as", "human/alice")
	if _, stderr := stop(); s.Author != "" || !strings.Contains(stderr, "node name") || !strings.Contains(stderr, "agora init") {
		t.Errorf("a person without a node: author %q, stderr %q", s.Author, stderr)
	}

	// So does one who serves on another address than a loopback one.
	var errOut bytes.Buffer
	c := &ctx{env: Env{Getenv: func(k string) string { return m.env[k] }, GOOS: m.goos, Stderr: &errOut}, o: &opts{as: "human/alice"}}
	b, err := c.open(m.root)
	if err != nil {
		t.Fatal(err)
	}
	author, publish, err := c.forms(b, "wsl", &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 7777})
	if err != nil || author != "" || publish != nil || !strings.Contains(errOut.String(), "loopback") {
		t.Errorf("on 192.0.2.1: author %q, %v, stderr %q", author, err, errOut.String())
	}
}
