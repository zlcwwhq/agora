package testdir

import (
	"io/fs"
	"syscall"
	"testing"
)

func TestHeldWindowsErrors(t *testing.T) {
	for _, code := range []syscall.Errno{5, 32, 145} {
		if !held(&fs.PathError{Op: "unlinkat", Path: `C:\x`, Err: code}) {
			t.Errorf("held rejects Windows error %d", code)
		}
	}
	if held(&fs.PathError{Op: "unlinkat", Path: `C:\x`, Err: syscall.Errno(2)}) {
		t.Error("held accepts ERROR_FILE_NOT_FOUND")
	}
}
