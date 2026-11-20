package post

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ParseID parses an id, a positive decimal integer without leading zeros,
// optionally preceded by '#'.
func ParseID(s string) (int64, error) {
	t := strings.TrimPrefix(s, "#")
	if !validNumber(t) {
		return 0, Errorf(Invalid, "invalid post id %q: want a number such as 12", s)
	}
	n, err := strconv.ParseInt(t, 10, 64)
	if err != nil {
		return 0, Errorf(Invalid, "invalid post id %q: %v", s, err)
	}
	return n, nil
}

// validNumber reports whether s is a decimal number of at least 1 without
// leading zeros, short enough to fit an int64.
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

// Title checks a root's title and returns it without surrounding whitespace.
func Title(s string) (string, error) {
	if !utf8.ValidString(s) {
		return "", Errorf(Invalid, "the title isn't valid UTF-8")
	}
	if strings.ContainsAny(s, "\r\n") {
		return "", Errorf(Invalid, "the title must be a single line")
	}
	t := strings.TrimSpace(s)
	if t == "" {
		return "", Errorf(Invalid, "a thread needs a title")
	}
	return t, nil
}

// Body normalizes a body: it drops a leading byte order mark, converts \r\n
// line endings to \n, and ends a body that isn't empty with a newline.
func Body(s string) (string, error) {
	if !utf8.ValidString(s) {
		return "", Errorf(Invalid, "the body isn't valid UTF-8")
	}
	b := strings.ReplaceAll(strings.TrimPrefix(s, "\uFEFF"), "\r\n", "\n")
	if b != "" && !strings.HasSuffix(b, "\n") {
		b += "\n"
	}
	return b, nil
}

// ReplyBody normalizes a reply's body, which mustn't be empty.
func ReplyBody(s string) (string, error) {
	b, err := Body(s)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(b) == "" {
		return "", Errorf(Invalid, "a reply needs a body")
	}
	return b, nil
}

// NormalizeTag trims and lowercases a tag, reporting whether the result is a
// valid tag.
func NormalizeTag(s string) (string, bool) {
	if !utf8.ValidString(s) {
		return "", false
	}
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

// Tags normalizes tags and drops duplicates, keeping the first occurrence of
// each. It fails on the first invalid tag.
func Tags(tags []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, tag := range tags {
		t, ok := NormalizeTag(tag)
		if !ok {
			return nil, Errorf(Invalid, "invalid tag %q: tags have 1 to 32 characters, without whitespace, ',' or '#'", tag)
		}
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out, nil
}

// SplitTags splits a form's field of tags, separated by whitespace or commas.
func SplitTags(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == ',' })
}

// Fold case-folds s for searching. It maps each rune on its own, so the result
// has as many runes as s, and rune offsets carry over between the two.
func Fold(s string) string { return strings.Map(unicode.ToLower, s) }

var noncePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// ValidNonce reports whether s is a valid nonce, or empty.
func ValidNonce(s string) bool { return s == "" || noncePattern.MatchString(s) }

// ParseSince parses a lookback such as 90m or 2d: a non-negative integer
// followed by s, m, h, or d.
func ParseSince(s string) (time.Duration, error) {
	unit := map[byte]time.Duration{'s': time.Second, 'm': time.Minute, 'h': time.Hour, 'd': 24 * time.Hour}
	if len(s) < 2 || unit[s[len(s)-1]] == 0 {
		return 0, fmt.Errorf("invalid duration %q: want a number followed by s, m, h, or d, such as 90m or 2d", s)
	}
	n, err := strconv.ParseInt(s[:len(s)-1], 10, 64)
	if err != nil || n < 0 || n > int64(100*365*24*time.Hour/unit[s[len(s)-1]]) {
		return 0, fmt.Errorf("invalid duration %q: want a number followed by s, m, h, or d, such as 90m or 2d", s)
	}
	return time.Duration(n) * unit[s[len(s)-1]], nil
}
