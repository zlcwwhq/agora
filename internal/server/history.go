package server

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/zlcwwhq/agora/internal/post"
)

func (s *server) apiHistory(w http.ResponseWriter, r *http.Request) {
	board, ok := pathBoard(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	h, err := s.Store.History(r.Context(), board, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h)
}

// history shows /b/BOARD/t/ID/history, the versions of post ID.
func (p *pages) history(w http.ResponseWriter, r *http.Request, b *post.Board, _ form) {
	id, ok := p.pathID(w, r, b, form{})
	if !ok {
		return
	}
	h, err := p.s.Store.History(r.Context(), b.ID, id)
	if err != nil {
		p.boardFailed(w, r, b, err, form{})
		return
	}
	p.render(w, r, http.StatusOK, "history", p.historyView(b, h))
}

// historyView is the data of /b/BOARD/t/ID/history.
type historyView struct {
	base
	ID, Thread int64
	// Versions are newest first.
	Versions []versionView
}

// versionView is a version of a post.
type versionView struct {
	Version int
	Who     who
	Time    stamp
	// Changes are what the version changed in the one before it, by field;
	// the first version has none, and shows Title, Tags, and Body instead.
	Changes []change
	First   bool
	Title   string
	Tags    []string
	Body    string
}

// change is what a version changed in one field of the one before it.
type change struct {
	Field string
	Lines []diffLine
}

func (p *pages) historyView(b *post.Board, h post.History) historyView {
	v := historyView{base: p.base(b, "", fmt.Sprintf("Versions of #%d", h.ID), query{}), ID: h.ID, Thread: h.Thread}
	for i, ver := range h.Versions {
		vv := versionView{
			Version: ver.Version, Who: parseWho(ver.Author), Time: p.stamp(ver.Time),
			First: i == 0, Title: ver.Title, Tags: ver.Tags, Body: ver.Body,
		}
		if i > 0 {
			before := h.Versions[i-1]
			for _, c := range []change{
				{Field: "Title", Lines: diffLines(lines(before.Title), lines(ver.Title))},
				{Field: "Labels", Lines: diffLines(before.Tags, ver.Tags)},
				{Field: "Message", Lines: diffLines(lines(before.Body), lines(ver.Body))},
			} {
				if slices.ContainsFunc(c.Lines, func(l diffLine) bool { return l.Op == removed || l.Op == added }) {
					vv.Changes = append(vv.Changes, c)
				}
			}
		}
		v.Versions = append(v.Versions, vv)
	}
	slices.Reverse(v.Versions)
	return v
}

// lines splits text into its lines, without the newline that ends a body.
func lines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(text, "\n"), "\n")
}

// What a line of a diff is.
const (
	same    = ' '
	removed = '-'
	added   = '+'
	// gap stands for lines that both versions have, left out.
	gap = '~'
)

// diffLine is a line of a diff: Op is same, removed, added, or gap.
type diffLine struct {
	Op   byte
	Text string
}

// Class returns the line's class on the page.
func (l diffLine) Class() string {
	switch l.Op {
	case removed:
		return "del"
	case added:
		return "add"
	case gap:
		return "gap"
	}
	return "ctx"
}

// Sign returns the mark that the page shows before the line.
func (l diffLine) Sign() string { return string(l.Op) }

// diffContext is the number of lines that both versions have that a diff
// shows around each change.
const diffContext = 2

// maxDiffCells bounds the table of the longest common subsequence; larger
// changes show every line between the common start and end as removed and
// added.
const maxDiffCells = 4 << 20

// diffLines returns the lines that a removed and b added, in order, with the
// lines that both have around each change.
func diffLines(a, b []string) []diffLine {
	start := 0
	for start < len(a) && start < len(b) && a[start] == b[start] {
		start++
	}
	end := 0
	for end < len(a)-start && end < len(b)-start && a[len(a)-1-end] == b[len(b)-1-end] {
		end++
	}
	var all []diffLine
	for _, l := range a[:start] {
		all = append(all, diffLine{same, l})
	}
	all = append(all, middle(a[start:len(a)-end], b[start:len(b)-end])...)
	for _, l := range a[len(a)-end:] {
		all = append(all, diffLine{same, l})
	}
	return withContext(all)
}

// middle diffs a and b by their longest common subsequence.
func middle(a, b []string) []diffLine {
	var out []diffLine
	if len(a)*len(b) > maxDiffCells {
		for _, l := range a {
			out = append(out, diffLine{removed, l})
		}
		for _, l := range b {
			out = append(out, diffLine{added, l})
		}
		return out
	}
	// lcs[i][j] is the length of the longest common subsequence of a[i:]
	// and b[j:].
	w := len(b) + 1
	lcs := make([]int32, (len(a)+1)*w)
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i*w+j] = lcs[(i+1)*w+j+1] + 1
			} else {
				lcs[i*w+j] = max(lcs[(i+1)*w+j], lcs[i*w+j+1])
			}
		}
	}
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			out = append(out, diffLine{same, a[i]})
			i, j = i+1, j+1
		case i < len(a) && (j == len(b) || lcs[(i+1)*w+j] >= lcs[i*w+j+1]):
			out = append(out, diffLine{removed, a[i]})
			i++
		default:
			out = append(out, diffLine{added, b[j]})
			j++
		}
	}
	return out
}

// withContext keeps the lines that changed and diffContext lines around
// each, and replaces each run of other lines with a gap.
func withContext(all []diffLine) []diffLine {
	near := make([]bool, len(all))
	for i, l := range all {
		if l.Op == same {
			continue
		}
		for k := max(0, i-diffContext); k <= min(len(all)-1, i+diffContext); k++ {
			near[k] = true
		}
	}
	var out []diffLine
	for i, l := range all {
		switch {
		case near[i]:
			out = append(out, l)
		case len(out) == 0 || out[len(out)-1].Op != gap:
			out = append(out, diffLine{Op: gap})
		}
	}
	return out
}
