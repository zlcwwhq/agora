//go:build windows

package board

import (
	"errors"
	"syscall"
)

// Windows error codes for files that another process holds, such as one that
// reads a file while it is being replaced, or a scanner that inspects a new
// file.
const (
	errorAccessDenied     syscall.Errno = 5
	errorSharingViolation syscall.Errno = 32
	errorLockViolation    syscall.Errno = 33
)

func transient(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	return errno == errorAccessDenied || errno == errorSharingViolation || errno == errorLockViolation
}
