package testdir

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewIsRemovedAfterTheTest(t *testing.T) {
	var dir string
	t.Run("sub/test", func(t *testing.T) {
		dir = New(t)
		if err := os.WriteFile(filepath.Join(dir, "board.db"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if other := New(t); other == dir {
			t.Errorf("two directories are both %s", dir)
		}
	})
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%s is still there after the test: %v", dir, err)
	}
}

var errHeld = errors.New("held")

func isHeld(err error) bool { return errors.Is(err, errHeld) }

// failing returns a remover that fails with err n times, then succeeds, and
// counts its calls.
func failing(n int, err error, calls *int) func(string) error {
	return func(string) error {
		*calls++
		if *calls <= n {
			return &fs.PathError{Op: "unlinkat", Path: "x", Err: err}
		}
		return nil
	}
}

func TestRemoveAllRetriesWhileHeld(t *testing.T) {
	dir := New(t)
	if err := os.WriteFile(filepath.Join(dir, "board.db"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	tries, left, err := removeAll(dir, failing(3, errHeld, &calls), isHeld, time.Minute)
	if err != nil || tries != 4 || calls != 4 {
		t.Fatalf("tries %d, calls %d, err %v; want 4 tries and success", tries, calls, err)
	}
	if len(left) != 1 || left[0] != "board.db" {
		t.Errorf("left %q, want what the first try left", left)
	}
}

func TestRemoveAllGivesUpAtTheTimeout(t *testing.T) {
	calls := 0
	start := time.Now()
	tries, _, err := removeAll(New(t), failing(1<<30, errHeld, &calls), isHeld, 50*time.Millisecond)
	if !errors.Is(err, errHeld) || tries < 2 {
		t.Fatalf("tries %d, err %v; want several tries and the last error", tries, err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("gave up after %v, with a timeout of 50ms", d)
	}
}

func TestRemoveAllDoesNotRetryOtherErrors(t *testing.T) {
	calls := 0
	other := errors.New("other")
	tries, left, err := removeAll(New(t), failing(1, other, &calls), isHeld, time.Minute)
	if !errors.Is(err, other) || tries != 1 || left != nil {
		t.Fatalf("tries %d, left %q, err %v; want one try and its error", tries, left, err)
	}
	if tries, _, err := removeAll(New(t), failing(0, nil, &calls), isHeld, time.Minute); err != nil || tries != 1 {
		t.Fatalf("a removal that succeeds: tries %d, err %v", tries, err)
	}
}

func TestHeldOnlyForWindowsErrors(t *testing.T) {
	if held(errors.New("other")) || held(&fs.PathError{Op: "unlinkat", Path: "x", Err: fs.ErrPermission}) {
		t.Error("held accepts an error that isn't Windows' for a held file")
	}
}
