package post

import (
	"strconv"
	"strings"
)

// Refs returns the ids that body references with #<node>-<n>, in order of
// first appearance, without self and without duplicates. References in fenced
// code blocks and inline code spans don't count.
func Refs(body string, self ID) []ID {
	out := []ID{}
	seen := map[ID]bool{self: true}
	var fenceChar byte
	var fenceLen int
	for _, line := range strings.Split(body, "\n") {
		if char, n, closing := fence(line); n > 0 {
			if fenceLen == 0 {
				fenceChar, fenceLen = char, n
				continue
			}
			if char == fenceChar && n >= fenceLen && closing {
				fenceLen = 0
				continue
			}
		}
		if fenceLen > 0 {
			continue
		}
		for _, id := range lineRefs(blankCodeSpans(line)) {
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
func lineRefs(line string) []ID {
	var out []ID
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

// RefAt reports whether s starts with a reference, #<node>-<n>, given prev,
// the byte before s on its line, or 0 at the start of a line. It returns the
// referenced id and the reference's length in bytes. It doesn't know about
// code, which renderers of Markdown leave out themselves.
func RefAt(prev byte, s string) (ID, int, bool) {
	if s == "" || s[0] != '#' {
		return ID{}, 0, false
	}
	if prev != 0 && (isASCIIAlnum(prev) || strings.IndexByte("_-/#&.", prev) >= 0) {
		return ID{}, 0, false
	}
	j := 1
	if j >= len(s) || s[j] < 'a' || s[j] > 'z' {
		return ID{}, 0, false
	}
	for j < len(s) && (s[j] >= 'a' && s[j] <= 'z' || s[j] >= '0' && s[j] <= '9') {
		j++
	}
	node := s[1:j]
	if !ValidNode(node) || j >= len(s) || s[j] != '-' {
		return ID{}, 0, false
	}
	k := j + 1
	for k < len(s) && s[k] >= '0' && s[k] <= '9' {
		k++
	}
	number := s[j+1 : k]
	if !validNumber(number) {
		return ID{}, 0, false
	}
	if k < len(s) && (isASCIIAlnum(s[k]) || s[k] == '_' || s[k] == '-') {
		return ID{}, 0, false
	}
	n, err := strconv.Atoi(number)
	if err != nil {
		return ID{}, 0, false
	}
	return ID{Node: node, N: n}, k, true
}
