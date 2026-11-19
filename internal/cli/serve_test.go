package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// lockedBuffer is a buffer that a server's goroutines can write to.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// serve runs agora serve on a free port. It returns the interface's address
// and the board that serve printed, and a function that stops it and returns
// its exit status and standard error.
func (m *machine) serve(args ...string) (string, string, func() (int, string)) {
	m.t.Helper()
	s, stop := m.startServe(args...)
	return s.URL, s.Board, stop
}

// served is what serve prints once it listens.
type served struct{ URL, Board, Author string }

// startServe is like serve, but returns all that serve printed.
func (m *machine) startServe(args ...string) (served, func() (int, string)) {
	m.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	out, w := io.Pipe()
	var errOut lockedBuffer
	done := make(chan int, 1)
	go func() {
		code := Run(Env{
			Args:   append([]string{"serve", "--addr", "127.0.0.1:0", "--json"}, args...),
			Getenv: func(k string) string { return m.env[k] },
			Dir:    m.dir,
			GOOS:   m.goos,
			Stdin:  unreadable{m.t},
			Stdout: w, Stderr: &errOut,
			Now:     m.clock,
			Context: ctx,
		})
		_ = w.Close()
		done <- code
	}()
	stopped := false
	var code int
	stop := func() (int, string) {
		if !stopped {
			stopped = true
			cancel()
			select {
			case code = <-done:
			case <-time.After(30 * time.Second):
				m.t.Fatal("serve didn't stop")
			}
		}
		return code, errOut.String()
	}
	m.t.Cleanup(func() { stop() })
	var started served
	if err := json.NewDecoder(out).Decode(&started); err != nil {
		code, stderr := stop()
		m.t.Fatalf("agora serve: status %d, %v\n%s", code, err, stderr)
	}
	go func() { _, _ = io.Copy(io.Discard, out) }()
	return started, stop
}

var client = &http.Client{Timeout: 30 * time.Second}

// get requests a page, with host as its Host if given.
func get(t *testing.T, method, page string, host ...string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, page, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(host) > 0 {
		req.Host = host[0]
	}
	resp, err := client.Do(req)
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

// page gets a page that must exist.
func page(t *testing.T, page string) string {
	t.Helper()
	resp, body := get(t, http.MethodGet, page)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d\n%s", page, resp.StatusCode, body)
	}
	return body
}

// attrs returns the values of a data attribute in a page, in order.
func attrs(body, name string) []string {
	out := []string{}
	for _, m := range regexp.MustCompile(` data-`+name+`="([^"]*)"`).FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

func threadIDs[T any](items []T, id func(T) string) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, id(it))
	}
	return out
}

type listed struct{ ID string }

type found struct {
	ID      string
	Matches []struct{ ID string }
}

