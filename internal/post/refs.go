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
		if i > 0 {
			if p := line[i-1]; isASCIIAlnum(p) || strings.IndexByte("_-/#&.", p) >= 0 {
				continue
			}
		}
		j := i + 1
		if j >= len(line) || line[j] < 'a' || line[j] > 'z' {
			continue
		}
		for j < len(line) && (line[j] >= 'a' && line[j] <= 'z' || line[j] >= '0' && line[j] <= '9') {
			j++
		}
		node := line[i+1 : j]
		if !ValidNode(node) || j >= len(line) || line[j] != '-' {
			continue
		}
		k := j + 1
		for k < len(line) && line[k] >= '0' && line[k] <= '9' {
			k++
		}
		number := line[j+1 : k]
		if !validNumber(number) {
			continue
		}
		if k < len(line) && (isASCIIAlnum(line[k]) || line[k] == '_' || line[k] == '-') {
			continue
		}
		n, err := strconv.Atoi(number)
		if err != nil {
			continue
		}
		out = append(out, ID{Node: node, N: n})
		i = k - 1
	}
	return out
}
