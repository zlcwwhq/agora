package post

import (
	"strconv"
	"strings"
)

// Refs returns the ids that body refers to with #<id>, in order of first
// appearance, without duplicates. References in fenced code blocks and inline
// code spans don't count. Which of them count is for the caller to decide:
// only those of other posts that exist.
func Refs(body string) []int64 {
	out := []int64{}
	seen := map[int64]bool{}
	for _, line := range codeFreeLines(body) {
		for _, id := range lineRefs(line) {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// fence reports the marker character and length of a code fence line, and
// whether the line could close a fence, having nothing after its marker.
func fence(line string) (char byte, n int, closing bool) {
	t := line
	for i := 0; i < 3 && strings.HasPrefix(t, " "); i++ {
		t = t[1:]
	}
	if t == "" || (t[0] != '`' && t[0] != '~') {
		return 0, 0, false
	}
	char = t[0]
	for n < len(t) && t[n] == char {
		n++
	}
	if n < 3 {
		return 0, 0, false
	}
	return char, n, strings.TrimSpace(t[n:]) == ""
}

// blankCodeSpans replaces inline code spans, including their backticks, with
// spaces. A run of backticks without a closing run of the same length is
// literal text.
func blankCodeSpans(line string) string {
	b := []byte(line)
	for i := 0; i < len(b); {
		if b[i] != '`' {
			i++
			continue
		}
		n := run(b, i)
		end := -1
		for j := i + n; j < len(b); {
			if b[j] != '`' {
				j++
				continue
			}
			m := run(b, j)
			if m == n {
				end = j + m
				break
			}
			j += m
		}
		if end < 0 {
			i += n
			continue
		}
		for k := i; k < end; k++ {
			b[k] = ' '
		}
		i = end
	}
	return string(b)
}

func run(b []byte, i int) int {
	n := 0
	for i+n < len(b) && b[i+n] == '`' {
		n++
	}
	return n
}

func isASCIIAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// lineRefs finds references in one line outside code.
func lineRefs(line string) []int64 {
	var out []int64
	for i := 0; i < len(line); i++ {
		if line[i] != '#' {
			continue
		}
		var prev byte
		if i > 0 {
			prev = line[i-1]
		}
		if id, n, ok := RefAt(prev, line[i:]); ok {
			out = append(out, id)
			i += n - 1
		}
	}
	return out
}

// RefAt reports whether s starts with a reference, #<id>, given prev, the
// byte before s on its line, or 0 at the start of a line. It returns the id
// and the reference's length in bytes. It doesn't know about code, which
// renderers of Markdown leave out themselves.
func RefAt(prev byte, s string) (int64, int, bool) {
	if s == "" || s[0] != '#' {
		return 0, 0, false
	}
	if prev != 0 && (isASCIIAlnum(prev) || strings.IndexByte("_-/#&.", prev) >= 0) {
		return 0, 0, false
	}
	k := 1
	for k < len(s) && s[k] >= '0' && s[k] <= '9' {
		k++
	}
	number := s[1:k]
	if !validNumber(number) {
		return 0, 0, false
	}
	if k < len(s) && (isASCIIAlnum(s[k]) || s[k] == '_' || s[k] == '-') {
		return 0, 0, false
	}
	n, err := strconv.ParseInt(number, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return n, k, true
}
