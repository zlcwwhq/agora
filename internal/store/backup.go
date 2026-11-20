package store

import (
	"context"
	"io"
	"os"
	"path/filepath"
)

// Backup writes a consistent copy of the database, with every write that
// finished before it and none in part, to a temporary file next to the
// database, and returns it to read, with its size. Closing it removes the
// file.
func (s *Store) Backup(ctx context.Context) (io.ReadCloser, int64, error) {
	dir, err := os.MkdirTemp(s.dir, ".agora-backup-")
	if err != nil {
		return nil, 0, err
	}
	b := &backup{dir: dir}
	// VACUUM INTO writes a file that mustn't exist yet, outside of any
	// transaction; it reads the database as one.
	path := filepath.Join(dir, "board.db")
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		_ = b.Close()
		return nil, 0, err
	}
	if b.f, err = os.Open(path); err != nil {
		_ = b.Close()
		return nil, 0, err
	}
	info, err := b.f.Stat()
	if err != nil {
		_ = b.Close()
		return nil, 0, err
	}
	return b, info.Size(), nil
}

// backup is a copy of the database in a temporary directory, which Close
// removes.
type backup struct {
	dir string
	f   *os.File
}

func (b *backup) Read(p []byte) (int, error) { return b.f.Read(p) }

// Close closes the copy and removes it: on Windows, an open file can't be
// removed.
func (b *backup) Close() error {
	var err error
	if b.f != nil {
		err = b.f.Close()
	}
	if rerr := os.RemoveAll(b.dir); err == nil {
		err = rerr
	}
	return err
}
