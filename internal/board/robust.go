package board

import (
	mrand "math/rand/v2"
	"os"
	"time"
)

// transientTimeout bounds retries of operations that fail only while another
// process briefly holds a file, as on Windows.
const transientTimeout = 2 * time.Second

func retry(f func() error) error {
	deadline := time.Now().Add(transientTimeout)
	for {
		err := f()
		if err == nil || !transient(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(time.Duration(5+mrand.IntN(20)) * time.Millisecond)
	}
}

// readFile reads a file, retrying while another process holds it.
func readFile(path string) ([]byte, error) {
	var data []byte
	err := retry(func() error {
		var err error
		data, err = os.ReadFile(path)
		return err
	})
	return data, err
}

// rename renames a file, retrying while another process holds the target.
func rename(from, to string) error {
	return retry(func() error { return os.Rename(from, to) })
}
