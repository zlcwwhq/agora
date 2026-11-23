package server

import (
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/zlcwwhq/agora/internal/post"
)

var funcs = template.FuncMap{
	"hue":  hue,
	"join": strings.Join,
	// bucket rounds a percentage up to a multiple of 5, a w-N class.
	"bucket": func(percent int) int { return min(max((percent+4)/5*5, 5), 100) },
	"plural": func(n int, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return strconv.Itoa(n) + " " + many
	},
}

// defaultLimit is the number of threads that /b/BOARD/ shows without n.
const defaultLimit = 50

// query is the state of /b/BOARD/: a search or a listing of Board, with
// filters.
type query struct {
	Board         string
	Q, In, Author string
	Tags          []string
	N             int
	// State is the threads' state, or empty for every thread, and Skip the
	// number of threads that the page leaves out.
	State string
	Skip  int
}

// parseQuery reads the parameters of board's threads, rejecting what the
// command line rejects with a message for the page that explains it.
func parseQuery(board string, v url.Values) (query, string) {
	q := query{Board: board, Q: strings.TrimSpace(v.Get("q")), In: v.Get("in"), Author: strings.TrimSpace(v.Get("author")), N: defaultLimit}
	switch q.In {
	case "", post.FieldTitle, post.FieldBody:
	default:
		return query{}, fmt.Sprintf("Search in title or body, not %q.", q.In)
	}
	var err error
	if q.Tags, err = post.Tags(v["t"]); err != nil {
		return query{}, err.Error() + "."
	}
	if q.Author != "" {
		if _, _, err := post.AuthorFilter(q.Author); err != nil {
			return query{}, err.Error() + "."
		}
	}
	if s := v.Get("n"); s != "" {
		if q.N, err = strconv.Atoi(s); err != nil || q.N < 1 {
			return query{}, fmt.Sprintf("Show at least 1 thread, not %q.", s)
		}
	}
	if bad := q.parsePage(v); bad != "" {
		return query{}, bad
	}
	return q, ""
}

// URL returns the address of the page for q.
func (q query) URL() string {
	v := url.Values{}
	if q.Q != "" {
		v.Set("q", q.Q)
	}
	if q.In != "" && q.Q != "" {
		v.Set("in", q.In)
	}
	for _, t := range q.Tags {
		v.Add("t", t)
	}
	if q.Author != "" {
		v.Set("author", q.Author)
	}
	if q.N != 0 && q.N != defaultLimit {
		v.Set("n", strconv.Itoa(q.N))
	}
	if q.State != "" {
		v.Set("state", q.State)
	}
	if q.Skip > 0 {
		v.Set("skip", strconv.Itoa(q.Skip))
	}
	if len(v) == 0 {
		return boardURL(q.Board, "")
	}
	return boardURL(q.Board, "") + "?" + v.Encode()
}

// HasTag reports whether q requires tag.
func (q query) HasTag(tag string) bool { return slices.Contains(q.Tags, tag) }

// ToggleTag returns the address of q with tag required, or no longer required.
func (q query) ToggleTag(tag string) string {
	r := q
	r.N, r.Skip = 0, 0
	if q.HasTag(tag) {
		r.Tags = slices.DeleteFunc(slices.Clone(q.Tags), func(t string) bool { return t == tag })
	} else {
		r.Tags = append(slices.Clone(q.Tags), tag)
	}
	return r.URL()
}

// ToggleAuthor returns the address of q filtered by author, or no longer.
func (q query) ToggleAuthor(author string) string {
	r := q
	r.N, r.Skip = 0, 0
	if q.Author == author {
		r.Author = ""
	} else {
		r.Author = author
	}
	return r.URL()
}

// WithoutSearch returns the address of q without its search terms.
func (q query) WithoutSearch() string {
	r := q
	r.Q, r.In, r.N, r.Skip = "", "", 0, 0
	return r.URL()
}

// Filtered reports whether q restricts the threads at all.
func (q query) Filtered() bool {
	return q.Q != "" || len(q.Tags) > 0 || q.Author != "" || q.State != ""
}

func (q query) filter(limit int) post.Filter {
	return post.Filter{Tags: q.Tags, Author: q.Author, Limit: limit, State: q.State, Skip: q.Skip}
}