// snapshot returns every file under dir with its contents.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		files[path] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestServe(t *testing.T) {
	m := newMachine(t, "wsl")
	mac := newMachine(t, "mac")
	root := m.post("--title", "Star edges should only warn", "-t", "mutagen", "-t", "design",
		"-m", "Proposal: warn instead. See #mac-9, which hasn't arrived.")
	reply := m.reply(root, "--as", "claude-code/session-one", "-t", "review", "-m", "Agree, but **replicas** still block star edges.")
	m.reply(reply, "--as", "gemini-cli/g1", "-m", "Replicas still block, yes.")
	escaped := m.post("--as", "claude-code/session-two", "--title", "<script>alert(1)</script> in titles", "-t", "security",
		"-m", "Titles are text. Follows #"+root+".")
	m.post("--title", "Index 冲突 on rebuild", "-t", "index", "-m", "The STAR of this body is 冲突.")
	var init struct{ Board string }
	m.json(&init, "init", "--node", "wsl")

	// A reader that has seen every post, to see that pages mark nothing seen.
	m.ok("unread", "--as", "codex/watcher")
	cursors := snapshot(t, m.env["XDG_STATE_HOME"])
	for path := range cursors {
		if !strings.Contains(filepath.ToSlash(path), "/cursors/") {
			delete(cursors, path)
		}
	}

	base, board, stop := m.serve()
	if board != init.Board || !strings.HasPrefix(base, "http://127.0.0.1:") || !strings.HasSuffix(base, "/") {
		t.Fatalf("serve printed url %q and board %q, want board %q", base, board, init.Board)
	}
	base = strings.TrimSuffix(base, "/")
	files := snapshot(t, m.root)

	// / lists threads as list does, with each filter.
	for _, c := range []struct {
		query string
		args  []string
	}{
		{"", nil},
		{"?t=mutagen", []string{"-t", "mutagen"}},
		{"?t=MUTAGEN&t=design", []string{"-t", "mutagen", "-t", "design"}},
		{"?author=claude-code", []string{"--author", "claude-code"}},
		{"?author=claude-code/session-t", []string{"--author", "claude-code/session-t"}},
		{"?n=2", []string{"-n", "2"}},
	} {
		var want []listed
		m.json(&want, append([]string{"list", "-n", "50"}, c.args...)...)
		body := page(t, base+"/"+c.query)
		if got := attrs(body, "thread"); !reflect.DeepEqual(got, threadIDs(want, func(l listed) string { return l.ID })) {
			t.Errorf("/%s lists %v, want %v as agora list %v", c.query, got, want, c.args)
		}
	}
	if body := page(t, base+"/?n=2"); !strings.Contains(body, `href="/?n=52"`) {
		t.Errorf("/?n=2 doesn't offer more threads")
	}
	if body := page(t, base+"/"); strings.Contains(body, `href="/?n=`) {
		t.Errorf("/ offers more threads, but shows all")
	}

	// It searches as search does.
	for _, c := range []struct {
		query string
		args  []string
	}{
		{"?q=star", []string{"star"}},
		{"?q=STAR+warn", []string{"STAR", "warn"}},
		{"?q=%22still+block%22", []string{"still block"}},
		{"?q=star&in=title", []string{"star", "--title"}},
		{"?q=star&in=body", []string{"star", "--body"}},
		{"?q=%E5%86%B2%E7%AA%81", []string{"冲突"}},
		{"?q=star&t=review", []string{"star", "-t", "review"}},
		{"?q=still&author=gemini-cli", []string{"still", "--author", "gemini-cli"}},
		{"?q=nothing-matches-this", []string{"nothing-matches-this"}},
	} {
		var want []found
		m.json(&want, append([]string{"search", "-n", "50"}, c.args...)...)
		body := page(t, base+"/"+c.query)
		if got := attrs(body, "thread"); !reflect.DeepEqual(got, threadIDs(want, func(f found) string { return f.ID })) {
			t.Errorf("/%s finds %v, want %v as agora search %v", c.query, got, want, c.args)
		}
		var matches []string
		for _, f := range want {
			for _, mt := range f.Matches {
				matches = append(matches, mt.ID)
			}
		}
		if got := attrs(body, "match"); !reflect.DeepEqual(got, append([]string{}, matches...)) {
			t.Errorf("/%s matches %v, want %v", c.query, got, matches)
		}
	}
	if body := page(t, base+"/?q="+url.QueryEscape("#"+root)); !strings.Contains(body, `href="/t/`+root+`#`+root+`"`) {
		t.Errorf("searching for an id doesn't lead to its post")
	}

	// /t/ID shows the thread as show does.
	var show struct {
		Posts      []listed
		Backlinks  []listed
		Unresolved []string
	}
	m.json(&show, "show", reply)
	body := page(t, base+"/t/"+reply)
	if got := attrs(body, "post"); !reflect.DeepEqual(got, threadIDs(show.Posts, func(l listed) string { return l.ID })) {
		t.Errorf("/t/%s shows %v, want %v", reply, got, show.Posts)
	}
	for _, want := range []string{
		`class="post focus" data-post="` + reply + `"`,
		`<strong>replicas</strong>`,
		`href="/t/` + escaped + `#` + escaped + `"`,
		`<a href="/t/mac-9#mac-9" class="ref missing"`,
		`class="hicon claude-code"`, `class="hicon codex"`, `>G</text>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/t/%s has no %s", reply, want)
		}
	}
	if body := page(t, base+"/t/"+root); strings.Contains(body, ` focus" data-post=`) {
		t.Errorf("/t/%s marks a post as the focus, but it names the thread", root)
	}
	if len(show.Backlinks) != 1 || len(show.Unresolved) != 1 {
		t.Fatalf("show %s: backlinks %v, unresolved %v", reply, show.Backlinks, show.Unresolved)
	}

	// /tags lists tags as tags does.
	var tags []struct{ Tag string }
	m.json(&tags, "tags")
	if got := attrs(page(t, base+"/tags"), "tag"); !reflect.DeepEqual(got, threadIDs(tags, func(tc struct{ Tag string }) string { return tc.Tag })) {
		t.Errorf("/tags lists %v, want %v", got, tags)
	}

	// Pages show text as text.
	for _, p := range []string{"/", "/t/" + escaped, "/?q=script"} {
		body := page(t, base+p)
		if strings.Contains(body, "<script") || !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
			t.Errorf("%s doesn't escape a title", p)
		}
	}

	// Posts that arrive while serving appear.
	late := mac.post("--title", "Arrived from mac while serving", "-m", "Hello.")
	copyPosts(t, mac, m, late)
	var withLate []listed
	m.json(&withLate, "list", "-n", "50")
	got := attrs(page(t, base+"/"), "thread")
	if want := threadIDs(withLate, func(l listed) string { return l.ID }); !reflect.DeepEqual(got, want) || !strings.Contains(strings.Join(got, " "), late) {
		t.Errorf("/ lists %v, want %v with %s", got, want, late)
	}
	if body := page(t, base+"/t/"+late); !strings.Contains(body, "Hello.") {
		t.Errorf("/t/%s doesn't show it", late)
	}

	// Status codes and headers.
	for _, c := range []struct {
		method, path string
		host         string
		status       int
	}{
		{http.MethodHead, "/", "", http.StatusOK},
		{http.MethodGet, "/static/style.css", "", http.StatusOK},
		{http.MethodGet, "/t/wsl-999", "", http.StatusNotFound},
		{http.MethodGet, "/t/not-an-id", "", http.StatusBadRequest},
		{http.MethodGet, "/?n=0", "", http.StatusBadRequest},
		{http.MethodGet, "/?t=a,b", "", http.StatusBadRequest},
		{http.MethodGet, "/?author=Not+Valid", "", http.StatusBadRequest},
		{http.MethodGet, "/?q=x&in=tags", "", http.StatusBadRequest},
		{http.MethodGet, "/nowhere", "", http.StatusNotFound},
		{http.MethodGet, "/static/", "", http.StatusNotFound},
		{http.MethodPost, "/", "", http.StatusMethodNotAllowed},
		{http.MethodDelete, "/t/" + root, "", http.StatusMethodNotAllowed},
		{http.MethodGet, "/", "evil.example", http.StatusForbidden},
		{http.MethodGet, "/", "evil.example:80", http.StatusForbidden},
		{http.MethodGet, "/", "localhost", http.StatusOK},
		{http.MethodGet, "/", "[::1]:7777", http.StatusOK},
	} {
		resp, body := get(t, c.method, base+c.path, c.host)
		if resp.StatusCode != c.status {
			t.Errorf("%s %s (Host %q): status %d, want %d", c.method, c.path, c.host, resp.StatusCode, c.status)
		}
		if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "script-src") {
			t.Errorf("%s %s: Content-Security-Policy %q", c.method, c.path, csp)
		}
		if strings.Contains(body, "<script") {
			t.Errorf("%s %s: the page has a script", c.method, c.path)
		}
		if c.status == http.StatusMethodNotAllowed && resp.Header.Get("Allow") != "GET, HEAD" {
			t.Errorf("%s %s: Allow %q", c.method, c.path, resp.Header.Get("Allow"))
		}
	}

	// Pages wrote nothing but the late post, and no cursor.
	after := snapshot(t, m.root)
	for path := range after {
		if _, ok := files[path]; !ok && !strings.HasSuffix(filepath.ToSlash(path), "/posts/mac/1.md") {
			t.Errorf("serving wrote %s", path)
		}
	}
	for path, data := range cursors {
		if now, err := os.ReadFile(path); err != nil || string(now) != data {
			t.Errorf("serving changed the cursor %s", path)
		}
	}

	if code, stderr := stop(); code != 0 {
		t.Errorf("serve stopped with status %d\n%s", code, stderr)
	}
	if _, err := client.Get(base + "/"); err == nil {
		t.Error("serve still answers after it stopped")
	}
}

func TestServeWithoutBoard(t *testing.T) {
	m := newMachine(t, "")
	out, errOut, code := m.run("serve", "--addr", "127.0.0.1:0", "--json")
	if code != 1 || out != "" || !strings.Contains(errOut, "no board") {
		t.Errorf("serve without a board: status %d, stdout %q, stderr %q", code, out, errOut)
	}
}
