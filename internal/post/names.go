package post

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	nodePattern    = regexp.MustCompile(`^[a-z][a-z0-9]{0,15}$`)
	harnessPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	sessionPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)
)

// reserved are the device names that Windows reserves, in any case and with
// any extension, which therefore can't name synchronized directories.
var reserved = func() map[string]bool {
	m := map[string]bool{"con": true, "prn": true, "aux": true, "nul": true}
	for i := '0'; i <= '9'; i++ {
		m["com"+string(i)] = true
		m["lpt"+string(i)] = true
	}
	return m
}()

// ValidNode reports whether s is a valid node name.
func ValidNode(s string) bool { return nodePattern.MatchString(s) && !reserved[s] }

// ValidHarness reports whether s is a valid harness name.
func ValidHarness(s string) bool { return harnessPattern.MatchString(s) }

// ValidSession reports whether s is a valid session id.
func ValidSession(s string) bool { return sessionPattern.MatchString(s) }

// ID identifies a post: the node that wrote it and its number there.
type ID struct {
	Node string
	N    int
}

// IsZero reports whether id is the zero ID, which identifies no post.
func (id ID) IsZero() bool { return id == ID{} }

// String formats id as <node>-<n>.
func (id ID) String() string {
	if id.IsZero() {
		return ""
	}
	return id.Node + "-" + strconv.Itoa(id.N)
}

// Less orders ids by node, then by number.
func (id ID) Less(other ID) bool {
	if id.Node != other.Node {
		return id.Node < other.Node
	}
	return id.N < other.N
}

// ParseID parses <node>-<n>, optionally preceded by '#'.
func ParseID(s string) (ID, error) {
	t := strings.TrimPrefix(s, "#")
	node, number, ok := strings.Cut(t, "-")
	if !ok || !ValidNode(node) || !validNumber(number) {
		return ID{}, fmt.Errorf("invalid post id %q: want <node>-<n>, such as wsl-12", s)
	}
	n, err := strconv.Atoi(number)
	if err != nil {
		return ID{}, fmt.Errorf("invalid post id %q: %w", s, err)
	}
	return ID{Node: node, N: n}, nil
}

// validNumber reports whether s is a decimal number of at least 1 without
// leading zeros, short enough to fit an int.
func validNumber(s string) bool {
	if s == "" || len(s) > 18 || s[0] < '1' || s[0] > '9' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// FileName returns the name of the post's file within its node's directory.
func FileName(n int) string { return strconv.Itoa(n) + ".md" }

// ParseFileName returns the number of a post file name, <n>.md.
func ParseFileName(name string) (int, bool) {
	number, ok := strings.CutSuffix(name, ".md")
	if !ok || !validNumber(number) {
		return 0, false
	}
	n, err := strconv.Atoi(number)
	return n, err == nil
}

// NormalizeTag trims and lowercases a tag, reporting whether the result is a
// valid tag.
func NormalizeTag(s string) (string, bool) {
	t := strings.ToLower(strings.TrimSpace(s))
	if n := utf8.RuneCountInString(t); n < 1 || n > 32 {
		return "", false
	}
	for _, r := range t {
		if unicode.IsSpace(r) || r == ',' || r == '#' {
			return "", false
		}
	}
	return t, true
}

// NormalizeTags normalizes tags and drops duplicates, keeping the first
// occurrence of each. It fails on the first invalid tag.
func NormalizeTags(tags []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, tag := range tags {
		t, ok := NormalizeTag(tag)
		if !ok {
			return nil, fmt.Errorf("invalid tag %q: tags have 1 to 32 characters, without whitespace, ',' or '#'", tag)
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out, nil
}

// NormalizeBody drops a leading byte order mark and converts \r\n line
// endings to \n, as writers do before rendering a body.
func NormalizeBody(s string) string {
	return strings.ReplaceAll(strings.TrimPrefix(s, "\uFEFF"), "\r\n", "\n")
}

// Fold case-folds s for searching. It maps each rune on its own, so the result
// has as many runes as s, and rune offsets carry over between the two.
func Fold(s string) string { return strings.Map(unicode.ToLower, s) }

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
