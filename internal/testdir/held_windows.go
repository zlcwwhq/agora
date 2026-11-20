package testdir

import (
	"errors"
	"syscall"
)

// Windows' errors for a file that is still held: it denies access to a file
// that is open or being deleted, refuses to share one that is open, and
// refuses to remove a directory while one is left in it.
const (
	errorAccessDenied     = syscall.Errno(5)
	errorSharingViolation = syscall.Errno(32)
	errorDirNotEmpty      = syscall.Errno(145)
)

// held reports whether err may come from a file that is still held, and may
// go away.
func held(err error) bool {
	return errors.Is(err, errorAccessDenied) || errors.Is(err, errorSharingViolation) || errors.Is(err, errorDirNotEmpty)
}
