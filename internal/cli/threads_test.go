package cli

import (
	"bytes"
	"context"
	"mime"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zlcwwhq/agora/internal/client"
	"github.com/zlcwwhq/agora/internal/post"
	"github.com/zlcwwhq/agora/internal/store"
)

// threadsEnv serves a server of its own with the board proj, which commands
// use as codex.
func threadsEnv(t *testing.T) *testEnv {
	t.Helper()
	e := newEnv(t)
	e.serve(t)
	e.vars["AGORA_AUTHOR"] = "codex/019a2b3c-codex"
	e.ok(t, "boards", "create", "proj")
	e.vars["AGORA_BOARD"] = "proj"
	return e
}

// writeTree writes files, by their paths in the working directory.
func (e *testEnv) writeTree(t *testing.T, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(e.dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// mustFail runs a command that must exit with code, saying want.
func (e *testEnv) mustFail(t *testing.T, code int, want string, args ...string) {
	t.Helper()
	r := e.run(args...)
	if r.code != code || !strings.Contains(r.err, want) || (code == 1 && strings.Contains(strings.Join(args, " "), "--json") && r.out != "") {
		t.Errorf("agora %s: exit %d, %q; want exit %d saying %q", strings.Join(args, " "), r.code, r.err, code, want)
	}
}

type threadShape struct {
	ID             int64
	Title, State   string
	Pinned, Locked bool
	Replies        int
}

func listedThreads(t *testing.T, e *testEnv, args ...string) []int64 {
	t.Helper()
	var ts []threadShape
	e.json(t, &ts, args...)
	ids := []int64{}
	for _, th := range ts {
		ids = append(ids, th.ID)
	}
	return ids
}

func TestThreadCommand(t *testing.T) {
	e := threadsEnv(t)
	e.ok(t, "post", "--title", "Question", "-m", "how?")
	e.ok(t, "reply", "1", "--as", "claude-code/95d3e5c1", "-m", "like so")
	e.ok(t, "post", "--title", "Other", "-m", "something else")

	// Without flags, it reads, without an identity.
	delete(e.vars, "AGORA_AUTHOR")
	var th threadShape
	e.json(t, &th, "thread", "#2")
	if th.ID != 1 || th.Title != "Question" || th.State != "open" || th.Pinned || th.Locked || th.Replies != 1 {
		t.Errorf("thread = %+v", th)
	}
	if out := e.ok(t, "thread", "1"); !strings.Contains(out, "open · codex/019a2b3c@testbox · 1 reply") {
		t.Errorf("thread prints %q", out)
	}
	e.mustFail(t, 1, "no identity", "thread", "1", "--resolve")
	e.vars["AGORA_AUTHOR"] = "codex/019a2b3c-codex"

	e.mustFail(t, 1, "only its author or a person", "thread", "2", "--resolve", "--as", "claude-code/95d3e5c1", "--json")
	e.json(t, &th, "thread", "2", "--resolve")
	if th.State != "resolved" {
		t.Errorf("resolved thread = %+v", th)
	}
	e.mustFail(t, 1, "only a person", "thread", "1", "--pin")
	e.json(t, &th, "thread", "1", "--pin", "--lock", "--reopen", "--as", "human/alice")
	if th.State != "open" || !th.Pinned || !th.Locked {
		t.Errorf("a person's change = %+v", th)
	}
	if out := e.ok(t, "list"); !strings.Contains(out, "pinned · locked · 1 reply") {
		t.Errorf("list prints %q", out)
	}
	e.mustFail(t, 1, "locked", "reply", "2", "-m", "more")
	e.ok(t, "reply", "2", "-m", "more", "--as", "human/alice")
	e.json(t, &th, "thread", "1", "--unlock", "--unpin", "--resolve", "--as", "human/alice")
	if th.State != "resolved" || th.Pinned || th.Locked {
		t.Errorf("after undoing = %+v", th)
	}
	for _, pair := range [][]string{{"--resolve", "--reopen"}, {"--pin", "--unpin"}, {"--lock", "--unlock"}} {
		e.mustFail(t, 2, "exclude each other", append([]string{"thread", "1"}, pair...)...)
	}
	e.mustFail(t, 2, "usage", "thread")
	e.mustFail(t, 1, "no post #9", "thread", "9")
}

func TestStateAndSkip(t *testing.T) {
	e := threadsEnv(t)
	for _, title := range []string{"one", "two", "three"} {
		e.ok(t, "post", "--title", title, "-m", "a common word")
	}
	e.ok(t, "thread", "1", "--pin", "--as", "human/alice")
	e.ok(t, "thread", "2", "--resolve")
	for _, c := range []struct {
		args []string
		want []int64
	}{
		{[]string{"list"}, []int64{1, 3, 2}},
		{[]string{"list", "--state", "open"}, []int64{1, 3}},
		{[]string{"list", "--state", "resolved"}, []int64{2}},
		{[]string{"list", "--skip", "1", "-n", "1"}, []int64{3}},
		{[]string{"list", "--skip", "3"}, []int64{}},
		{[]string{"search", "common"}, []int64{3, 2, 1}},
		{[]string{"search", "common", "--state", "open", "--skip", "1"}, []int64{1}},
	} {
		if got := listedThreads(t, e, c.args...); !slices.Equal(got, c.want) {
			t.Errorf("agora %s = %v, want %v", strings.Join(c.args, " "), got, c.want)
		}
	}
	for _, args := range [][]string{{"list", "--state", "closed"}, {"list", "--skip", "-1"}, {"search", "x", "--state", "Open"}, {"search", "x", "--skip", "-2"}} {
		e.mustFail(t, 2, "", args...)
	}
}

type attachedPost struct {
	ID          int64
	Edited      string
	Attachments []post.Attachment
}

// postOf returns post id as show prints it.
func postOf(t *testing.T, e *testEnv, id int64) attachedPost {
	t.Helper()
	var show struct{ Posts []attachedPost }
	e.json(t, &show, "show", strconv.FormatInt(id, 10))
	for _, p := range show.Posts {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("no post %d in %+v", id, show)
	return attachedPost{}
}

func TestAttachFiles(t *testing.T) {
	e := threadsEnv(t)
	e.writeTree(t, map[string]string{"src/build.log": "line\n", "src/shot.png": "\x89PNG", "other/build.log": "x", "notes": "plain", "src/b.txt": "b"})
	logType := mime.TypeByExtension(".log")
	if logType == "" {
		logType = "application/octet-stream"
	}

	var pub published
	e.json(t, &pub, "post", "--title", "With files", "-m", "see", "-a", "src/build.log", "-a", filepath.Join(e.dir, "src", "shot.png"))
	p := postOf(t, e, pub.ID)
	want := []post.Attachment{{Name: "build.log", Type: logType, Size: 5}, {Name: "shot.png", Type: "image/png", Size: 4}}
	if !slices.Equal(p.Attachments, want) {
		t.Errorf("attachments = %+v, want %+v", p.Attachments, want)
	}
	e.json(t, &pub, "reply", "1", "-m", "and this", "-a", "src/b.txt")
	if p := postOf(t, e, pub.ID); len(p.Attachments) != 1 || p.Attachments[0].Type != mime.TypeByExtension(".txt") {
		t.Errorf("reply's attachments = %+v", p.Attachments)
	}

	// Every file is checked before anything is written.
	big := filepath.Join(e.dir, "big.bin")
	if f, err := os.Create(big); err != nil || f.Truncate(post.MaxAttachment+1) != nil || f.Close() != nil {
		t.Fatal("can't make big.bin")
	}
	e.mustFail(t, 1, "missing.txt", "post", "--title", "x", "-m", "x", "-a", "src/b.txt", "-a", "missing.txt")
	e.mustFail(t, 1, "at most 10 MiB", "post", "--title", "x", "-m", "x", "-a", "big.bin")
	e.mustFail(t, 1, "not a file", "reply", "1", "-m", "x", "-a", "src")
	e.mustFail(t, 2, "both be attached as build.log", "post", "--title", "x", "-m", "x", "-a", "src/build.log", "-a", "other/build.log")
	e.mustFail(t, 2, "invalid attachment name", "edit", "1", "--detach", "a/b")
	if ids := listedThreads(t, e, "list"); len(ids) != 1 {
		t.Errorf("refused posts were written: %v", ids)
	}
	var th threadShape
	if e.json(t, &th, "thread", "1"); th.Replies != 1 {
		t.Errorf("refused replies were written: %+v", th)
	}

	// Attaching and detaching aren't edits.
	var edited struct {
		ID     int64
		Edited string
	}
	e.json(t, &edited, "edit", "1", "-a", "notes", "-a", "other/build.log", "--detach", "shot.png")
	if edited.ID != 1 || edited.Edited != "" {
		t.Errorf("edit -a = %+v", edited)
	}
	p = postOf(t, e, 1)
	want = []post.Attachment{{Name: "build.log", Type: logType, Size: 1}, {Name: "notes", Type: "application/octet-stream", Size: 5}}
	if !slices.Equal(p.Attachments, want) || p.Edited != "" {
		t.Errorf("after edit -a = %+v, want %+v", p, want)
	}
	e.mustFail(t, 1, "has no attachment", "edit", "1", "--detach", "shot.png")
	e.mustFail(t, 1, "only its author or a person", "edit", "1", "-a", "notes", "--as", "claude-code/95d3e5c1")
	e.json(t, &edited, "edit", "1", "-m", "changed", "-a", "src/shot.png")
	if edited.Edited == "" || len(postOf(t, e, 1).Attachments) != 3 {
		t.Errorf("an edit with -a = %+v", edited)
	}
}

func TestAttachFailureNamesWhatIsLeft(t *testing.T) {
	e := threadsEnv(t)
	e.writeTree(t, map[string]string{"a.txt": "a", "b.txt": "b"})
	e.ok(t, "post", "--title", "T", "-m", "x")
	c := &ctx{env: e.env(nil, nil, &bytes.Buffer{}, &bytes.Buffer{}), o: &opts{set: map[string]bool{}}}
	c.o.attachFiles = stringList{"a.txt", "b.txt"}
	files, err := c.uploads()
	if err != nil {
		t.Fatal(err)
	}
	cl := client.New("http://"+e.vars["AGORA_SERVER"], "proj", post.Identity{Harness: "codex", Session: "x", Node: "testbox"})
	err = c.attach(cl, "posted", 9, files)
	if err == nil || !strings.Contains(err.Error(), "posted #9, but couldn't attach a.txt") || !strings.Contains(err.Error(), "not attached: a.txt, b.txt") ||
		!strings.Contains(err.Error(), "agora edit 9 -a FILE") {
		t.Errorf("attach = %v", err)
	}
	// An edit that only attaches wrote nothing.
	if err := c.changeAttachments(cl, 9, false, files[1:]); err == nil || !strings.HasPrefix(err.Error(), "#9: couldn't attach b.txt") {
		t.Errorf("changeAttachments = %v", err)
	}
}

func TestDownload(t *testing.T) {
	e := threadsEnv(t)
	e.writeTree(t, map[string]string{"src/build.log": "the log\n"})
	e.ok(t, "post", "--title", "T", "-m", "x", "-a", "src/build.log")

	var w struct {
		File string
		Size int64
	}
	e.json(t, &w, "download", "1", "build.log")
	if w.File != filepath.Join(e.dir, "build.log") || w.Size != 8 {
		t.Errorf("download = %+v", w)
	}
	if data, err := os.ReadFile(w.File); err != nil || string(data) != "the log\n" {
		t.Errorf("downloaded %q, %v", data, err)
	}
	e.mustFail(t, 1, "exists", "download", "1", "build.log")
	e.ok(t, "download", "1", "build.log", "--force")
	if err := os.MkdirAll(filepath.Join(e.dir, "out", "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	e.json(t, &w, "download", "#1", "build.log", "-o", filepath.Join("out", "copy.log"))
	if w.File != filepath.Join(e.dir, "out", "copy.log") {
		t.Errorf("download -o = %+v", w)
	}
	e.mustFail(t, 1, "is a directory", "download", "1", "build.log", "-o", "out/dir", "--force")
	if out := e.ok(t, "download", "1", "build.log", "-o", "-"); out != "the log\n" {
		t.Errorf("download -o - = %q", out)
	}
	e.mustFail(t, 2, "--json", "download", "1", "build.log", "-o", "-", "--json")
	e.mustFail(t, 2, "invalid attachment name", "download", "1", "a/b")
	e.mustFail(t, 2, "usage", "download", "1")
	e.mustFail(t, 1, "has no attachment", "download", "1", "nope.log", "--json")
	e.mustFail(t, 1, "no post #9", "download", "9", "build.log", "-o", "nope.log")
	assertNoTemp(t, e.dir, "build.log", "out", "src")
}

// assertNoTemp checks that dir holds only names, and no files that failed
// commands left behind.
func assertNoTemp(t *testing.T, dir string, names ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !slices.Contains(names, entry.Name()) {
			t.Errorf("%s holds %s", dir, entry.Name())
		}
	}
}

func TestMoveCommand(t *testing.T) {
	e := threadsEnv(t)
	e.ok(t, "post", "--title", "A", "-m", "a")
	e.ok(t, "reply", "1", "-m", "two")
	e.ok(t, "reply", "2", "-m", "three")
	e.ok(t, "post", "--title", "B", "-m", "b")

	e.mustFail(t, 1, "only a person", "move", "2", "--to", "4")
	var moved struct {
		Thread int64
		Moved  []int64
	}
	e.json(t, &moved, "move", "2", "--to", "#4", "--as", "human/alice")
	if moved.Thread != 4 || !slices.Equal(moved.Moved, []int64{2, 3}) {
		t.Errorf("move = %+v", moved)
	}
	if out := e.ok(t, "move", "3", "--title", "C", "--as", "human/alice"); !strings.Contains(out, "moved #3 to thread #3") {
		t.Errorf("move --title prints %q", out)
	}
	// Threads are by their newest posts: 4 holds 2 and 4, and 3 only 3.
	if ids := listedThreads(t, e, "list"); !slices.Equal(ids, []int64{4, 3, 1}) {
		t.Errorf("threads after moves = %v", ids)
	}
	for _, args := range [][]string{{"move", "2"}, {"move", "2", "--to", "4", "--title", "x"}, {"move", "2", "--to", "x"}, {"move", "--to", "4"}} {
		e.mustFail(t, 2, "", append(args, "--as", "human/alice")...)
	}
	e.mustFail(t, 1, "moves with", "move", "1", "--to", "1", "--as", "human/alice")
}

func TestBackupCommand(t *testing.T) {
	e := threadsEnv(t)
	e.ok(t, "post", "--title", "Kept", "-m", "x")
	// A backup is of the server, not of a board.
	delete(e.vars, "AGORA_BOARD")
	var w struct {
		File string
		Size int64
	}
	e.json(t, &w, "backup", "copy.db")
	if w.File != filepath.Join(e.dir, "copy.db") || w.Size == 0 {
		t.Fatalf("backup = %+v", w)
	}
	if info, err := os.Stat(w.File); err != nil || info.Size() != w.Size {
		t.Fatalf("the backup's file: %v, %v", info, err)
	}
	st, err := store.Open(w.File, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ts, err := st.List(context.Background(), "proj", post.Filter{})
	_ = st.Close()
	if err != nil || len(ts) != 1 || ts[0].Title != "Kept" {
		t.Errorf("the backup's threads = %+v, %v", ts, err)
	}

	e.mustFail(t, 1, "exists", "backup", "copy.db")
	e.ok(t, "backup", "copy.db", "--force")
	if err := os.Mkdir(filepath.Join(e.dir, "dir.db"), 0o755); err != nil {
		t.Fatal(err)
	}
	e.mustFail(t, 1, "is a directory", "backup", "dir.db", "--force")
	e.mustFail(t, 1, "", "backup", filepath.Join("missing", "copy.db"))
	delete(e.vars, "AGORA_AUTHOR")
	e.mustFail(t, 1, "no identity", "backup", "other.db")
	e.mustFail(t, 2, "usage", "backup")
	assertNoTemp(t, e.dir, "copy.db", "dir.db")
}
