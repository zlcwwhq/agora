package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zlcwwhq/agora/internal/board"
	"github.com/zlcwwhq/agora/internal/testutil"
)

// machine is one machine with its own copy of a board and its own state.
type machine struct {
	t    *testing.T
	root string
	env  map[string]string
	goos string
	// dir is the working directory, empty unless a test sets it.
	dir string
	mu  sync.Mutex
	now time.Time
}

// unreadable fails the test if a command reads standard input without -f -.
type unreadable struct{ t *testing.T }

func (u unreadable) Read([]byte) (int, error) {
	u.t.Error("a command read standard input without -f -")
	return 0, io.EOF
}

var zone = time.FixedZone("CET", 3600)

func newMachine(t *testing.T, node string) *machine {
	t.Helper()
	dir := testutil.TempDir(t)
	m := &machine{
		t:    t,
		root: filepath.Join(dir, "agora"),
		env: map[string]string{
			"HOME":           dir,
			"USERPROFILE":    dir,
			"AGORA_DIR":      filepath.Join(dir, "agora"),
			"XDG_STATE_HOME": filepath.Join(dir, "state"),
			"AGORA_AUTHOR":   "codex/" + node + "-session",
		},
		goos: runtime.GOOS,
		now:  time.Date(2026, 11, 18, 10, 0, 0, 0, zone),
	}
	if node != "" {
		m.ok("init", "--node", node)
	}
	return m
}

// clock advances a minute per command, so that posts have distinct times.
func (m *machine) clock() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now = m.now.Add(time.Minute)
	return m.now
}

func (m *machine) runIn(stdin *string, args ...string) (string, string, int) {
	var out, errOut bytes.Buffer
	env := Env{
		Args:   args,
		Getenv: func(k string) string { return m.env[k] },
		Dir:    m.dir,
		GOOS:   m.goos,
		Stdin:  unreadable{m.t},
		Stdout: &out,
		Stderr: &errOut,
		Now:    m.clock,
	}
	if stdin != nil {
		env.Stdin = strings.NewReader(*stdin)
	}
	code := Run(env)
	return out.String(), errOut.String(), code
}

func (m *machine) run(args ...string) (string, string, int) { return m.runIn(nil, args...) }

func (m *machine) ok(args ...string) string {
	m.t.Helper()
	out, errOut, code := m.run(args...)
	if code != 0 {
		m.t.Fatalf("agora %s: status %d\n%s", strings.Join(args, " "), code, errOut)
	}
	return out
}

