package board

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zlcwwhq/agora/internal/post"
)

// scratchFiles lists what is left in the scratch directory.
func scratchFiles(t *testing.T, b *Board) []string {
	t.Helper()
	entries, err := os.ReadDir(b.scratch())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestDelete(t *testing.T) {
	b := open(t)
	if err := b.Init("wsl", false); err != nil {
		t.Fatal(err)
	}
	p, err := b.Publish(draft("A secret"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(b.Root, "posts", "wsl", "1.md")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Only the author may delete it.
	if _, _, err := b.Delete(p.ID, "codex", "s1", "2026-11-19T10:00:00Z"); !errors.Is(err, ErrNotYours) {
		t.Errorf("another author's delete: %v, want ErrNotYours", err)
	}
	if _, _, err := b.Delete(post.ID{Node: "mac", N: 1}, "claude-code", "s1", "2026-11-19T10:00:00Z"); !errors.Is(err, ErrNotYours) ||
		!strings.Contains(err.Error(), "machine mac wrote it") {
		t.Errorf("deleting another machine's post: %v, want ErrNotYours", err)
	}
	if data, _ := os.ReadFile(path); string(data) != string(original) {
		t.Fatal("a refused delete changed the post")
	}

	stub, already, err := b.Delete(p.ID, "claude-code", "s1", "2026-11-19T10:00:00Z")
	if err != nil || already || stub.Deleted != "2026-11-19T10:00:00Z" || stub.Title != "" || stub.Body != "" {
		t.Fatalf("delete: %+v, already %v, %v", stub, already, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := post.Render(p.Stub("2026-11-19T10:00:00Z")); string(data) != string(want) {
		t.Fatalf("the post's file after delete:\n%s\nwant its stub:\n%s", data, want)
	}
	if left := scratchFiles(t, b); len(left) != 0 {
		t.Errorf("delete left scratch files %v", left)
	}

	// A stub is final.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	again, already, err := b.Delete(p.ID, "claude-code", "s1", "2026-11-20T10:00:00Z")
	if err != nil || !already || again.Deleted != "2026-11-19T10:00:00Z" {
		t.Fatalf("deleting again: %+v, already %v, %v", again, already, err)
	}
	if after, err := os.Stat(path); err != nil || !after.ModTime().Equal(info.ModTime()) {
		t.Errorf("deleting again rewrote the stub: %v", err)
	}
	if data2, _ := os.ReadFile(path); string(data2) != string(data) {
		t.Error("deleting again changed the stub")
	}
}

func TestDeleteWhileThePostIsOpen(t *testing.T) {
	b := open(t)
	if err := b.Init("wsl", false); err != nil {
		t.Fatal(err)
	}
	p, err := b.Publish(draft("Held open"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(b.Root, "posts", "wsl", "1.md")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// On Windows, another program holding the file may keep it from being
	// replaced; elsewhere, it can't.
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = b.Delete(p.ID, "claude-code", "s1", "2026-11-19T10:00:00Z")
	_ = f.Close()
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err != nil {
		if string(data) != string(original) {
			t.Errorf("a failed delete changed the post: %v", err)
		}
	} else if !strings.Contains(string(data), "deleted: ") {
		t.Errorf("delete succeeded, but the file isn't a stub:\n%s", data)
	}
	if left := scratchFiles(t, b); len(left) != 0 {
		t.Errorf("delete left scratch files %v (%v)", left, err)
	}
}
