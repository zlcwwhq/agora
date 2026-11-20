package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zlcwwhq/agora/internal/testdir"
)

// testEnv is a process environment of its own: a working directory, a home,
// and only the variables in vars, so that the real environment, such as a
// harness's session variable or a ~/.agora, never leaks in.
type testEnv struct {
	dir, home string
	vars      map[string]string
}

func newEnv(t *testing.T) *testEnv {
	root := testdir.New(t)
	// The working directory is in the home directory, so that the search
	// for .agora stops there, below the real home of the machine that runs
	// the tests.
	e := &testEnv{dir: filepath.Join(root, "home", "proj"), home: filepath.Join(root, "home"), vars: map[string]string{}}
	for _, d := range []string{e.dir, e.home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	e.vars["HOME"] = e.home
	e.vars["USERPROFILE"] = e.home
	e.vars["XDG_STATE_HOME"] = filepath.Join(root, "state")
	e.vars["LOCALAPPDATA"] = filepath.Join(root, "state")
	return e
}

func (e *testEnv) env(args []string, stdin io.Reader, stdout, stderr io.Writer) Env {
	return Env{
		Args:     args,
		Getenv:   func(k string) string { return e.vars[k] },
		Dir:      e.dir,
		GOOS:     "linux",
		Hostname: func() (string, error) { return "testbox", nil },
		Stdin:    stdin,
		Stdout:   stdout,
		Stderr:   stderr,
		Now:      time.Now,
		Version:  "v0.5.0-test",
	}
}

type result struct {
	out, err string
	code     int
}

func (e *testEnv) run(args ...string) result {
	var out, errb bytes.Buffer
	code := Run(e.env(args, strings.NewReader(""), &out, &errb))
	return result{out.String(), errb.String(), code}
}

func (e *testEnv) ok(t *testing.T, args ...string) string {
	t.Helper()
	r := e.run(args...)
	if r.code != 0 {
		t.Fatalf("agora %s: exit %d: %s", strings.Join(args, " "), r.code, r.err)
	}
	return r.out
}

func (e *testEnv) json(t *testing.T, v any, args ...string) {
	t.Helper()
	out := e.ok(t, append(args, "--json")...)
	if err := json.Unmarshal([]byte(out), v); err != nil {
		t.Fatalf("agora %s: %v in %q", strings.Join(args, " "), err, out)
	}
}

// serve runs agora serve on a free port with a database of its own, until
// the test ends, and points the environment at it.
func (e *testEnv) serve(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	pr, pw := io.Pipe()
	var errb bytes.Buffer
	env := e.env(append([]string{"serve", "--addr", "127.0.0.1:0", "--db", filepath.Join(testdir.New(t), "board.db"), "--json"}, args...), nil, pw, &errb)
	env.Context = ctx
	done := make(chan int, 1)
	go func() {
		code := Run(env)
		_ = pw.Close()
		done <- code
	}()
	line, err := bufio.NewReader(pr).ReadString('\n')
	if err != nil {
		cancel()
		t.Fatalf("serve: %v: %s", err, errb.String())
	}
	go func() { _, _ = io.Copy(io.Discard, pr) }()
	t.Cleanup(func() {
		cancel()
		if code := <-done; code != 0 {
			t.Errorf("serve exited %d: %s", code, errb.String())
		}
	})
	var out struct{ URL, Author string }
	if err := json.Unmarshal([]byte(line), &out); err != nil {
		t.Fatalf("serve printed %q", line)
	}
	addr := strings.TrimSuffix(strings.TrimPrefix(out.URL, "http://"), "/")
	e.vars["AGORA_SERVER"] = addr
	return out.Author
}

type published struct {
	ID, Thread int64
	URL        string
}

func TestEndToEnd(t *testing.T) {
	e := newEnv(t)
	e.serve(t)
	e.vars["CODEX_THREAD_ID"] = "019a2b3c-codex"
	if r := e.run("list"); r.code != 1 || !strings.Contains(r.err, "no board: pass --board ID") {
		t.Errorf("list without a board: %+v", r)
	}
	e.ok(t, "boards", "create", "proj")
	e.vars["AGORA_BOARD"] = "proj"

	var root published
	e.json(t, &root, "post", "--title", "Plan", "-t", "Design", "-m", "the plan")
	if root.ID != 1 || root.Thread != 1 || !strings.HasSuffix(root.URL, "/b/proj/t/1#1") {
		t.Fatalf("post = %+v", root)
	}
	var rep published
	e.json(t, &rep, "reply", "#1", "--as", "human/alice", "-m", "see #1")
	if rep.Thread != 1 || !strings.HasSuffix(rep.URL, "/b/proj/t/1#2") {
		t.Fatalf("reply = %+v", rep)
	}

	var show struct {
		Focus  int64
		Thread struct {
			Title   string
			Replies int
			Tags    []string
		}
		Posts []struct {
			ID      int64
			Author  string
			ReplyTo *int64 `json:"reply_to"`
			Body    string
			Refs    []int64
			Editor  string
		}
	}
	e.json(t, &show, "show", "2")
	if show.Focus != 2 || show.Thread.Title != "Plan" || show.Thread.Replies != 1 || len(show.Posts) != 2 {
		t.Fatalf("show = %+v", show)
	}
	if show.Posts[0].Author != "codex/019a2b3c@testbox" || show.Posts[1].Author != "human/alice@testbox" || len(show.Posts[1].Refs) != 1 {
		t.Errorf("posts = %+v", show.Posts)
	}

	// The author edits from any machine; a person edits anything.
	e.vars["AGORA_NODE"] = "elsewhere"
	var edited struct {
		ID, Thread int64
		Edited     string
	}
	e.json(t, &edited, "edit", "1", "--title", "Plan B", "--no-tags")
	if edited.ID != 1 || edited.Edited == "" {
		t.Errorf("edit = %+v", edited)
	}
	if r := e.run("edit", "2", "-m", "mine now"); r.code != 1 || !strings.Contains(r.err, "only its author or a person") {
		t.Errorf("edit of another's post: %+v", r)
	}
	e.ok(t, "edit", "1", "--as", "human/bob", "-f", "-")
	e.json(t, &show, "show", "1")
	if show.Thread.Title != "Plan B" || len(show.Thread.Tags) != 0 || show.Posts[0].Body != "" || show.Posts[0].Editor != "human/bob@elsewhere" {
		t.Errorf("after edits: %+v", show)
	}

	var threads []struct {
		ID    int64
		Title string
	}
	e.json(t, &threads, "list")
	if len(threads) != 1 || threads[0].Title != "Plan B" {
		t.Errorf("list = %+v", threads)
	}
	var results []struct {
		ID      int64
		Matches []struct{ ID int64 }
	}
	e.json(t, &results, "search", "SEE", "--body")
	if len(results) != 1 || results[0].Matches[0].ID != 2 {
		t.Errorf("search = %+v", results)
	}

	// Unread: alice has read up to her reply; codex's next post is new.
	e.ok(t, "reply", "2", "-m", "answer")
	var unread struct {
		NewReader bool `json:"new_reader"`
		Posts     []struct{ ID int64 }
	}
	e.json(t, &unread, "unread", "--as", "human/alice")
	if unread.NewReader || len(unread.Posts) != 1 || unread.Posts[0].ID != 3 {
		t.Errorf("unread = %+v", unread)
	}
	r := e.run("unread", "--as", "human/alice", "--since", "1d")
	if r.code != 0 || !strings.Contains(r.err, "--since counts only for a new reader") {
		t.Errorf("unread --since of an old reader: %+v", r)
	}

	var removed struct {
		ID      int64
		Removed []int64
	}
	if r := e.run("delete", "2"); r.code != 1 {
		t.Errorf("delete of another's post: %+v", r)
	}
	e.json(t, &removed, "delete", "1")
	if removed.ID != 1 || len(removed.Removed) != 3 {
		t.Errorf("delete = %+v", removed)
	}
	if r := e.run("show", "2", "--json"); r.code != 1 || r.out != "" || !strings.Contains(r.err, "no post #2 on board proj") {
		t.Errorf("show after delete: %+v", r)
	}
	if out := e.ok(t, "list"); !strings.Contains(out, "no threads") {
		t.Errorf("list after delete: %q", out)
	}
}

func TestIdentityOrder(t *testing.T) {
	e := newEnv(t)
	cfg := filepath.Join(e.dir, ".agora")
	e.ok(t, "init", "--node", "laptop", "--author", "human/alice", "--server", "127.0.0.1:9", "--board", "proj")
	if data, _ := os.ReadFile(cfg); !strings.Contains(string(data), `author = "human/alice"`) || !strings.Contains(string(data), `id = "proj"`) {
		t.Fatalf("config:\n%s", data)
	}
	var who struct{ Author, Source, Config, Server, Node, Board string }
	e.json(t, &who, "whoami")
	if who.Author != "human/alice@laptop" || who.Source != "config" || who.Config != cfg || who.Server != "http://127.0.0.1:9" || who.Board != "proj" {
		t.Errorf("config identity = %+v", who)
	}
	e.vars["CLAUDE_CODE_SESSION_ID"] = "95d3e5c1-claude"
	e.json(t, &who, "whoami")
	if who.Author != "claude-code/95d3e5c1@laptop" || who.Source != "detected" {
		t.Errorf("detected identity = %+v", who)
	}
	e.vars["AGORA_AUTHOR"] = "codex/abc"
	e.json(t, &who, "whoami")
	if who.Author != "codex/abc@laptop" || who.Source != "env" {
		t.Errorf("env identity = %+v", who)
	}
	e.json(t, &who, "whoami", "--as", "human/bob")
	if who.Author != "human/bob@laptop" || who.Source != "flag" {
		t.Errorf("flag identity = %+v", who)
	}
	if r := e.run("init"); r.code != 1 || !strings.Contains(r.err, "exists") {
		t.Errorf("init over a file: %+v", r)
	}
	e.ok(t, "init", "--force")
	delete(e.vars, "AGORA_AUTHOR")
	delete(e.vars, "CLAUDE_CODE_SESSION_ID")
	e.json(t, &who, "whoami", "--board", "other")
	if who.Author != "" || who.Node != "testbox" || who.Server != "http://127.0.0.1:7777" || who.Board != "other" {
		t.Errorf("after init --force = %+v", who)
	}
	if r := e.run("post", "--title", "x", "--board", "b"); r.code != 1 || !strings.Contains(r.err, "no identity") {
		t.Errorf("post without identity: %+v", r)
	}
}

func TestUsageErrors(t *testing.T) {
	e := newEnv(t)
	e.vars["AGORA_AUTHOR"] = "human/alice"
	for _, args := range [][]string{
		{"nope"},
		{"edit", "1"},
		{"edit", "1", "-t", "a", "--no-tags"},
		{"edit", "x", "-m", "b"},
		{"reply", "1"},
		{"reply", "1", "-m", "a", "-f", "b"},
		{"post", "-m", "body"},
		{"show"},
		{"list", "-n", "0"},
		{"list", "--author", "Bad"},
		{"search", "x", "--title", "--body"},
		{"unread", "--since", "1w"},
		{"--version", "x"},
		{"boards", "make", "x"},
		{"boards", "create", "Bad"},
		{"boards", "--title", "x"},
		{"init", "--board", "Bad"},
	} {
		if r := e.run(args...); r.code != 2 {
			t.Errorf("agora %s: exit %d, want 2: %s", strings.Join(args, " "), r.code, r.err)
		}
	}
	if out := e.ok(t, "--version"); out != "agora v0.5.0-test\n" {
		t.Errorf("--version = %q", out)
	}
}

func TestUnreachableServer(t *testing.T) {
	e := newEnv(t)
	e.vars["AGORA_SERVER"] = "127.0.0.1:9"
	e.vars["AGORA_BOARD"] = "b"
	r := e.run("list", "--json")
	if r.code != 1 || r.out != "" || !strings.Contains(r.err, "can't reach the agora server at http://127.0.0.1:9") {
		t.Errorf("list without a server: %+v", r)
	}
}

func TestOldBoardDirectory(t *testing.T) {
	e := newEnv(t)
	if err := os.MkdirAll(filepath.Join(e.home, ".agora", "posts", "wsl"), 0o755); err != nil {
		t.Fatal(err)
	}
	if r := e.run("whoami"); r.code != 1 || !strings.Contains(r.err, "a board of agora v0.4 or earlier") {
		t.Errorf("whoami with an old board: %+v", r)
	}
	if r := e.run("init", "--config", filepath.Join(e.home, ".agora")); r.code != 1 || !strings.Contains(r.err, "directory") {
		t.Errorf("init over an old board: %+v", r)
	}
}

func TestServeForms(t *testing.T) {
	e := newEnv(t)
	e.vars["CLAUDE_CODE_SESSION_ID"] = "95d3e5c1-claude"
	if author := e.serve(t); author != "" {
		t.Errorf("serve with a detected identity acts as %q", author)
	}
	e2 := newEnv(t)
	if author := e2.serve(t, "--as", "human/alice"); author != "human/alice@testbox" {
		t.Errorf("serve --as human/alice acts as %q", author)
	}
}

func TestServeLocksDatabase(t *testing.T) {
	e := newEnv(t)
	db := filepath.Join(testdir.New(t), "board.db")
	ctx, cancel := context.WithCancel(context.Background())
	pr, pw := io.Pipe()
	env := e.env([]string{"serve", "--addr", "127.0.0.1:0", "--db", db, "--json"}, nil, pw, io.Discard)
	env.Context = ctx
	done := make(chan int, 1)
	go func() { done <- Run(env); _ = pw.Close() }()
	if _, err := bufio.NewReader(pr).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.Copy(io.Discard, pr) }()
	r := e.run("serve", "--addr", "127.0.0.1:0", "--db", db)
	cancel()
	if code := <-done; code != 0 {
		t.Errorf("first serve exited %d", code)
	}
	if r.code != 1 || !strings.Contains(r.err, "in use") {
		t.Errorf("second serve on the database: %+v", r)
	}
}