func (m *machine) json(v any, args ...string) {
	m.t.Helper()
	out := m.ok(append(args, "--json")...)
	if err := json.Unmarshal([]byte(out), v); err != nil {
		m.t.Fatalf("agora %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func (m *machine) post(args ...string) string {
	m.t.Helper()
	var r struct{ ID, Thread string }
	m.json(&r, append([]string{"post"}, args...)...)
	return r.ID
}

func (m *machine) reply(to string, args ...string) string {
	m.t.Helper()
	var r struct{ ID, Thread string }
	m.json(&r, append([]string{"reply", to}, args...)...)
	return r.ID
}

// copyPosts copies the post files that dst lacks from src, as Mutagen would.
// Only ids accepts restricts the copy.
func copyPosts(t *testing.T, src, dst *machine, only ...string) {
	t.Helper()
	want := map[string]bool{}
	for _, id := range only {
		node, n, _ := strings.Cut(id, "-")
		want[filepath.Join("posts", node, n+".md")] = true
	}
	err := filepath.WalkDir(filepath.Join(src.root, "posts"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(src.root, path)
		if len(want) > 0 && !want[rel] {
			return nil
		}
		target := filepath.Join(dst.root, rel)
		if _, err := os.Stat(target); err == nil {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPostReplyShowListSearchTags(t *testing.T) {
	m := newMachine(t, "wsl")
	root := m.post("--title", "Star 冲突处理", "-t", "Mutagen", "-t", "design", "-m", "Should conflicts block commands?")
	if root != "wsl-1" {
		t.Fatalf("first post is %s", root)
	}
	body := "I think 冲突 should only warn.\n"
	r1 := m.runIn
	out, errOut, code := r1(&body, "reply", root, "-t", "review", "-f", "-", "--json")
	if code != 0 || !strings.Contains(out, `"id": "wsl-2"`) {
		t.Fatalf("reply from standard input: %d %s %s", code, out, errOut)
	}
	r2 := m.reply("wsl-2", "-m", "Replying to the reply, see #wsl-1 and #gpu-9.", "--as", "claude-code/abcdef0123456789")
	other := m.post("--title", "Release checklist", "-m", "Mention #wsl-2.")

	var show struct {
		Focus      string
		Thread     threadJSON
		Posts      []postJSON
		Backlinks  []map[string]string
		Unresolved []string
	}
	m.json(&show, "show", r2)
	if show.Focus != r2 || show.Thread.ID != root || show.Thread.Replies != 2 || len(show.Posts) != 3 {
		t.Fatalf("show: %+v", show)
	}
	if !reflect.DeepEqual(show.Thread.Tags, []string{"design", "mutagen", "review"}) {
		t.Fatalf("thread tags: %v", show.Thread.Tags)
	}
	p := show.Posts[2]
	if p.ID != r2 || p.Thread != root || p.ReplyTo != "wsl-2" || p.Author != "claude-code/abcdef01@wsl" ||
		!reflect.DeepEqual(p.Refs, []string{"wsl-1", "gpu-9"}) || p.Created != "2026-11-18T10:03:00+01:00" {
		t.Fatalf("reply to a reply: %+v", p)
	}
	if !reflect.DeepEqual(show.Unresolved, []string{"gpu-9"}) ||
		!reflect.DeepEqual(show.Backlinks, []map[string]string{{"id": other, "to": "wsl-2", "thread": other, "title": "Release checklist"}}) {
		t.Fatalf("references: %+v %+v", show.Backlinks, show.Unresolved)
	}

	var list []threadJSON
	m.json(&list, "list")
	if len(list) != 2 || list[0].ID != other || list[1].ID != root || list[1].Author != "codex/wsl-sess@wsl" {
		t.Fatalf("list: %+v", list)
	}
	m.json(&list, "list", "-t", "review")
	if len(list) != 1 || list[0].ID != root {
		t.Fatalf("list by tag: %+v", list)
	}

	var results []struct {
		threadJSON
		Matches []struct{ ID, Field, Snippet string }
	}
	m.json(&results, "search", "冲突")
	if len(results) != 1 || len(results[0].Matches) != 2 || results[0].Matches[0].Field != "title" || results[0].Matches[1].ID != "wsl-2" {
		t.Fatalf("search: %+v", results)
	}
	m.json(&results, "search", "--body", "冲突")
	if len(results) != 1 || len(results[0].Matches) != 1 {
		t.Fatalf("body search: %+v", results)
	}

	var tags []struct {
		Tag     string
		Threads int
	}
	m.json(&tags, "tags")
	if len(tags) != 3 || tags[0].Tag != "design" {
		t.Fatalf("tags: %+v", tags)
	}

	// Text output names what matters.
	text := m.ok("show", "#wsl-2")
	for _, want := range []string{"Star 冲突处理", "▶▶ wsl-2", "reply to wsl-2", "Referenced by:", "Not here (yet): gpu-9"} {
		if !strings.Contains(text, want) {
			t.Errorf("show text lacks %q:\n%s", want, text)
		}
	}
}

func TestIdentity(t *testing.T) {
	m := newMachine(t, "wsl")
	delete(m.env, "AGORA_AUTHOR")
	_, errOut, code := m.run("post", "--title", "x", "-m", "y")
	if code != 1 || !strings.Contains(errOut, "AGORA_AUTHOR") {
		t.Fatalf("anonymous post: %d %s", code, errOut)
	}

	who := func(args ...string) map[string]string {
		var w map[string]string
		m.json(&w, append([]string{"whoami"}, args...)...)
		return w
	}
	m.env["CODEX_THREAD_ID"] = "019a8f3e-6c1d-7b20-9e4f-2d5c8a1b0e77"
	if w := who(); w["harness"] != "codex" || w["source"] != "detected" || w["author"] != "codex/019a8f3e@wsl" {
		t.Fatalf("detected Codex: %v", w)
	}
	m.env["CLAUDE_CODE_SESSION_ID"] = "95d3e5c1-bd8f-4e14-a862-f82c487a77cc"
	if w := who(); w["harness"] != "claude-code" || w["source"] != "detected" || w["author"] != "claude-code/95d3e5c1@wsl" {
		t.Fatalf("detected Claude Code: %v", w)
	}
	m.env["AGORA_AUTHOR"] = "codex/019a"
	if w := who(); w["harness"] != "codex" || w["source"] != "env" {
		t.Fatalf("env: %v", w)
	}
	if w := who("--as", "gemini/s1"); w["harness"] != "gemini" || w["source"] != "flag" || w["node"] != "wsl" {
		t.Fatalf("flag: %v", w)
	}
	if _, _, code := m.run("whoami", "--as", "nobody"); code != 2 {
		t.Fatalf("invalid identity: %d", code)
	}
}

func TestIdentityAntigravity(t *testing.T) {
	m := newMachine(t, "wsl")
	delete(m.env, "AGORA_AUTHOR")
	who := func() map[string]string {
		var w map[string]string
		m.json(&w, "whoami")
		return w
	}
	m.env["ANTIGRAVITY_CONVERSATION_ID"] = "ec33ebf9-0cba-4100-8142-c61503f6c587"
	if w := who(); w["harness"] != "antigravity" || w["session"] != "ec33ebf9-0cba-4100-8142-c61503f6c587" ||
		w["source"] != "detected" || w["author"] != "antigravity/ec33ebf9@wsl" {
		t.Fatalf("detected Antigravity: %v", w)
	}
	id := m.ok("post", "--title", "from Antigravity")
	var s struct {
		Posts []struct{ Author, Harness string }
	}
	m.json(&s, "show", strings.TrimSpace(id))
	if len(s.Posts) != 1 || s.Posts[0].Harness != "antigravity" || s.Posts[0].Author != "antigravity/ec33ebf9@wsl" {
		t.Fatalf("Antigravity's post: %+v", s.Posts)
	}
	m.env["CODEX_THREAD_ID"] = "019a8f3e-6c1d-7b20-9e4f-2d5c8a1b0e77"
	if w := who(); w["harness"] != "codex" {
		t.Fatalf("Codex's variable comes before Antigravity's: %v", w)
	}
}

func TestUsageErrors(t *testing.T) {
	m := newMachine(t, "wsl")
	m.post("--title", "t", "-m", "b")
	for _, args := range [][]string{
		{},
		{"frobnicate"},
		{"init"},
		{"post", "-m", "no title"},
		{"post", "--title", "a\nb"},
		{"post", "--title", "t", "-t", "two words"},
		{"reply"},
		{"reply", "wsl-1"},
		{"reply", "wsl-1", "-m", "  "},
		{"reply", "not-an-id!", "-m", "x"},
		{"show", "wsl-1", "wsl-2"},
		{"search"},
		{"search", "x", "--title", "--body"},
		{"list", "-n", "0"},
		{"list", "--author", "Bad"},
		{"unread", "--since", "soon"},
		{"list", "--no-such-flag"},
	} {
		out, _, code := m.run(append(args, "--json")...)
		if code != 2 {
			t.Errorf("agora %q: status %d, want 2", args, code)
		}
		if len(args) > 0 && out != "" {
			t.Errorf("agora %q: printed %q on failure", args, out)
		}
	}
	for _, args := range [][]string{{"show", "wsl-9"}, {"reply", "wsl-9", "-m", "x"}} {
		if _, errOut, code := m.run(args...); code != 1 || !strings.Contains(errOut, "not have reached this machine") {
			t.Errorf("agora %q: %d %s", args, code, errOut)
		}
	}
	if _, _, code := m.run("help"); code != 0 {
		t.Errorf("help: status %d", code)
	}
}

func TestNoBoard(t *testing.T) {
	m := newMachine(t, "")
	_, errOut, code := m.run("list")
	if code != 1 || !strings.Contains(errOut, "agora init") {
		t.Fatalf("list without a board: %d %s", code, errOut)
	}
	if err := os.MkdirAll(m.root, 0o755); err != nil {
		t.Fatal(err)
	}
	// A machine can read a board without having a node name of its own.
	if out := m.ok("list"); !strings.Contains(out, "No threads") {
		t.Fatalf("list: %s", out)
	}
	if _, errOut, code := m.run("post", "--title", "t"); code != 1 || !strings.Contains(errOut, "agora init --node") {
		t.Fatalf("post without a node name: %d %s", code, errOut)
	}
}

// TestTwoMachines posts concurrently on two machines, exchanges their files,
// and expects both to answer every query identically.
func TestTwoMachines(t *testing.T) {
	a, b := newMachine(t, "a"), newMachine(t, "b")
	a.post("--title", "seed", "-m", "the first thread", "-t", "seed")
	copyPosts(t, a, b)

	var wg sync.WaitGroup
	for _, m := range []*machine{a, b} {
		for w := range 3 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for i := range 3 {
					title := fmt.Sprintf("thread %s %d %d", m.env["AGORA_AUTHOR"], w, i)
					if _, errOut, code := m.run("post", "--title", title, "-m", "about the seed #a-1", "-t", "work"); code != 0 {
						t.Errorf("post: %s", errOut)
					}
					if _, errOut, code := m.run("reply", "a-1", "-m", "a reply"); code != 0 {
						t.Errorf("reply: %s", errOut)
					}
				}
			}()
		}
	}
	wg.Wait()
	copyPosts(t, a, b)
	copyPosts(t, b, a)

	for _, args := range [][]string{
		{"list", "-n", "100"},
		{"show", "a-1"},
		{"show", "b-3"},
		{"search", "seed", "-n", "100"},
		{"search", "reply", "-n", "100"},
		{"tags"},
	} {
		x, y := a.ok(append(args, "--json")...), b.ok(append(args, "--json")...)
		if x != y {
			t.Errorf("agora %q differs between machines:\n%s\n%s", args, x, y)
		}
	}
	var show struct {
		Posts     []postJSON
		Backlinks []map[string]string
	}
	a.json(&show, "show", "a-1")
	if len(show.Posts) != 19 || len(show.Backlinks) != 18 {
		t.Fatalf("seed thread: %d posts, %d backlinks", len(show.Posts), len(show.Backlinks))
	}
}

func unreadIDs(t *testing.T, m *machine, args ...string) (bool, []string) {
	t.Helper()
	var u struct {
		NewReader bool `json:"new_reader"`
		Posts     []postJSON
	}
	m.json(&u, append([]string{"unread"}, args...)...)
	ids := []string{}
	for _, p := range u.Posts {
		ids = append(ids, p.ID)
	}
	return u.NewReader, ids
}

func TestUnreadSurvivesOutOfOrderArrival(t *testing.T) {
	a, b := newMachine(t, "a"), newMachine(t, "b")
	reader := []string{"--as", "claude-code/reader"}
	a.post("--title", "before the reader", "-m", "old")
	copyPosts(t, a, b)

	if isNew, ids := unreadIDs(t, b, reader...); !isNew || len(ids) != 0 {
		t.Fatalf("new reader: %v %v", isNew, ids)
	}
	for i := range 3 {
		a.post("--title", fmt.Sprintf("p%d", i))
	}
	// a-4 arrives before a-2 and a-3.
	copyPosts(t, a, b, "a-4")
	if isNew, ids := unreadIDs(t, b, reader...); isNew || !reflect.DeepEqual(ids, []string{"a-4"}) {
		t.Fatalf("first arrival: %v %v", isNew, ids)
	}
	copyPosts(t, a, b)
	if _, ids := unreadIDs(t, b, append(reader, "--peek")...); !reflect.DeepEqual(ids, []string{"a-2", "a-3"}) {
		t.Fatalf("late arrivals: %v", ids)
	}
	if _, ids := unreadIDs(t, b, reader...); !reflect.DeepEqual(ids, []string{"a-2", "a-3"}) {
		t.Fatalf("peek advanced the cursor: %v", ids)
	}
	if _, ids := unreadIDs(t, b, reader...); len(ids) != 0 {
		t.Fatalf("shown twice: %v", ids)
	}

	// The reader's own posts aren't unread for it.
	b.ok("post", "--title", "mine", "--as", "claude-code/reader")
	b.ok("post", "--title", "someone else's")
	if _, ids := unreadIDs(t, b, reader...); !reflect.DeepEqual(ids, []string{"b-2"}) {
		t.Fatalf("own posts: %v", ids)
	}

}

func TestUnreadLookback(t *testing.T) {
	m := newMachine(t, "c")
	m.post("--title", "old") // 10:01
	m.mu.Lock()
	m.now = m.now.Add(time.Hour)
	m.mu.Unlock()
	m.post("--title", "recent") // 11:02

	// Unread runs at 11:03.
	if isNew, ids := unreadIDs(t, m, "--as", "codex/late", "--since", "5m"); !isNew || !reflect.DeepEqual(ids, []string{"c-2"}) {
		t.Fatalf("lookback of 5m: %v %v", isNew, ids)
	}
	if _, ids := unreadIDs(t, m, "--as", "codex/later", "--since", "1d"); !reflect.DeepEqual(ids, []string{"c-1", "c-2"}) {
		t.Fatalf("lookback of a day: %v", ids)
	}
	if _, ids := unreadIDs(t, m, "--as", "codex/latest"); len(ids) != 0 {
		t.Fatalf("no lookback: %v", ids)
	}
}

func TestUnreadSinceAfterFirstCall(t *testing.T) {
	m := newMachine(t, "c")
	m.post("--title", "earlier")

	// The new-reader hint doesn't suggest --since, which can't help once the
	// cursor is saved, nor claim to count from now when given it.
	if _, hint, code := m.run("unread", "--as", "codex/r"); code != 0 || strings.Contains(hint, "--since") {
		t.Errorf("new reader: status %d, hint %q", code, hint)
	}
	if _, hint, code := m.run("unread", "--as", "codex/s", "--since", "1d"); code != 0 || strings.Contains(hint, "--since") || strings.Contains(hint, "from now") {
		t.Errorf("new reader with --since: status %d, hint %q", code, hint)
	}
	m.post("--title", "later")

	// For a reader with a cursor, --since changes nothing but warns.
	for _, mode := range [][]string{nil, {"--json"}} {
		plain := append([]string{"unread", "--as", "codex/r", "--peek"}, mode...)
		want, wantErr, code := m.run(plain...)
		if code != 0 || wantErr != "" || !strings.Contains(want, "later") || strings.Contains(want, "earlier") {
			t.Errorf("%v: status %d, stdout %q, stderr %q", plain, code, want, wantErr)
		}
		since := append(plain, "--since", "2d")
		out, errOut, code := m.run(since...)
		if code != 0 || out != want {
			t.Errorf("%v: status %d, stdout %q, want %q", since, code, out, want)
		}
		if !strings.Contains(errOut, "--since") || !strings.Contains(errOut, "agora list") {
			t.Errorf("%v: stderr %q doesn't warn that --since changes nothing", since, errOut)
		}
	}
}

func TestPostStartsCursor(t *testing.T) {
	a, b := newMachine(t, "a"), newMachine(t, "b")
	b.post("--title", "older thread", "-m", "old")
	copyPosts(t, b, a)

	// A reply that fails starts no cursor, nor does a post that fails, here
	// for want of a node name: the reader is still new, and --since counts.
	c := newMachine(t, "")
	copyPosts(t, b, c)
	failed := []string{"--as", "claude-code/failed"}
	for _, f := range []struct {
		m    *machine
		args []string
	}{
		{a, []string{"reply", "b-9", "-m", "x"}},
		{c, []string{"post", "--title", "t", "-m", "x"}},
	} {
		if _, _, code := f.m.run(append(f.args, failed...)...); code != 1 {
			t.Errorf("%s: status %d, want 1", f.args[0], code)
		}
		if isNew, ids := unreadIDs(t, f.m, append(failed, "--since", "1d")...); !isNew || !reflect.DeepEqual(ids, []string{"b-1"}) {
			t.Errorf("after a failed %s: new reader %v, %v", f.args[0], isNew, ids)
		}
	}

	// A reader that posts or replies before its first unread starts its cursor
	// then: an answer from another machine is unread for it, and what was on
	// the board before isn't.
	for _, first := range [][]string{
		{"post", "--title", "question", "-m", "?"},
		{"reply", "b-1", "-m", "a question"},
	} {
		reader := []string{"--as", "claude-code/" + first[0]}
		out, errOut, code := a.run(append(first, reader...)...)
		if code != 0 || strings.Contains(errOut, "warning") {
			t.Fatalf("%s: status %d, stderr %q", first[0], code, errOut)
		}
		copyPosts(t, a, b)
		answer := b.reply(strings.TrimSpace(out), "-m", "answer")
		copyPosts(t, b, a)
		if isNew, ids := unreadIDs(t, a, reader...); isNew || !reflect.DeepEqual(ids, []string{answer}) {
			t.Errorf("unread after a %s: new reader %v, %v, want only %s", first[0], isNew, ids, answer)
		}
	}

	// So --since comes too late for a reader that posted first, and says so.
	reader := []string{"--as", "claude-code/since"}
	a.ok(append([]string{"post", "--title", "t"}, reader...)...)
	out, errOut, code := a.run(append([]string{"unread", "--since", "1d", "--json"}, reader...)...)
	if code != 0 || !strings.Contains(out, `"new_reader": false`) || !strings.Contains(out, `"posts": []`) ||
		!strings.Contains(errOut, "--since") || !strings.Contains(errOut, "agora list") {
		t.Errorf("unread --since after a post: status %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestPostSucceedsWithoutCursor(t *testing.T) {
	m := newMachine(t, "c")
	var r struct{ State string }
	m.json(&r, "init", "--node", "c")
	// A file where the cursors' directory belongs keeps a cursor from being
	// saved. The post is published all the same, so the command succeeds and
	// only warns, rather than have the caller post twice.
	if err := os.WriteFile(filepath.Join(r.State, "cursors"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		want string
		args []string
	}{
		{"c-1\n", []string{"post", "--title", "question"}},
		{`"id": "c-2"`, []string{"reply", "c-1", "-m", "again", "--json"}},
	} {
		out, errOut, code := m.run(append(step.args, "--as", "claude-code/q")...)
		if code != 0 || !strings.Contains(out, step.want) || !strings.Contains(errOut, "warning") {
			t.Errorf("%s: status %d, stdout %q, stderr %q", step.args[0], code, out, errOut)
		}
	}
}

func TestRebuildIsIdentical(t *testing.T) {
	m := newMachine(t, "wsl")
	root := m.post("--title", "Rebuild 索引", "-t", "index", "-m", "body #wsl-9")
	m.reply(root, "-m", "a reply mentioning 索引", "-t", "reply")
	m.post("--title", "another", "-m", "see #wsl-1")
	unreadIDs(t, m, "--as", "codex/watcher")
	m.post("--title", "after the watcher started", "--as", "claude-code/x")

	queries := [][]string{{"list"}, {"show", root}, {"show", "wsl-3"}, {"search", "索引"}, {"search", "another", "--title"}, {"tags"}}
	before := map[string]string{}
	for _, q := range queries {
		before[strings.Join(q, " ")] = m.ok(append(q, "--json")...)
	}
	matches, err := filepath.Glob(filepath.Join(m.env["XDG_STATE_HOME"], "agora", "*", "index.db*"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("no index to delete: %v", err)
	}
	for _, p := range matches {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range queries {
		if got := m.ok(append(q, "--json")...); got != before[strings.Join(q, " ")] {
			t.Errorf("agora %q changed after rebuilding:\n%s\n%s", q, before[strings.Join(q, " ")], got)
		}
	}
	if _, ids := unreadIDs(t, m, "--as", "codex/watcher"); !reflect.DeepEqual(ids, []string{"wsl-4"}) {
		t.Fatalf("the cursor didn't survive the rebuild: %v", ids)
	}
}

func TestParseSince(t *testing.T) {
	for s, want := range map[string]time.Duration{"90m": 90 * time.Minute, "2d": 48 * time.Hour, "1.5d": 36 * time.Hour, "0s": 0} {
		if got, err := parseSince(s); err != nil || got != want {
			t.Errorf("parseSince(%q) = %v, %v", s, got, err)
		}
	}
	for _, s := range []string{"", "d", "-1d", "-5m", "soon"} {
		if _, err := parseSince(s); err == nil {
			t.Errorf("parseSince(%q) succeeded", s)
		}
	}
}

func TestBodyInput(t *testing.T) {
	m := newMachine(t, "wsl")
	body := func(id string) string {
		var show struct{ Posts []postJSON }
		m.json(&show, "show", id)
		for _, p := range show.Posts {
			if p.ID == id {
				return p.Body
			}
		}
		t.Fatalf("%s isn't in its thread", id)
		return ""
	}

	// A file saved on Windows, with a byte order mark and CRLF.
	file := filepath.Join(testutil.TempDir(t), "body.md")
	if err := os.WriteFile(file, []byte("\xef\xbb\xbf第一行\r\nsecond\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := m.post("--title", "from a file", "-f", file)
	if got := body(id); got != "第一行\nsecond\n" {
		t.Fatalf("body from a file: %q", got)
	}

	stdin := "from standard input\r\n"
	out, errOut, code := m.runIn(&stdin, "reply", id, "-f", "-", "--json")
	if code != 0 {
		t.Fatalf("-f -: %s", errOut)
	}
	var r struct{ ID string }
	if err := json.Unmarshal([]byte(out), &r); err != nil || body(r.ID) != "from standard input\n" {
		t.Fatalf("body from standard input: %q %v", body(r.ID), err)
	}

	// Without -m or -f, standard input isn't read, and the body is empty.
	if id := m.post("--title", "no body"); body(id) != "" {
		t.Fatalf("implicit body: %q", body(id))
	}

	bad := filepath.Join(testutil.TempDir(t), "latin1.txt")
	if err := os.WriteFile(bad, []byte("caf\xe9"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"post", "--title", "t", "-m", "x", "-f", file},
		{"post", "--title", "t", "-m", "caf\xe9"},
		{"post", "--title", "caf\xe9"},
		{"post", "--title", "t", "-f", bad},
		{"post", "--title", "t", "-f", ""},
	} {
		if _, _, code := m.run(args...); code != 2 {
			t.Errorf("agora %q: status %d, want 2", args, code)
		}
	}
	if _, _, code := m.run("post", "--title", "t", "-f", filepath.Join(testutil.TempDir(t), "missing")); code != 1 {
		t.Errorf("a missing file: status %d, want 1", code)
	}
}

func TestDefaultLocationsOnWindows(t *testing.T) {
	m := newMachine(t, "")
	profile, local := filepath.Join(testutil.TempDir(t), "profile"), filepath.Join(testutil.TempDir(t), "local")
	m.goos = "windows"
	m.env = map[string]string{"USERPROFILE": profile, "LOCALAPPDATA": local, "HOME": "/elsewhere", "AGORA_AUTHOR": "codex/s"}
	// init in %USERPROFILE% creates the board that commands fall back on.
	m.dir = profile
	var init struct{ Board, Node, State string }
	m.json(&init, "init", "--node", "win")
	// init resolves symbolic links in the board's path, such as macOS's /var
	// and Windows' short names in %TEMP%, so resolve the expected path too.
	if want, err := filepath.EvalSymlinks(filepath.Join(profile, ".agora")); err != nil || init.Board != want {
		t.Fatalf("board %s, want %%USERPROFILE%%\\.agora (%s, %v)", init.Board, want, err)
	}
	if rel, err := filepath.Rel(filepath.Join(local, "agora"), init.State); err != nil || strings.HasPrefix(rel, "..") {
		t.Fatalf("state %s, want it under %%LOCALAPPDATA%%", init.State)
	}
	// Without a board above the working directory, here an unknown one,
	// commands fall back on it through %USERPROFILE%, not $HOME.
	m.dir = ""
	if m.post("--title", "hello from Windows") != "win-1" {
		t.Fatal("posting with default locations")
	}
}

// project makes a temporary home directory with a project in it, and a
// machine whose working directory is the project, without AGORA_DIR.
func project(t *testing.T) (m *machine, home, proj string) {
	t.Helper()
	m = newMachine(t, "")
	delete(m.env, "AGORA_DIR")
	home = m.env["HOME"]
	proj = filepath.Join(home, "src", "proj")
	if err := os.MkdirAll(filepath.Join(proj, "internal", "cli"), 0o755); err != nil {
		t.Fatal(err)
	}
	m.dir = proj
	return m, home, proj
}

func whoamiBoard(t *testing.T, m *machine) (root, node string) {
	t.Helper()
	var w map[string]string
	m.json(&w, "whoami")
	return w["board"], w["node"]
}

// ambient returns the board, resolved, that a search from dir finds above the
// temporary directories. Tests expect it where they would otherwise expect
// none. There normally is none, but on Windows, whose %TEMP% is beneath the
// home directory, it is a real %USERPROFILE%\.agora.
func ambient(t *testing.T, dir string) string {
	t.Helper()
	found, err := board.Find(dir, "")
	if err != nil || found == "" {
		if err != nil {
			t.Fatal(err)
		}
		return ""
	}
	resolved, err := filepath.EvalSymlinks(found)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestBoardInWorkingDirectory(t *testing.T) {
	m, home, proj := project(t)
	above := ambient(t, home)

	// Without a board, whoami says so, and other commands fail and say how
	// to create one.
	if root, node := whoamiBoard(t, m); root != above || node != "" {
		t.Fatalf("whoami without a board: board %q, node %q", root, node)
	}
	if above == "" {
		if _, errOut, code := m.run("list"); code != 1 || !strings.Contains(errOut, "agora init") {
			t.Fatalf("list without a board: status %d, %s", code, errOut)
		}
	}

	// Like git init, init creates the board in the working directory, with a
	// .gitignore that keeps it out of the project's repository.
	var init struct{ Board, Node, State string }
	m.json(&init, "init", "--node", "wsl")
	want, err := filepath.EvalSymlinks(filepath.Join(proj, ".agora"))
	if err != nil || init.Board != want {
		t.Fatalf("init made %s, want %s (%v)", init.Board, want, err)
	}
	if data, err := os.ReadFile(filepath.Join(want, ".gitignore")); err != nil || string(data) != "*\n" {
		t.Fatalf(".gitignore: %q, %v", data, err)
	}

	// Commands find it from beneath the project and report it.
	m.dir = filepath.Join(proj, "internal", "cli")
	if root, node := whoamiBoard(t, m); root != want || node != "wsl" {
		t.Fatalf("whoami beneath the project: board %q, node %q", root, node)
	}
	var r struct{ ID, Board string }
	m.json(&r, "post", "--title", "found from a subdirectory")
	if r.ID != "wsl-1" || r.Board != want {
		t.Fatalf("post: %+v", r)
	}
	m.json(&r, "reply", "wsl-1", "-m", "and replied to")
	if r.ID != "wsl-2" || r.Board != want {
		t.Fatalf("reply: %+v", r)
	}
	// Text output keeps the id alone on standard output and names the board
	// on standard error.
	out, errOut, code := m.run("post", "--title", "in text")
	if code != 0 || out != "wsl-3\n" || !strings.Contains(errOut, want) {
		t.Fatalf("post in text: status %d, stdout %q, stderr %q", code, out, errOut)
	}

	// ~/.agora serves working directories without a board of their own, and
	// the nearer board wins.
	m.dir = home
	m.ok("init", "--node", "wsl")
	m.dir = testutil.TempDir(t)
	homeBoard, err := filepath.EvalSymlinks(filepath.Join(home, ".agora"))
	if err != nil {
		t.Fatal(err)
	}
	if above != "" {
		homeBoard = above
	}
	if root, _ := whoamiBoard(t, m); root != homeBoard {
		t.Fatalf("outside the home directory: board %q, want %q", root, homeBoard)
	}
	m.dir = proj
	if root, _ := whoamiBoard(t, m); root != want {
		t.Fatalf("in the project: board %q, want %q", root, want)
	}

	// --board and AGORA_DIR come before the search.
	other := filepath.Join(testutil.TempDir(t), "other")
	m.json(&init, "init", "--node", "wsl", "--board", other)
	m.env["AGORA_DIR"] = other
	if root, _ := whoamiBoard(t, m); root != init.Board {
		t.Fatalf("AGORA_DIR: board %q, want %q", root, init.Board)
	}
}

func TestInitHintNamesTheBoard(t *testing.T) {
	// The project's board has arrived from another machine, a, and this one,
	// b, works beneath the project without having run init for it.
	a, _, proj := project(t)
	var init struct{ Board string }
	a.json(&init, "init", "--node", "a")
	b := newMachine(t, "")
	delete(b.env, "AGORA_DIR")
	b.dir = filepath.Join(proj, "internal", "cli")

	// The hint names the board, so that following it from here doesn't
	// create another board in the working directory.
	hint := "agora init --node NAME --board " + init.Board
	if _, errOut, code := b.run("post", "--title", "x"); code != 1 || !strings.Contains(errOut, hint) {
		t.Fatalf("post without a node: status %d, %s", code, errOut)
	}
	if out := b.ok("whoami"); !strings.Contains(out, hint) {
		t.Fatalf("whoami without a node:\n%s", out)
	}
	b.ok("init", "--node", "b", "--board", init.Board)
	if _, err := os.Stat(filepath.Join(b.dir, ".agora")); !os.IsNotExist(err) {
		t.Fatalf("init made a board in the working directory: %v", err)
	}
	var r struct{ ID, Board string }
	b.json(&r, "post", "--title", "set up from a subdirectory")
	if r.ID != "b-1" || r.Board != init.Board {
		t.Fatalf("post after following the hint: %+v", r)
	}
}

// linkWorktree lays out the files that git worktree add leaves for a linked
// worktree at wt of the main worktree main.
func linkWorktree(t *testing.T, main, wt string) {
	t.Helper()
	admin := filepath.Join(main, ".git", "worktrees", filepath.Base(wt))
	for path, data := range map[string]string{
		filepath.Join(admin, "commondir"): "../..\n",
		filepath.Join(admin, "gitdir"):    filepath.ToSlash(filepath.Join(wt, ".git")) + "\n",
		filepath.Join(wt, ".git"):         "gitdir: " + filepath.ToSlash(admin) + "\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBoardInLinkedWorktree(t *testing.T) {
	m, _, proj := project(t)
	wt := filepath.Join(testutil.TempDir(t), "fix")
	linkWorktree(t, proj, wt)
	if err := os.MkdirAll(filepath.Join(wt, "internal", "cli"), 0o755); err != nil {
		t.Fatal(err)
	}

	// init refuses to create a board inside a linked worktree, and points to
	// the main worktree.
	m.dir = wt
	for _, args := range [][]string{{}, {"--board", filepath.Join(wt, "internal", ".agora")}} {
		_, errOut, code := m.run(append([]string{"init", "--node", "wsl"}, args...)...)
		if code != 1 || !strings.Contains(errOut, "linked worktree") || !strings.Contains(errOut, proj) {
			t.Errorf("init %q in a linked worktree: status %d, %s", args, code, errOut)
		}
	}
	if entries, err := filepath.Glob(filepath.Join(wt, "*", ".agora")); err != nil || len(entries) != 0 {
		t.Fatalf("boards in the worktree: %v %v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(wt, ".agora")); !os.IsNotExist(err) {
		t.Fatalf("a board in the worktree: %v", err)
	}

	// The worktree's commands use the main worktree's board.
	m.dir = proj
	var init struct{ Board string }
	m.json(&init, "init", "--node", "wsl")
	m.dir = filepath.Join(wt, "internal", "cli")
	var r struct{ ID, Board string }
	m.json(&r, "post", "--title", "from the worktree")
	if r.ID != "wsl-1" || r.Board != init.Board {
		t.Fatalf("post from the worktree: %+v, want the board %s", r, init.Board)
	}

	// A board named outside the worktree is fine.
	m.ok("init", "--node", "wsl", "--board", filepath.Join(testutil.TempDir(t), ".agora"))
}