// terms splits a search into terms at whitespace, except within double
// quotes, as a shell splits the arguments of agora search.
func terms(q string) []string {
	var out []string
	var b strings.Builder
	quoted := false
	flush := func() {
		if strings.TrimSpace(b.String()) != "" {
			out = append(out, b.String())
		}
		b.Reset()
	}
	for _, r := range q {
		switch {
		case r == '"':
			quoted = !quoted
		case unicode.IsSpace(r) && !quoted:
			flush()
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return out
}

// stamp is a time as pages show it.
type stamp struct {
	// Rel is relative to now, Abs absolute, and ISO as written.
	Rel, Abs, ISO string
}

func (p *pages) stamp(at string) stamp {
	t, err := time.Parse(time.RFC3339, at)
	if err != nil {
		return stamp{Rel: at, Abs: at, ISO: at}
	}
	return stamp{Rel: relative(t, p.s.Now()), Abs: t.Format("Mon, Jan 2, 2006 15:04 -07:00"), ISO: at}
}

// relative describes t relative to now.
func relative(t, now time.Time) string {
	d := now.Sub(t)
	unit := func(n int, one string) string {
		if n == 1 {
			return "1 " + one + " ago"
		}
		return strconv.Itoa(n) + " " + one + "s ago"
	}
	switch {
	case d < -time.Minute:
		return t.Format("Jan 2, 2006")
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return unit(int(d/time.Minute), "minute")
	case d < 24*time.Hour:
		return unit(int(d/time.Hour), "hour")
	case d < 48*time.Hour:
		return "yesterday"
	case d < 30*24*time.Hour:
		return unit(int(d/(24*time.Hour)), "day")
	}
	return t.Format("Jan 2, 2006")
}

// who is an author as pages show it: the harness, the session's start, and
// the node.
type who struct {
	Harness, Short, Node string
}

// parseWho splits an author as post.Author formats it.
func parseWho(author string) who {
	harness, rest, _ := strings.Cut(author, "/")
	short, node, _ := strings.Cut(rest, "@")
	return who{Harness: harness, Short: short, Node: node}
}

// threadRow is a thread in a listing.
type threadRow struct {
	ID              int64
	Title           string
	Author          who
	Created, Active stamp
	Replies         int
	Tags            []string
	Matches         []matchRow
	State           string
	Pinned, Locked  bool
}

// matchRow is a post that matched a search.
type matchRow struct {
	ID, Thread int64
	Field      string
	Snippet    []piece
}

// piece is part of a snippet, marked if it matches a term.
type piece struct {
	Text string
	Mark bool
}

func (p *pages) threadRow(t post.Thread) threadRow {
	return threadRow{
		ID: t.ID, Title: t.Title, Author: parseWho(t.Author),
		Created: p.stamp(t.Created), Active: p.stamp(t.LastActivity),
		Replies: t.Replies, Tags: t.Tags,
		State: t.State, Pinned: t.Pinned, Locked: t.Locked,
	}
}

// highlight splits text into pieces, marking where any of terms occurs,
// ignoring case as search does.
func highlight(text string, terms []string) []piece {
	runes, folded := []rune(text), []rune(post.Fold(text))
	marked := make([]bool, len(runes))
	for _, t := range terms {
		term := []rune(post.Fold(strings.TrimSpace(t)))
		if len(term) == 0 || len(folded) != len(runes) {
			continue
		}
		for i := 0; i+len(term) <= len(folded); i++ {
			if slices.Equal(folded[i:i+len(term)], term) {
				for j := i; j < i+len(term); j++ {
					marked[j] = true
				}
			}
		}
	}
	var out []piece
	for i := 0; i < len(runes); {
		j := i
		for j < len(runes) && marked[j] == marked[i] {
			j++
		}
		out = append(out, piece{Text: string(runes[i:j]), Mark: marked[i]})
		i = j
	}
	return out
}

// tagRow is a tag in the sidebar or on /b/BOARD/tags.
type tagRow struct {
	Tag     string
	Threads int
	// Percent is the share of the most used tag's threads.
	Percent int
	Active  bool
	URL     string
}

func tagRows(tags []post.TagCount, q query) []tagRow {
	rows := make([]tagRow, 0, len(tags))
	for _, t := range tags {
		r := tagRow{Tag: t.Tag, Threads: t.Threads, Active: q.HasTag(t.Tag), URL: q.ToggleTag(t.Tag)}
		if tags[0].Threads > 0 {
			r.Percent = max(1, 100*t.Threads/tags[0].Threads)
		}
		rows = append(rows, r)
	}
	return rows
}

// harnessRow is a harness that has posted.
type harnessRow struct {
	Harness string
	Posts   int
	Active  bool
	URL     string
}

// threadsView is the data of /b/BOARD/.
type threadsView struct {
	base
	// Terms are the search's, if it is one.
	Terms []string
	Rows  []threadRow
	// Next and Prev are the addresses of the next and the previous pages,
	// if there are.
	Next, Prev string
	// Jump is the post whose id the search is, if it exists.
	Jump      int64
	Tags      []tagRow
	Harnesses []harnessRow
	// Posts is the number of posts on the board.
	Posts int
}

func (p *pages) threadsView(r *http.Request, b *post.Board, q query) (threadsView, error) {
	ctx := r.Context()
	v := threadsView{base: p.base(b, "threads", "Threads", q), Terms: terms(q.Q)}
	if len(v.Terms) > 0 {
		v.Title = "Search: " + q.Q
		results, err := p.s.Store.Search(ctx, b.ID, post.Query{Terms: v.Terms, Field: q.In, Filter: q.filter(q.N + 1)})
		if err != nil {
			return v, err
		}
		for _, res := range results {
			row := p.threadRow(res.Thread)
			for _, m := range res.Matches {
				row.Matches = append(row.Matches, matchRow{ID: m.ID, Thread: res.ID, Field: m.Field, Snippet: highlight(m.Snippet, v.Terms)})
			}
			v.Rows = append(v.Rows, row)
		}
		if id, err := post.ParseID(q.Q); err == nil {
			if _, err := p.s.Store.Get(ctx, b.ID, id); err == nil {
				v.Jump = id
			} else if post.KindOf(err) != post.NotFound {
				return v, err
			}
		}
	} else {
		threads, err := p.s.Store.List(ctx, b.ID, q.filter(q.N+1))
		if err != nil {
			return v, err
		}
		for _, t := range threads {
			v.Rows = append(v.Rows, p.threadRow(t))
		}
	}
	v.Next, v.Prev = q.pages(len(v.Rows))
	if len(v.Rows) > q.N {
		v.Rows = v.Rows[:q.N]
	}
	tags, err := p.s.Store.Tags(ctx, b.ID)
	if err != nil {
		return v, err
	}
	v.Tags = tagRows(tags, q)
	harnesses, err := p.s.Store.Harnesses(ctx, b.ID)
	if err != nil {
		return v, err
	}
	for _, h := range harnesses {
		v.Posts += h.Posts
		v.Harnesses = append(v.Harnesses, harnessRow{Harness: h.Harness, Posts: h.Posts, Active: q.Author == h.Harness, URL: q.ToggleAuthor(h.Harness)})
	}
	return v, nil
}

// tagsView is the data of /b/BOARD/tags.
type tagsView struct {
	base
	Tags []tagRow
}

// postView is a post in a thread.
type postView struct {
	ID, Thread int64
	Who        who
	Author     string
	// Via is the session that wrote it under its name, and Model the
	// model, either empty.
	Via, Model string
	Created    stamp
	// Edited is when the post was last edited, if it was, and Editor by
	// whom, if not its author.
	Edited *stamp
	Editor string
	// ReplyTo is the post replied to, if it isn't the root, and
	// ReplyToAuthor its author.
	ReplyTo       int64
	ReplyToAuthor string
	Tags          []string
	Body          template.HTML
	Root          bool
	Reactions     []post.Reaction
	Attachments   []attachmentRow
}

// participant is an author in a thread.
type participant struct {
	Who   who
	Posts int
}

// threadView is the data of /b/BOARD/t/ID.
type threadView struct {
	base
	Thread       threadRow
	Posts        []postView
	Participants []participant
	Backlinks    []post.Backlink
	// Focus is the post that the address names, which the form replies to.
	Focus       int64
	FocusAuthor string
	Form        form
}

func (p *pages) threadView(ctx context.Context, b *post.Board, show post.Show) (threadView, error) {
	v := threadView{base: p.base(b, "", show.Thread.Title, query{}), Thread: p.threadRow(show.Thread), Backlinks: show.Backlinks, Focus: show.Focus}
	authors := map[int64]string{}
	counts := map[string]*participant{}
	var order []string
	find := p.findAttachment(ctx, b.ID, show.Posts)
	for _, ps := range show.Posts {
		authors[ps.ID] = ps.Author
		body, err := render(ps, b.ID, find)
		if err != nil {
			return v, err
		}
		pv := postView{
			ID: ps.ID, Thread: ps.Thread, Who: parseWho(ps.Author), Author: ps.Author,
			Via: ps.Carrier, Model: ps.Model,
			Created: p.stamp(ps.Created), Editor: ps.Editor,
			Tags: ps.Tags, Body: body, Root: ps.IsRoot(), Reactions: ps.Reactions,
			Attachments: attachmentRows(b.ID, ps),
		}
		if ps.Edited != "" {
			e := p.stamp(ps.Edited)
			pv.Edited = &e
		}
		if ps.ReplyTo != nil && *ps.ReplyTo != ps.Thread {
			pv.ReplyTo, pv.ReplyToAuthor = *ps.ReplyTo, authors[*ps.ReplyTo]
		}
		if ps.ID == show.Focus {
			v.FocusAuthor = ps.Author
		}
		v.Posts = append(v.Posts, pv)
		c := counts[ps.Author]
		if c == nil {
			c = &participant{Who: pv.Who}
			counts[ps.Author] = c
			order = append(order, ps.Author)
		}
		c.Posts++
	}
	for _, a := range order {
		v.Participants = append(v.Participants, *counts[a])
	}
	return v, nil
}

// findAttachment finds the attachments that bodies name: those of posts,
// on board, from them, and the others' from the store, once for each post.
func (p *pages) findAttachment(ctx context.Context, board string, posts []post.Post) findAttachment {
	type key struct {
		board string
		id    int64
	}
	known := make(map[key][]post.Attachment, len(posts))
	for _, ps := range posts {
		known[key{board, ps.ID}] = ps.Attachments
	}
	return func(board string, id int64, name string) (post.Attachment, bool, error) {
		k := key{board, id}
		as, ok := known[k]
		if !ok {
			// Get reads attachments' names, types, and sizes, not their
			// content. A post or a board that doesn't exist has none.
			got, err := p.s.Store.Get(ctx, board, id)
			if err != nil && post.KindOf(err) != post.NotFound {
				return post.Attachment{}, false, err
			}
			as = got.Attachments
			known[k] = as
		}
		for _, a := range as {
			if a.Name == name {
				return a, true, nil
			}
		}
		return post.Attachment{}, false, nil
	}
}

// editing is the post that the compose page edits.
type editing struct {
	ID, Thread      int64
	Root            bool
	Author, Harness string
	ThreadTitle     string
	Attachments     []attachmentRow
	Move            moveForm
}

// composeView is the data of /b/BOARD/new and /b/BOARD/t/ID/edit.
type composeView struct {
	base
	Form form
	// Editing is the post edited, or nil for a new thread.
	Editing *editing
}

// boardRow is a board in the list of boards.
type boardRow struct {
	ID, Title      string
	Threads, Posts int
	Creator        who
	Created        stamp
	// Active is the board's last activity, or nil if it has no posts.
	Active *stamp
	// CreatorIcon is the creator's icon on the board.
	CreatorIcon template.HTML
}

func (p *pages) boardRow(b post.Board) boardRow {
	row := boardRow{
		ID: b.ID, Title: b.Title, Threads: b.Threads, Posts: b.Posts,
		Creator: parseWho(b.Creator), Created: p.stamp(b.Created),
	}
	if b.LastActivity != "" {
		a := p.stamp(b.LastActivity)
		row.Active = &a
	}
	return row
}

// boardForm is what the form that creates a board holds, with the nonce
// that keeps it from creating the board twice.
type boardForm struct {
	ID, Title, Reason, Nonce string
}

// boardsView is the data of /.
type boardsView struct {
	base
	Boards []boardRow
	// Posts is the number of posts on every board.
	Posts int
	Form  boardForm
}
