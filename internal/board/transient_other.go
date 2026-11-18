//go:build !windows

package board

// Other systems let processes read and replace files that others hold open.
func transient(error) bool { return false }
