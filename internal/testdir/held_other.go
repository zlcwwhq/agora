//go:build !windows

package testdir

// held reports whether err may come from a file that is still held, which
// only Windows keeps a directory from being removed for.
func held(error) bool { return false }
