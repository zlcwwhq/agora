// Package testdir gives tests temporary directories, as testing.T's TempDir
// does, whose removal after the test waits out errors that Windows reports
// while a file in them is still held, which TempDir gives up on. Windows
// runs of the tests have failed with "The directory is not empty" for
// directories that were empty a moment later, with no file of the test
// process still open.
package testdir

import (
	"os"
	"regexp"
	"testing"
	"time"
)

// timeout is how long removing a directory waits for its files to go.
const timeout = 10 * time.Second

var unsafe = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

// New returns a new, empty directory, which is removed with everything in it
// once the test and its subtests are done, as TempDir's are. If removing it
// takes more than one try, the test's log says how many, how long they took,
// and what the first one left.
func New(t testing.TB) string {
	t.Helper()
	prefix := unsafe.ReplaceAllString(t.Name(), "_")
	if len(prefix) > 64 {
		prefix = prefix[:64]
	}
	dir, err := os.MkdirTemp("", prefix+"-")
	if err != nil {
		t.Fatalf("testdir: %v", err)
	}
	t.Cleanup(func() {
		start := time.Now()
		tries, left, err := removeAll(dir, os.RemoveAll, held, timeout)
		if tries > 1 {
			t.Logf("testdir: removing %s took %d tries in %v; the first left %q", dir, tries, time.Since(start), left)
		}
		if err != nil {
			t.Errorf("testdir: removing %s: %v", dir, err)
		}
	})
	return dir
}

// removeAll removes dir with rm, and tries again, until timeout has passed,
// while again reports that the error may go away. It returns the number of
// tries, the names that dir held after the first one that failed, and the
// last error.
func removeAll(dir string, rm func(string) error, again func(error) bool, timeout time.Duration) (tries int, left []string, err error) {
	deadline := time.Now().Add(timeout)
	wait := time.Millisecond
	for {
		tries++
		if err = rm(dir); err == nil || !again(err) {
			return tries, left, err
		}
		if tries == 1 {
			left = names(dir)
		}
		if time.Now().Add(wait).After(deadline) {
			return tries, left, err
		}
		time.Sleep(wait)
		wait = min(2*wait, 100*time.Millisecond)
	}
}

// names returns the names of dir's entries, or the error that reading it
// gives, so that a log can say what was left.
func names(dir string) []string {
	es, err := os.ReadDir(dir)
	if err != nil {
		return []string{err.Error()}
	}
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Name()
	}
	return out
}
