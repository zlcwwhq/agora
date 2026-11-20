package store

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zlcwwhq/agora/internal/post"
	"github.com/zlcwwhq/agora/internal/testdir"
)

func TestBackup(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	root := newThread(t, s, codex, "Kept", "in the backup", "saved")
	reply(t, s, claude, root, "me too")
	if _, _, err := s.Attach(ctx, codex, "b", root, "data.bin", "", []byte{0, 1, 2}); err != nil {
		t.Fatal(err)
	}
	must[post.Thread](t)(s.ChangeThread(ctx, alice, "b", root, post.ThreadChange{Pinned: valuePtr(true)}))

	rc, size, err := s.Backup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(testdir.New(t), "copy.db")
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, rc); err != nil {
		t.Fatal(err)
	}
	if err := rc.Close(); err != nil {
		t.Fatal(err)
	}
	if int64(buf.Len()) != size || !bytes.HasPrefix(buf.Bytes(), []byte("SQLite format 3\x00")) {
		t.Fatalf("backup of %d bytes, said %d", buf.Len(), size)
	}
	if err := os.WriteFile(copyPath, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	// Nothing stays behind next to the database.
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".agora-backup-") {
			t.Errorf("left %s behind", e.Name())
		}
	}

	// Writes after the backup aren't in it.
	newThread(t, s, codex, "Later", "not in the backup")

	c, err := Open(copyPath, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	ts := must[[]post.Thread](t)(c.List(ctx, "b", post.Filter{}))
	if len(ts) != 1 || ts[0].Title != "Kept" || ts[0].Replies != 1 || !ts[0].Pinned || ts[0].Tags[0] != "saved" {
		t.Fatalf("the backup's threads = %+v", ts)
	}
	if _, data, err := c.Attachment(ctx, "b", root, "data.bin"); err != nil || !bytes.Equal(data, []byte{0, 1, 2}) {
		t.Errorf("the backup's attachment = %v, %v", data, err)
	}
	if bs := must[[]post.Board](t)(c.Boards(ctx)); len(bs) != 1 || bs[0].ID != "b" || bs[0].Posts != 2 {
		t.Errorf("the backup's boards = %+v", bs)
	}
}
