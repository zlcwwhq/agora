package index

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/zlcwwhq/agora/internal/post"
	"github.com/zlcwwhq/agora/internal/testutil"
)

// fakeBoard holds post files in memory.
type fakeBoard struct {
	data  map[string][]byte
	mtime map[string]int64
	reads int
}

func newFakeBoard() *fakeBoard {
	return &fakeBoard{data: map[string][]byte{}, mtime: map[string]int64{}}
}

func path(id post.ID) string { return fmt.Sprintf("posts/%s/%d.md", id.Node, id.N) }

func (b *fakeBoard) put(t *testing.T, p post.Post) {
	t.Helper()
	data, err := post.Render(p)
	if err != nil {
		t.Fatal(err)
	}
	b.putRaw(path(p.ID), data)
}

func (b *fakeBoard) putRaw(path string, data []byte) {
	b.data[path] = data
	b.mtime[path]++
}

func (b *fakeBoard) files() []File {
	var out []File
	for p, data := range b.data {
		parts := strings.Split(p, "/")
		n, _ := post.ParseFileName(parts[2])
		out = append(out, File{Path: p, ID: post.ID{Node: parts[1], N: n}, Size: int64(len(data)), MtimeNs: b.mtime[p]})
	}
	return out
}

func (b *fakeBoard) read(path string) ([]byte, error) {
	b.reads++
	data, ok := b.data[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return data, nil
}

func id(s string) post.ID {
	i, err := post.ParseID(s)
	if err != nil {
		panic(err)
	}
	return i
}

func rootPost(s, title, created, body string, tags ...string) post.Post {
	return post.Post{ID: id(s), Harness: "claude-code", Session: "s-" + s, Created: created, Title: title, Tags: append([]string{}, tags...), Body: body}
}

func replyPost(s, thread, to, created, body string, tags ...string) post.Post {
	return post.Post{ID: id(s), Harness: "codex", Session: "s-" + s, Created: created, Thread: id(thread), ReplyTo: id(to), Tags: append([]string{}, tags...), Body: body}
}

func openIndex(t *testing.T) (*DB, string) {
	t.Helper()
	p := filepath.Join(testutil.TempDir(t), "index.db")
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d, p
}

func sync(t *testing.T, d *DB, b *fakeBoard) []Warning {
	t.Helper()
	w, err := d.Sync(b.files(), b.read)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// sample is a small board: two threads that reference each other, a reply
// whose root hasn't arrived, and Chinese text.
func sample(t *testing.T) *fakeBoard {
	b := newFakeBoard()
	b.put(t, rootPost("wsl-1", "Star 冲突处理", "2026-11-18T10:00:00Z", "Should conflicts block commands?\n", "mutagen", "design"))
	b.put(t, replyPost("mac-1", "wsl-1", "wsl-1", "2026-11-18T10:05:00+01:00", "I think 冲突 should warn. See #mac-2.\n", "review"))
	b.put(t, rootPost("mac-2", "Release checklist", "2026-11-18T11:00:00Z", "Mention the 冲突 work from #wsl-1, and #gpu-7.\n", "release"))
	b.put(t, replyPost("gpu-2", "gpu-1", "gpu-1", "2026-11-18T12:00:00Z", "an orphan reply about 冲突\n"))
	return b
}

func TestSyncIsIncremental(t *testing.T) {
	d, _ := openIndex(t)
	b := sample(t)
	if w := sync(t, d, b); len(w) != 0 {
		t.Fatalf("warnings: %v", w)
	}
	if b.reads != 4 {
		t.Fatalf("first sync read %d files", b.reads)
	}

	b.reads = 0
	sync(t, d, b)
	if b.reads != 0 {
		t.Fatalf("an unchanged board read %d files", b.reads)
	}

	// A changed file is read again; a removed one leaves the index.
	b.put(t, rootPost("mac-2", "Release checklist v2", "2026-11-18T11:00:00Z", "new body\n"))
	delete(b.data, "posts/gpu/2.md")
	sync(t, d, b)
	if b.reads != 1 {
		t.Fatalf("read %d files for one change", b.reads)
	}
	if e, ok, err := d.Get(id("mac-2")); err != nil || !ok || e.Title != "Release checklist v2" || len(e.Tags) != 0 {
		t.Fatalf("changed post: %+v %v %v", e, ok, err)
	}
	if _, ok, _ := d.Get(id("gpu-2")); ok {
		t.Fatal("a removed post is still indexed")
	}
}

func TestSyncWarnsOnceAboutInvalidFiles(t *testing.T) {
	d, _ := openIndex(t)
	b := sample(t)
	b.putRaw("posts/wsl/9.md", []byte("not a post"))
	p := rootPost("wsl-3", "misplaced", "2026-11-18T10:00:00Z", "")
	data, _ := post.Render(p)
	b.putRaw("posts/wsl/4.md", data)

	w := sync(t, d, b)
	if len(w) != 2 {
		t.Fatalf("warnings: %v", w)
	}
	for _, x := range w {
		if x.Path == "posts/wsl/4.md" && !strings.Contains(x.String(), "disagrees with its path") {
			t.Fatalf("warning: %s", x)
		}
	}
	if w := sync(t, d, b); len(w) != 0 {
		t.Fatalf("warned again: %v", w)
	}
	if _, ok, _ := d.Get(id("wsl-3")); ok {
		t.Fatal("indexed a post under the wrong path")
	}

	// Fixing the file indexes it.
	b.put(t, rootPost("wsl-9", "fixed", "2026-11-18T10:00:00Z", ""))
	if w := sync(t, d, b); len(w) != 0 {
		t.Fatalf("warnings: %v", w)
	}
	if _, ok, _ := d.Get(id("wsl-9")); !ok {
		t.Fatal("the fixed post isn't indexed")
	}
}

func TestOpenRebuildsAnotherVersion(t *testing.T) {
	d, p := openIndex(t)
	sync(t, d, sample(t))
	if _, err := d.db.Exec("PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	items, err := d.Items()
	if err != nil || len(items) != 0 {
		t.Fatalf("after rebuild: %v %v", items, err)
	}
	b := sample(t)
	sync(t, d, b)
	if b.reads != 4 {
		t.Fatalf("the rebuilt index read %d files", b.reads)
	}
}

func TestConcurrentOpenCreatesTheSchemaOnce(t *testing.T) {
	p := filepath.Join(testutil.TempDir(t), "index.db")
	errs := make(chan error, 8)
	for range 8 {
		go func() {
			d, err := Open(p)
			if err == nil {
				_, err = d.Sync(sample(t).files(), sample(t).read)
				_ = d.Close()
			}
			errs <- err
		}()
	}
	for range 8 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

func TestThreads(t *testing.T) {
	d, _ := openIndex(t)
	sync(t, d, sample(t))

	ts, err := d.List(Filter{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) != 2 || ts[0].ID != id("mac-2") || ts[1].ID != id("wsl-1") {
		t.Fatalf("list: %+v", ts)
	}
	w := ts[1]
	// mac-1 was created at 09:05Z, before the root; activity is the latest.
	if w.Replies != 1 || w.LastActivity != "2026-11-18T10:00:00Z" || !reflect.DeepEqual(w.Tags, []string{"design", "mutagen", "review"}) {
		t.Fatalf("thread: %+v", w)
	}

	if ts, _ := d.List(Filter{Tags: []string{"review"}}, 10); len(ts) != 1 || ts[0].ID != id("wsl-1") {
		t.Fatalf("a reply's tag selects its thread: %+v", ts)
	}
	if ts, _ := d.List(Filter{Tags: []string{"review", "release"}}, 10); len(ts) != 0 {
		t.Fatalf("tags are all required: %+v", ts)
	}
	if ts, _ := d.List(Filter{Harness: "claude-code", SessionPrefix: "s-mac"}, 10); len(ts) != 1 || ts[0].ID != id("mac-2") {
		t.Fatalf("author: %+v", ts)
	}
	if ts, _ := d.List(Filter{Harness: "codex"}, 10); len(ts) != 0 {
		t.Fatalf("the author filter applies to roots: %+v", ts)
	}
	if ts, _ := d.List(Filter{}, 1); len(ts) != 1 {
		t.Fatalf("limit: %+v", ts)
	}

	posts, err := d.ThreadPosts(id("wsl-1"))
	if err != nil || len(posts) != 2 || posts[0].ID != id("mac-1") || !reflect.DeepEqual(posts[0].Refs, []post.ID{id("mac-2")}) {
		t.Fatalf("thread posts in display order: %+v %v", posts, err)
	}

	orphan, err := d.ThreadInfo(id("gpu-1"))
	if err != nil || orphan.Title != "" || orphan.Replies != 1 || orphan.LastActivity != "2026-11-18T12:00:00Z" {
		t.Fatalf("orphan thread: %+v %v", orphan, err)
	}
}

func TestReferences(t *testing.T) {
	d, _ := openIndex(t)
	sync(t, d, sample(t))
	bl, err := d.Backlinks(id("wsl-1"))
	if err != nil {
		t.Fatal(err)
	}
	if want := []Backlink{{ID: id("mac-2"), To: id("wsl-1"), Thread: id("mac-2"), Title: "Release checklist"}}; !reflect.DeepEqual(bl, want) {
		t.Fatalf("backlinks: %+v", bl)
	}
	bl, _ = d.Backlinks(id("mac-2"))
	if len(bl) != 1 || bl[0].ID != id("mac-1") || bl[0].Thread != id("wsl-1") || bl[0].Title != "Star 冲突处理" {
		t.Fatalf("backlinks from a reply: %+v", bl)
	}
	un, err := d.Unresolved(id("mac-2"))
	if err != nil || !reflect.DeepEqual(un, []post.ID{id("gpu-7")}) {
		t.Fatalf("unresolved: %v %v", un, err)
	}
}

func TestSearch(t *testing.T) {
	d, _ := openIndex(t)
	sync(t, d, sample(t))
	threads := func(rs []Result) []string {
		var out []string
		for _, r := range rs {
			out = append(out, r.ID.String())
		}
		return out
	}

	// Two-character Chinese terms match titles and bodies; the title match
	// comes first, then by activity.
	rs, err := d.Search(Query{Terms: []string{"冲突"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := threads(rs); !reflect.DeepEqual(got, []string{"wsl-1", "gpu-1", "mac-2"}) {
		t.Fatalf("order: %v", got)
	}
	if m := rs[0].Matches; len(m) != 2 || m[0].ID != id("mac-1") || m[0].Field != FieldBody || m[1].Field != FieldTitle || m[1].Snippet != "Star 冲突处理" {
		t.Fatalf("matches: %+v", m)
	}
	if !strings.Contains(rs[0].Matches[0].Snippet, "冲突 should warn") {
		t.Fatalf("snippet: %q", rs[0].Matches[0].Snippet)
	}

	for _, c := range []struct {
		q    Query
		want []string
	}{
		{Query{Terms: []string{"STAR", "commands"}}, []string{"wsl-1"}}, // title and body of one post
		{Query{Terms: []string{"star", "warn"}}, nil},                   // in different posts
		{Query{Terms: []string{"冲突"}, Field: FieldTitle}, []string{"wsl-1"}},
		{Query{Terms: []string{"处理"}, Field: FieldBody}, nil},
		{Query{Terms: []string{"冲突"}, Filter: Filter{Tags: []string{"release"}}}, []string{"mac-2"}},
		{Query{Terms: []string{"冲突"}, Filter: Filter{Harness: "codex"}}, []string{"gpu-1", "wsl-1"}}, // no title matches: by activity
		{Query{Terms: []string{"冲突"}, Limit: 1}, []string{"wsl-1"}},
	} {
		rs, err := d.Search(c.q)
		if err != nil {
			t.Fatal(err)
		}
		if got := threads(rs); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v: got %v, want %v", c.q, got, c.want)
		}
	}
	if _, err := d.Search(Query{Terms: []string{" "}}); err == nil {
		t.Fatal("searched without terms")
	}
}

func TestSnippet(t *testing.T) {
	body := strings.Repeat("前", 40) + "Target\nword" + strings.Repeat("后", 80)
	s := snippet(body, post.Fold(body), []string{"target"})
	if !strings.HasPrefix(s, "…前") || !strings.Contains(s, "Target word") || !strings.HasSuffix(s, "…") {
		t.Fatalf("snippet: %q", s)
	}
	if s := snippet("short", "short", []string{"x"}); s != "short" {
		t.Fatalf("snippet: %q", s)
	}
}

func TestTagsAndItems(t *testing.T) {
	d, _ := openIndex(t)
	sync(t, d, sample(t))
	tags, err := d.Tags()
	if err != nil {
		t.Fatal(err)
	}
	want := []TagCount{{"design", 1}, {"mutagen", 1}, {"release", 1}, {"review", 1}}
	if !reflect.DeepEqual(tags, want) {
		t.Fatalf("tags: %+v", tags)
	}
	items, err := d.Items()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range items {
		got = append(got, it.ID.String())
	}
	if !reflect.DeepEqual(got, []string{"mac-1", "wsl-1", "mac-2", "gpu-2"}) {
		t.Fatalf("items: %v", got)
	}
}
