package board

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/zlcwwhq/agora/internal/cursor"
	"github.com/zlcwwhq/agora/internal/post"
	"github.com/zlcwwhq/agora/internal/testutil"
)

func open(t *testing.T) *Board {
	t.Helper()
	dir := testutil.TempDir(t)
	b, err := Open(filepath.Join(dir, "agora"), filepath.Join(dir, "state"))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func draft(title string) post.Post {
	return post.Post{
		Harness: "claude-code",
		Session: "s1",
		Created: "2026-11-18T10:00:00Z",
		Title:   title,
		Body:    "body of " + title + "\n",
	}
}

func TestStateIsOutsideTheRoot(t *testing.T) {
	b := open(t)
	if rel, err := filepath.Rel(b.Root, b.State); err != nil || !strings.HasPrefix(rel, "..") {
		t.Fatalf("state %s is inside root %s", b.State, b.Root)
	}
}

func TestOpenResolvesSymbolicLinks(t *testing.T) {
	dir := testutil.TempDir(t)
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(dir, "link")); err != nil {
		t.Skip("no symbolic links:", err)
	}
	a, err := Open(filepath.Join(real, "agora"), dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Open(filepath.Join(dir, "link", "agora"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if a.State != b.State || a.Root != b.Root {
		t.Fatalf("one board, two identities: %+v %+v", a, b)
	}
}

func TestHomeAndStateHome(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	// Absolute on whichever system runs the test.
	abs := func(name string) string { return filepath.Join(testutil.TempDir(t), name) }
	xdg, home, profile, local := abs("xdg"), abs("home"), abs("profile"), abs("local")
	all := map[string]string{"XDG_STATE_HOME": xdg, "HOME": home, "USERPROFILE": profile, "LOCALAPPDATA": local}
	noXDG := map[string]string{"XDG_STATE_HOME": "relative", "HOME": home, "USERPROFILE": profile, "LOCALAPPDATA": local}

	for _, c := range []struct {
		goos      string
		env       map[string]string
		home      string
		stateHome string
	}{
		{"linux", all, home, xdg},
		{"darwin", noXDG, home, filepath.Join(home, ".local", "state")},
		{"windows", all, profile, xdg},
		{"windows", noXDG, profile, local},
	} {
		if got, err := Home(env(c.env), c.goos); err != nil || got != c.home {
			t.Errorf("%s: Home = %q, %v; want %q", c.goos, got, err, c.home)
		}
		if got, err := StateHome(env(c.env), c.goos); err != nil || got != c.stateHome {
			t.Errorf("%s: StateHome = %q, %v; want %q", c.goos, got, err, c.stateHome)
		}
	}
	// Windows never falls back to roaming or Unix locations.
	if _, err := StateHome(env(map[string]string{"HOME": home, "APPDATA": abs("roaming")}), "windows"); err == nil {
		t.Error("Windows state without LOCALAPPDATA")
	}
	if _, err := Home(env(map[string]string{"HOME": home}), "windows"); err == nil {
		t.Error("Windows home from HOME")
	}
	if _, err := StateHome(env(nil), "linux"); err == nil {
		t.Error("no error without HOME")
	}
}

func TestInit(t *testing.T) {
	b := open(t)
	if _, err := b.Node(); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("before init: %v", err)
	}
	if _, err := b.Publish(draft("early")); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("publishing before init: %v", err)
	}
	if err := b.Init("Bad-Name", false); err == nil {
		t.Fatal("accepted an invalid name")
	}
	if err := b.Init("wsl", false); err != nil {
		t.Fatal(err)
	}
	if node, err := b.Node(); err != nil || node != "wsl" {
		t.Fatalf("node %q, %v", node, err)
	}
	for _, dir := range []string{"posts/wsl", ".tmp"} {
		if info, err := os.Stat(filepath.Join(b.Root, dir)); err != nil || !info.IsDir() {
			t.Fatalf("%s: %v", dir, err)
		}
	}
	if err := b.Init("wsl", false); err != nil {
		t.Fatalf("initializing again: %v", err)
	}
	if err := b.Init("mac", false); err == nil {
		t.Fatal("changed the node name")
	}
}

func TestInitRefusesANameInUse(t *testing.T) {
	other := open(t)
	if err := other.Init("gpu", false); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if _, err := other.Publish(draft("x")); err != nil {
			t.Fatal(err)
		}
	}

	// A second machine whose copy of the board has gpu's posts.
	b, err := Open(other.Root, filepath.Join(testutil.TempDir(t), "state"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Init("gpu", false); err == nil || !strings.Contains(err.Error(), "--resume") {
		t.Fatalf("accepted a name in use: %v", err)
	}
	if err := b.Init("gpu", true); err != nil {
		t.Fatal(err)
	}
	p, err := b.Publish(draft("y"))
	if err != nil {
		t.Fatal(err)
	}
	if p.ID.String() != "gpu-4" {
		t.Fatalf("resumed at %s", p.ID)
	}
}

func TestPublishNumbersAndNeverReuses(t *testing.T) {
	b := open(t)
	if err := b.Init("wsl", false); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		p, err := b.Publish(draft("t"))
		if err != nil {
			t.Fatal(err)
		}
		if p.ID != (post.ID{Node: "wsl", N: i}) {
			t.Fatalf("post %d got id %s", i, p.ID)
		}
	}
	if err := os.Remove(filepath.Join(b.Root, "posts", "wsl", "3.md")); err != nil {
		t.Fatal(err)
	}
	p, err := b.Publish(draft("t"))
	if err != nil {
		t.Fatal(err)
	}
	if p.ID.N != 4 {
		t.Fatalf("reused a number: %s", p.ID)
	}
	data, err := b.Read("posts/wsl/4.md")
	if err != nil {
		t.Fatal(err)
	}
	got, err := post.Parse(data)
	if err != nil || got.ID != p.ID || got.Title != "t" {
		t.Fatalf("published file: %+v, %v", got, err)
	}
}

func TestPublishSucceedsOnceLinked(t *testing.T) {
	if os.Getuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("directory permissions don't stop writes for root or on Windows")
	}
	b := open(t)
	if err := b.Init("wsl", false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Publish(draft("first")); err != nil {
		t.Fatal(err)
	}
	// The state directory is readable but not writable, so the high-water
	// mark can't be raised after the post is linked.
	if err := os.Chmod(b.State, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(b.State, 0o700) })
	p, err := b.Publish(draft("second"))
	if err != nil {
		t.Fatalf("a published post was reported as failed, inviting a duplicate: %v", err)
	}
	if _, err := b.Read("posts/wsl/2.md"); err != nil || p.ID.N != 2 {
		t.Fatalf("post %s: %v", p.ID, err)
	}
}

