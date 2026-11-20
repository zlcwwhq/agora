package post

import (
	"errors"
	"fmt"
)

// Kind classifies the errors that the board's rules give, so that the server
// can answer each with its status and the client can tell them apart.
type Kind int

// The kinds of errors.
const (
	// Other is any other failure, such as the database's.
	Other Kind = iota
	// Invalid breaks a rule, such as a reply without a body.
	Invalid
	// Forbidden is something the identity may not do.
	Forbidden
	// NotFound names a board or a post that doesn't exist.
	NotFound
	// Conflict is a board whose id is taken.
	Conflict
)

// Error is an error of a kind.
type Error struct {
	Kind Kind
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

// Errorf returns an error of kind.
func Errorf(kind Kind, format string, args ...any) error {
	return &Error{Kind: kind, Msg: fmt.Sprintf(format, args...)}
}

// KindOf returns the kind of err, or Other.
func KindOf(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return Other
}

// NoPost is the error for a post that doesn't exist.
func NoPost(board string, id int64) error {
	return Errorf(NotFound, "no post #%d on board %s: it doesn't exist, or it was deleted", id, board)
}

// NoBoard is the error for a board that doesn't exist.
func NoBoard(board string) error {
	return Errorf(NotFound, "no board %q on this server: agora boards lists its boards, and agora boards create ID creates one", board)
}