func TestBoards(t *testing.T) {
	e := newEnv(t)
	e.serve(t)
	e.vars["AGORA_AUTHOR"] = "human/alice"
	if out := e.ok(t, "boards"); !strings.Contains(out, "no boards") {
		t.Errorf("boards of a new server: %q", out)
	}
	var created struct{ ID, Title, URL string }
	e.json(t, &created, "boards", "create", "alpha", "--title", "Alpha project")
	if created.ID != "alpha" || created.Title != "Alpha project" || !strings.HasSuffix(created.URL, "/b/alpha/") {
		t.Errorf("create = %+v", created)
	}
	e.ok(t, "boards", "create", "beta")
	if r := e.run("boards", "create", "beta"); r.code != 1 || !strings.Contains(r.err, `board "beta" exists`) {
		t.Errorf("creating a taken board: %+v", r)
	}

	// Each board numbers its own posts; --board beats AGORA_BOARD.
	e.vars["AGORA_BOARD"] = "alpha"
	var pub published
	e.json(t, &pub, "post", "--title", "In alpha", "-m", "x")
	e.json(t, &pub, "post", "--title", "In beta", "-m", "x", "--board", "beta")
	if pub.ID != 1 || !strings.Contains(pub.URL, "/b/beta/t/1#1") {
		t.Errorf("beta's first post = %+v", pub)
	}
	var boards []struct {
		ID           string
		Threads      int
		LastActivity string `json:"last_activity"`
	}
	e.json(t, &boards, "boards")
	if len(boards) != 2 || boards[0].ID != "beta" || boards[0].Threads != 1 || boards[1].ID != "alpha" || boards[1].LastActivity == "" {
		t.Errorf("boards = %+v", boards)
	}
	if r := e.run("list", "--board", "gamma"); r.code != 1 || !strings.Contains(r.err, `no board "gamma"`) {
		t.Errorf("list on an unknown board: %+v", r)
	}
	if r := e.run("post", "--title", "x", "--board", "gamma"); r.code != 1 || !strings.Contains(r.err, `no board "gamma"`) {
		t.Errorf("post on an unknown board: %+v", r)
	}
}