func TestConcurrentPublishersGetDistinctNumbers(t *testing.T) {
	b := open(t)
	if err := b.Init("wsl", false); err != nil {
		t.Fatal(err)
	}
	const writers, each = 16, 8
	var wg sync.WaitGroup
	errs := make(chan error, writers*each)
	for w := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each writer opens its own Board, as separate processes would.
			mine, err := Open(b.Root, filepath.Dir(filepath.Dir(b.State)))
			if err != nil {
				errs <- err
				return
			}
			for range each {
				if _, err := mine.Publish(draft("w" + string(rune('a'+w)))); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	files, err := b.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != writers*each {
		t.Fatalf("%d files, want %d", len(files), writers*each)
	}
	for i, f := range files {
		if f.ID.N != i+1 {
			t.Fatalf("file %d is %s", i, f.ID)
		}
		data, err := b.Read(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		if p, err := post.Parse(data); err != nil || p.ID != f.ID {
			t.Fatalf("%s: %+v, %v", f.Path, p.ID, err)
		}
	}
	if left, _ := os.ReadDir(filepath.Join(b.Root, ".tmp")); len(left) != 0 {
		t.Fatalf("scratch files left: %d", len(left))
	}
}

func TestScanIgnoresOtherEntries(t *testing.T) {
	b := open(t)
	if err := b.Init("wsl", false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Publish(draft("t")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"posts/Bad/1.md", "posts/wsl/01.md", "posts/wsl/x.txt", "posts/wsl/2.md.tmp", "posts/stray.md"} {
		path := filepath.Join(b.Root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(b.Root, "posts", "wsl", "5.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	files, err := b.Scan()
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Path != "posts/wsl/1.md" || files[0].Size == 0 || files[0].MtimeNs == 0 {
		t.Fatalf("scan: %+v", files)
	}
}

func TestCursorStorage(t *testing.T) {
	b := open(t)
	c, existed, err := b.LoadCursor("codex", "s1")
	if err != nil || existed || len(c.Nodes) != 0 {
		t.Fatalf("new reader: %+v %v %v", c, existed, err)
	}
	c = cursor.New()
	c.Mark("wsl", 1)
	if err := b.SaveCursor("codex", "s1", c); err != nil {
		t.Fatal(err)
	}
	got, existed, err := b.LoadCursor("codex", "s1")
	if err != nil || !existed || !got.Seen("wsl", 1) {
		t.Fatalf("saved reader: %+v %v %v", got, existed, err)
	}
	if _, existed, _ := b.LoadCursor("codex", "s2"); existed {
		t.Fatal("readers share a cursor")
	}

	// Readers stay apart whether their names differ by '-' placement or by
	// case, which macOS and Windows ignore by default, and names that
	// Windows reserves work.
	for _, pair := range [][2][2]string{
		{{"claude-code", "x"}, {"claude", "code-x"}},
		{{"codex", "ABC"}, {"codex", "abc"}},
		{{"codex", "CON"}, {"codex", "con.txt"}},
	} {
		first, second := pair[0], pair[1]
		if err := b.SaveCursor(first[0], first[1], c); err != nil {
			t.Fatal(err)
		}
		if got, existed, err := b.LoadCursor(first[0], first[1]); err != nil || !existed || got.Reader != first[0]+"/"+first[1] {
			t.Fatalf("%v: %+v %v %v", first, got, existed, err)
		}
		if _, existed, _ := b.LoadCursor(second[0], second[1]); existed {
			t.Fatalf("%v and %v share a cursor", first, second)
		}
	}
}
