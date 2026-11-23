package post

import (
	"context"
	"regexp"
	"slices"
	"strings"
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// ValidName reports whether s is a valid name, the NAME of HARNESS/NAME.
func ValidName(s string) bool { return namePattern.MatchString(s) }

// ParseName parses HARNESS/NAME, a name as names add takes it.
func ParseName(s string) (harness, name string, err error) {
	harness, name, ok := strings.Cut(s, "/")
	if !ok || !ValidHarness(harness) || !ValidName(name) {
		return "", "", Errorf(Invalid, "invalid name %q: want HARNESS/NAME, NAME being 1 to 32 lowercase letters, digits and '-', starting with a letter, such as codex/ops", s)
	}
	if harness == Person {
		return "", "", Errorf(Invalid, "%s has no names: people's identities already outlive sessions", Person)
	}
	return harness, name, nil
}

// CheckName returns an error of kind Invalid unless name is a valid NAME of
// harness.
func CheckName(harness, name string) error {
	_, _, err := ParseName(harness + "/" + name)
	return err
}

// ValidModel reports whether s is a model: 1 to 128 printable ASCII
// characters without spaces.
func ValidModel(s string) bool {
	if len(s) < 1 || len(s) > 128 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '!' || s[i] > '~' {
			return false
		}
	}
	return true
}

// CheckModel returns an error of kind Invalid unless s is empty or a model.
func CheckModel(s string) error {
	if s != "" && !ValidModel(s) {
		return Errorf(Invalid, "invalid model %q: want 1 to 128 printable ASCII characters without spaces, such as claude-opus-5-5", s)
	}
	return nil
}

// Via is what a request says of itself beyond its identity: the session
// that sends it, when its identity is another, and the model that writes.
type Via struct {
	Carrier string
	Model   string
}

type viaKey struct{}

// WithVia returns ctx carrying v, for the store.
func WithVia(ctx context.Context, v Via) context.Context { return context.WithValue(ctx, viaKey{}, v) }

// ViaOf returns what ctx carries of WithVia.
func ViaOf(ctx context.Context) Via {
	v, _ := ctx.Value(viaKey{}).(Via)
	return v
}

// Actor is who a request acts as on a board: its identity, resolved to the
// name that its session carries, and what comes with it.
type Actor struct {
	// Identity is the name, when the request acts as one, else the
	// request's identity.
	Identity
	// Name is true when Identity is a name of the board.
	Name bool
	// Carrier is the session that carries the name, when the request comes
	// from it; Model the model that writes, or empty.
	Carrier string
	Model   string
	// Adopted are the sessions that the name adopted, whose posts count as
	// its own.
	Adopted []string
	// AdoptedBy is, for a session that acts as itself, the name that
	// adopted it, which mentions of it reach instead.
	AdoptedBy string
	// Holder is the carrier of a name that the request names directly from
	// another session, displayed: the request may then only read.
	Holder string
	// Displaced is the name that another session took from the request's
	// session, until it claims a name again.
	Displaced string
}

// Sessions returns the sessions whose posts are the actor's: its own, and a
// name's adopted ones.
func (a Actor) Sessions() []string { return append([]string{a.Session}, a.Adopted...) }

// Wrote reports whether the actor is p's author, whatever machine either is
// on: for a name, also when one of its adopted sessions wrote p.
func (a Actor) Wrote(p Post) bool {
	return a.Harness == p.Harness && (a.Session == p.Session || slices.Contains(a.Adopted, p.Session))
}

// MayChange is Identity.MayChange for the actor, whose name's adopted
// sessions' posts are its own.
func (a Actor) MayChange(p Post, verb string) error {
	if a.Wrote(p) {
		return nil
	}
	return a.Identity.MayChange(p, verb)
}

// MayAct returns an error of kind Forbidden if the actor names a name that
// another session carries, as which it may only read.
func (a Actor) MayAct() error {
	if a.Holder != "" {
		return Errorf(Forbidden, "%s/%s is carried by %s: only that session writes or marks posts seen as it; this request may only read",
			a.Harness, a.Session, a.Holder)
	}
	return nil
}

// CarrierAuthor returns the carrier as it is displayed, or empty.
func (a Actor) CarrierAuthor() string {
	if a.Carrier == "" {
		return ""
	}
	return Author(a.Harness, a.Carrier, a.Node)
}

// Name is a name of a board, as api/cli.md's <name> shape.
type Name struct {
	Board   string `json:"board"`
	Name    string `json:"name"`
	Harness string `json:"harness"`
	Created string `json:"created"`
	// Carried says whether a session carries the name; Carrier, Session,
	// Node, Model, Claimed, and LastActive are that session's, or else the
	// last carrier's, empty if none ever did.
	Carried    bool   `json:"carried"`
	Carrier    string `json:"carrier"`
	Session    string `json:"session"`
	Node       string `json:"node"`
	Model      string `json:"model"`
	Claimed    string `json:"claimed"`
	LastActive string `json:"last_active"`
	Waiting    bool   `json:"waiting"`
	// Adopted are HARNESS/SESSION in full, in the order of adoption.
	Adopted []string `json:"adopted"`
	// Pending are the ids of the posts that unread --to-me would show it.
	Pending []int64 `json:"pending"`
}

// NewName asks to declare a name and to make it adopt sessions.
type NewName struct {
	Name  string   `json:"name"`
	Adopt []string `json:"adopt,omitempty"`
}

// ClaimRequest asks to claim a name.
type ClaimRequest struct {
	Name string `json:"name"`
	Take bool   `json:"take,omitempty"`
}

// MayChangeThread is Identity.MayChangeThread for the actor, which may
// resolve and reopen the threads of its name's adopted sessions.
func (a Actor) MayChangeThread(root Post, c ThreadChange) error {
	if c.State != nil && a.Wrote(root) {
		c.State = nil
	}
	return a.Identity.MayChangeThread(root, c)
}
