package post

import "strings"

// Mention is a mention in a body, @HARNESS or @HARNESS/SESSION: Session is
// empty for every session of the harness, or the start of the sessions that
// it mentions.
type Mention struct {
	Harness, Session string
}

// String returns the mention as written, without its @.
func (m Mention) String() string {
	if m.Session == "" {
		return m.Harness
	}
	return m.Harness + "/" + m.Session
}

// Names reports whether m mentions the reader harness/session: @HARNESS
// mentions every session of the harness, and @HARNESS/PREFIX every session
// that starts with PREFIX.
func (m Mention) Names(harness, session string) bool {
	return m.Harness == harness && strings.HasPrefix(session, m.Session)
}

// Mentions returns the mentions in body, in order of first appearance,
// without duplicates. Mentions in fenced code blocks and inline code spans
// don't count, as for references.
func Mentions(body string) []Mention {
	out := []Mention{}
	seen := map[Mention]bool{}
	for _, line := range codeFreeLines(body) {
		for i := 0; i < len(line); i++ {
			if line[i] != '@' {
				continue
			}
			var prev byte
			if i > 0 {
				prev = line[i-1]
			}
			m, n, ok := MentionAt(prev, line[i:])
			if !ok {
				continue
			}
			if !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
			i += n - 1
		}
	}
	return out
}

// MentionAt reports whether s starts with a mention, given prev, the byte
// before s on its line, or 0 at the start of a line. It returns the mention
// and its length in bytes, its @ included. It doesn't know about code, which
// renderers of Markdown leave out themselves.
func MentionAt(prev byte, s string) (Mention, int, bool) {
	if s == "" || s[0] != '@' {
		return Mention{}, 0, false
	}
	if prev != 0 && (isASCIIAlnum(prev) || strings.IndexByte("_-/.@", prev) >= 0) {
		return Mention{}, 0, false
	}
	k := 1 + span(s[1:], isHarnessByte)
	m := Mention{Harness: s[1:k]}
	if !ValidHarness(m.Harness) {
		return Mention{}, 0, false
	}
	if k < len(s) && s[k] == '/' {
		run := s[k+1:][:span(s[k+1:], isSessionByte)]
		// Dots that end a session end a sentence instead.
		m.Session = strings.TrimRight(run, ".")
		if !ValidSession(m.Session) {
			return Mention{}, 0, false
		}
		k += 1 + len(m.Session)
	}
	if k < len(s) && (isASCIIAlnum(s[k]) || s[k] == '_' || s[k] == '-' || s[k] == '/') {
		return Mention{}, 0, false
	}
	return m, k, true
}

func isHarnessByte(c byte) bool { return c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' }

func isSessionByte(c byte) bool { return isASCIIAlnum(c) || c == '.' || c == '_' || c == '-' }

// span returns the length of the longest prefix of s whose bytes satisfy ok.
func span(s string, ok func(byte) bool) int {
	n := 0
	for n < len(s) && ok(s[n]) {
		n++
	}
	return n
}

// codeFreeLines returns body's lines outside fenced code blocks, with their
// inline code spans blanked, which is where references and mentions count.
func codeFreeLines(body string) []string {
	var out []string
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
		out = append(out, blankCodeSpans(line))
	}
	return out
}
