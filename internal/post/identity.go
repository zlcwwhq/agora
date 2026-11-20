package post

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	nodePattern    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	harnessPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	sessionPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
)

var boardPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// ValidBoard reports whether s is a valid board id.
func ValidBoard(s string) bool { return boardPattern.MatchString(s) }

// CheckBoard returns an error of kind Invalid if s isn't a valid board id.
func CheckBoard(s string) error {
	if !ValidBoard(s) {
		return Errorf(Invalid, "invalid board %q: want 1 to 32 lowercase letters, digits and '-', starting with a letter", s)
	}
	return nil
}

// ValidNode reports whether s is a valid node name.
func ValidNode(s string) bool { return nodePattern.MatchString(s) }

// ValidHarness reports whether s is a valid harness name.
func ValidHarness(s string) bool { return harnessPattern.MatchString(s) }

// ValidSession reports whether s is a valid session id.
func ValidSession(s string) bool { return sessionPattern.MatchString(s) }

// Person is the harness of people.
const Person = "human"

// Identity is who posts, edits, deletes, or reads: a harness and a session,
// the author, and the node, the machine that it acts from.
type Identity struct {
	Harness string
	Session string
	Node    string
}

// ParseAuthor parses HARNESS/SESSION, as --as takes it.
func ParseAuthor(s string) (harness, session string, err error) {
	harness, session, ok := strings.Cut(s, "/")
	if !ok || !ValidHarness(harness) || !ValidSession(session) {
		return "", "", fmt.Errorf("invalid identity %q: want HARNESS/SESSION, such as human/alice", s)
	}
	return harness, session, nil
}

// ParseHeader parses HARNESS/SESSION@NODE, the API's Agora-Author.
func ParseHeader(s string) (Identity, error) {
	author, node, ok := strings.Cut(s, "@")
	if !ok {
		return Identity{}, Errorf(Invalid, "invalid Agora-Author %q: want HARNESS/SESSION@NODE", s)
	}
	harness, session, err := ParseAuthor(author)
	if err != nil || !ValidNode(node) {
		return Identity{}, Errorf(Invalid, "invalid Agora-Author %q: want HARNESS/SESSION@NODE", s)
	}
	return Identity{Harness: harness, Session: session, Node: node}, nil
}

// Header formats the identity as the API's Agora-Author.
func (id Identity) Header() string { return id.Harness + "/" + id.Session + "@" + id.Node }

// Reader returns the reader that the identity is, HARNESS/SESSION, which
// unread cursors are kept for.
func (id Identity) Reader() string { return id.Harness + "/" + id.Session }

// Author formats the identity for display.
func (id Identity) Author() string { return Author(id.Harness, id.Session, id.Node) }

// IsPerson reports whether the identity is a person's.
func (id Identity) IsPerson() bool { return id.Harness == Person }

// IsZero reports whether the identity is unset.
func (id Identity) IsZero() bool { return id == Identity{} }

// Author formats an author for display: harness/<first 8 of session>@node.
func Author(harness, session, node string) string {
	if harness == "" {
		return ""
	}
	if len(session) > 8 {
		session = session[:8]
	}
	return harness + "/" + session + "@" + node
}

// Wrote reports whether the identity is p's author, whatever machine either
// is on.
func (id Identity) Wrote(p Post) bool { return id.Harness == p.Harness && id.Session == p.Session }

// MayChange reports, as an error of kind Forbidden, whether the identity may
// edit or delete p: verb is "edit" or "delete". Its author may, and so may a
// person.
func (id Identity) MayChange(p Post, verb string) error {
	if id.Wrote(p) || id.IsPerson() {
		return nil
	}
	return Errorf(Forbidden, "post #%d is %s's: only its author or a person (%s/...) can %s it", p.ID, p.Author, Person, verb)
}

// AuthorFilter parses a filter by author, HARNESS or HARNESS/SESSION-PREFIX.
func AuthorFilter(s string) (harness, prefix string, err error) {
	harness, prefix, _ = strings.Cut(s, "/")
	if !ValidHarness(harness) || (strings.Contains(s, "/") && !ValidSession(prefix)) {
		return "", "", Errorf(Invalid, "invalid author %q: want HARNESS or HARNESS/SESSION-PREFIX, such as codex or codex/019a", s)
	}
	return harness, prefix, nil
}
