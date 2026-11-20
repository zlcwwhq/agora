package post

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The states of a thread.
const (
	StateOpen     = "open"
	StateResolved = "resolved"
)

// CheckState returns an error of kind Invalid unless s is a thread's state.
func CheckState(s string) error {
	if s != StateOpen && s != StateResolved {
		return Errorf(Invalid, "invalid state %q: want %s or %s", s, StateOpen, StateResolved)
	}
	return nil
}

// CheckSkip returns an error of kind Invalid if skip is below 0.
func CheckSkip(skip int) error {
	if skip < 0 {
		return Errorf(Invalid, "invalid skip %d: want a number of at least 0", skip)
	}
	return nil
}

// ThreadChange asks to change a thread, as agora thread does. A nil field
// keeps its value.
type ThreadChange struct {
	State  *string `json:"state,omitempty"`
	Pinned *bool   `json:"pinned,omitempty"`
	Locked *bool   `json:"locked,omitempty"`
}

// Empty reports whether c changes nothing.
func (c ThreadChange) Empty() bool { return c.State == nil && c.Pinned == nil && c.Locked == nil }

// MayChangeThread reports, as an error of kind Forbidden, whether the
// identity may make every change of c to the thread whose first post is root:
// its author and people may resolve and reopen it, and only people may pin
// and lock it.
func (id Identity) MayChangeThread(root Post, c ThreadChange) error {
	if (c.Pinned != nil || c.Locked != nil) && !id.IsPerson() {
		return Errorf(Forbidden, "only a person (%s/...) can pin, unpin, lock, or unlock thread #%d", Person, root.ID)
	}
	if c.State != nil && !id.Wrote(root) && !id.IsPerson() {
		return Errorf(Forbidden, "thread #%d is %s's: only its author or a person (%s/...) can resolve or reopen it", root.ID, root.Author, Person)
	}
	return nil
}

// MayWriteIn reports, as an error of kind Forbidden, whether the identity
// may reply to, edit, delete, or attach to the posts of a thread that is
// locked or not: in a locked thread, only people may.
func (id Identity) MayWriteIn(thread int64, locked bool) error {
	if locked && !id.IsPerson() {
		return Errorf(Forbidden, "thread #%d is locked: only a person (%s/...) can reply to its posts, edit, delete, or attach to them", thread, Person)
	}
	return nil
}

// MayMove reports, as an error of kind Forbidden, whether the identity may
// move posts: only people may.
func (id Identity) MayMove() error {
	if !id.IsPerson() {
		return Errorf(Forbidden, "only a person (%s/...) can move posts", Person)
	}
	return nil
}

// MaxAttachment is the most bytes that an attachment may have.
const MaxAttachment = 10 << 20

// Attachment is a file that a post carries, as <post>'s attachments hold it.
type Attachment struct {
	Name string `json:"name"`
	// Type is the media type.
	Type string `json:"type"`
	Size int64  `json:"size"`
}

// CheckAttachmentName returns an error of kind Invalid unless s is an
// attachment's name: 1 to 128 characters without '/', '\', or control
// characters, other than "." and "..".
func CheckAttachmentName(s string) error {
	n := utf8.RuneCountInString(s)
	switch {
	case !utf8.ValidString(s):
		return Errorf(Invalid, "invalid attachment name %q: it isn't valid UTF-8", s)
	case n < 1 || n > 128:
		return Errorf(Invalid, "invalid attachment name %q: want 1 to 128 characters", s)
	case s == "." || s == "..":
		return Errorf(Invalid, "invalid attachment name %q", s)
	case strings.ContainsFunc(s, func(r rune) bool { return r == '/' || r == '\\' || unicode.IsControl(r) }):
		return Errorf(Invalid, "invalid attachment name %q: it can't contain '/', '\\', or control characters", s)
	}
	return nil
}

// CheckAttachmentSize returns an error of kind Invalid if an attachment of
// size bytes is larger than MaxAttachment.
func CheckAttachmentSize(name string, size int64) error {
	if size > MaxAttachment {
		return Errorf(Invalid, "%s has %s: an attachment may have at most 10 MiB", name, FormatSize(size))
	}
	return nil
}

// FormatSize formats a number of bytes for people.
func FormatSize(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
}

// Move asks to move a post, with the posts that reply to it, as agora move
// does: into the thread of To, or into a new thread titled Title.
type Move struct {
	To    int64   `json:"to,omitempty"`
	Title *string `json:"title,omitempty"`
}

// Check returns an error of kind Invalid unless exactly one of To and Title
// is given.
func (m Move) Check() error {
	switch {
	case m.To < 0:
		return Errorf(Invalid, "invalid post id %d: want a number such as 12", m.To)
	case (m.To != 0) == (m.Title != nil):
		return Errorf(Invalid, "a move needs exactly one of to, a post to reply to, and title, a new thread's")
	}
	return nil
}

// Moved is what a move moved: the thread that the posts are in now, and
// their ids in order.
type Moved struct {
	Thread int64   `json:"thread"`
	Moved  []int64 `json:"moved"`
}
