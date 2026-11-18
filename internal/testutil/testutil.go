// Package testutil holds helpers for tests.
package testutil

import (
	"os"
	"strings"
	"testing"
	"time"
)

// TempDir is like t.TempDir, except that removing the directory retries for a
// while. On Windows, scanners briefly hold files that tests have just closed,
// and t.TempDir's cleanup then fails with "the directory is not empty".
func TempDir(t testing.TB) string {
	t.Helper()
	name := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '_'
	}, t.Name())
	dir, err := os.MkdirTemp("", name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		deadline := time.Now().Add(10 * time.Second)
		for {
			err := os.RemoveAll(dir)
			if err == nil {
				return
			}
			if time.Now().After(deadline) {
				t.Errorf("removing %s: %v", dir, err)
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	})
	return dir
}
