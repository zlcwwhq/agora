package web

import (
	"html/template"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/zlcwwhq/agora/internal/index"
	"github.com/zlcwwhq/agora/internal/post"
)

var funcs = template.FuncMap{
	"icon": icon,
	"hue":  hue,
	// bucket rounds a percentage up to a multiple of 5, a w-N class.
	"bucket": func(percent int) int { return min(max((percent+4)/5*5, 5), 100) },
	"plural": func(n int, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return strconv.Itoa(n) + " " + many
	},
}

// defaultLimit is the number of threads that / shows without n, and the
// number that each "more" adds.
const defaultLimit = 50

// query is the state of /: a search or a listing, with filters.
type query struct {
	Q, In, Author string
	Tags          []string
	N             int
}

// parseQuery reads /'s parameters, rejecting what the command line rejects.
func parseQuery(v url.Values) (query, error) {
	q := query{Q: strings.TrimSpace(v.Get("q")), In: v.Get("in"), Author: strings.TrimSpace(v.Get("author")), N: defaultLimit}
	switch q.In {
	case "", "title", "body":
	default:
		return query{}, badRequest("Search in title or body, not %q.", q.In)
	}
	var err error
	if q.Tags, err = post.NormalizeTags(v["t"]); err != nil {
		return query{}, badRequest("Invalid tag: %v.", err)
	}
	if q.Author != "" {
		if harness, _, _ := strings.Cut(q.Author, "/"); !post.ValidHarness(harness) {
			return query{}, badRequest("Invalid author %q: use HARNESS or HARNESS/SESSION-PREFIX.", q.Author)
		}
	}
	if s := v.Get("n"); s != "" {
		if q.N, err = strconv.Atoi(s); err != nil || q.N < 1 {
			return query{}, badRequest("Show at least 1 thread, not %q.", s)
		}
	}
	return q, nil
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
	if len(v) == 0 {
		return "/"
	}
	return "/?" + v.Encode()
}

// HasTag reports whether q requires tag.
func (q query) HasTag(tag string) bool { return slices.Contains(q.Tags, tag) }

// ToggleTag returns the address of q with tag required, or no longer required.
func (q query) ToggleTag(tag string) string {
	r := q
	r.N = 0
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
	r.N = 0
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
	r.Q, r.In, r.N = "", "", 0
	return r.URL()
}

// Filtered reports whether q restricts the threads at all.
func (q query) Filtered() bool { return q.Q != "" || len(q.Tags) > 0 || q.Author != "" }

func (q query) filter() index.Filter {
	harness, session, _ := strings.Cut(q.Author, "/")
	return index.Filter{Tags: q.Tags, Harness: harness, SessionPrefix: session}
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

// base is what every page's layout shows.
type base struct {
	Board, Node string
	// Nav is the current section: threads, tags, new, or empty.
	Nav   string
	Title string
	// Query fills the search form.
	Query query
	// Forms says whether pages offer forms, which post as Author and carry
	// Token.
	Forms         bool
	Author, Token string
}

func (s *server) base(nav, title string, q query) base {
	b := base{Board: s.cfg.Board, Node: s.cfg.Node, Nav: nav, Title: title, Query: q, Forms: s.forms}
	if s.forms {
		b.Author, b.Token = s.cfg.Author, s.token
	}
	return b
}

// form is what a form holds: what was typed, why posting it was refused, and
// the nonce that keeps it from posting twice.
type form struct {
	Title, Tags, Body, Reason, Nonce string
}

// freshForm is an empty form with a nonce of its own.
func freshForm() form { return form{Nonce: newToken()} }

// newView is the data of /new.
type newView struct {
	base
	Form form
}

func (s *server) newThread(_ *http.Request, _ *index.DB) (string, any, error) {
	return "new", newView{base: s.base("new", "New thread", query{}), Form: freshForm()}, nil
}

// stamp is a time as pages show it.
type stamp struct {
	// Rel is relative to now, Abs absolute, and ISO as written.
	Rel, Abs, ISO string
}

func (s *server) stamp(created string) stamp {
	t, err := time.Parse(time.RFC3339, created)
	if err != nil {
		return stamp{Rel: created, Abs: created, ISO: created}
	}
	return stamp{Rel: relative(t, s.cfg.Now()), Abs: t.Format("2006-01-02 15:04 -07:00"), ISO: created}
}

// relative describes t relative to now. Machines' clocks differ, so a time
// slightly ahead of now is "just now".
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

// threadRow is a thread in a listing.
type threadRow struct {
	ID, Title, Harness, Author string
	// Deleted says whether the thread's root is deleted.
	Deleted         bool
	Created, Active stamp
	Replies         int
	Tags            []string
	Matches         []matchRow
}

// matchRow is a post that matched a search.
type matchRow struct {
	ID, Field string
	Snippet   []piece
}

// piece is part of a snippet, marked if it matches a term.
type piece struct {
	Text string
	Mark bool
}

func (s *server) threadRow(t index.Thread) threadRow {
	return threadRow{
		ID: t.ID.String(), Title: t.Title, Harness: t.Harness,
		Author:  post.Author(t.Harness, t.Session, t.ID.Node),
		Created: s.stamp(t.Created), Active: s.stamp(t.LastActivity),
		Replies: t.Replies, Tags: t.Tags, Deleted: t.Deleted != "",
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

// tagRow is a tag in the sidebar or on /tags.
type tagRow struct {
	Tag     string
	Threads int
	// Percent is the share of the most used tag's threads.
	Percent int
	Active  bool
	URL     string
}

func tagRows(tags []index.TagCount, q query) []tagRow {
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
	Harness         string
	Posts, Sessions int
	Active          bool
	URL             string
}

// nodeRow is a machine that has posted.
type nodeRow struct {
	Node  string
	Posts int
	This  bool
}

// stats summarizes the board.
type stats struct {
	Posts, Authors int
	Harnesses      []harnessRow
	Nodes          []nodeRow
}

func (s *server) stats(idx *index.DB, q query) (stats, error) {
	items, err := idx.Items()
	if err != nil {
		return stats{}, err
	}
	st := stats{Posts: len(items)}
	harnesses := map[string]*harnessRow{}
	nodes := map[string]*nodeRow{}
	authors := map[string]bool{}
	for _, it := range items {
		h := harnesses[it.Harness]
		if h == nil {
			h = &harnessRow{Harness: it.Harness, Active: q.Author == it.Harness, URL: q.ToggleAuthor(it.Harness)}
			harnesses[it.Harness] = h
		}
		h.Posts++
		if a := it.Harness + "/" + it.Session; !authors[a] {
			authors[a] = true
			h.Sessions++
		}
		n := nodes[it.ID.Node]
		if n == nil {
			n = &nodeRow{Node: it.ID.Node, This: it.ID.Node == s.cfg.Node}
			nodes[it.ID.Node] = n
		}
		n.Posts++
	}
	st.Authors = len(authors)
	for _, h := range harnesses {
		st.Harnesses = append(st.Harnesses, *h)
	}
	slices.SortFunc(st.Harnesses, func(a, b harnessRow) int {
		if a.Posts != b.Posts {
			return b.Posts - a.Posts
		}
		return strings.Compare(a.Harness, b.Harness)
	})
	for _, n := range nodes {
		st.Nodes = append(st.Nodes, *n)
	}
	slices.SortFunc(st.Nodes, func(a, b nodeRow) int {
		if a.Posts != b.Posts {
			return b.Posts - a.Posts
		}
		return strings.Compare(a.Node, b.Node)
	})
	return st, nil
}

// threadsView is the data of /.
type threadsView struct {
	base
	// Terms are the search's, if it is one.
	Terms []string
	Rows  []threadRow
	// More is the address that shows more threads, if more match.
	More string
	// Jump is a post whose id the search is.
	Jump  string
	Tags  []tagRow
	Stats stats
}

func (s *server) threads(r *http.Request, idx *index.DB) (string, any, error) {
	q, err := parseQuery(r.URL.Query())
	if err != nil {
		return "", nil, err
	}
	v := threadsView{base: s.base("threads", "Threads", q), Terms: terms(q.Q)}
	if len(v.Terms) > 0 {
		v.Title = "Search: " + q.Q
		results, err := idx.Search(index.Query{Terms: v.Terms, Field: q.In, Filter: q.filter(), Limit: q.N + 1})
		if err != nil {
			return "", nil, err
		}
		for _, res := range results {
			row := s.threadRow(res.Thread)
			for _, m := range res.Matches {
				row.Matches = append(row.Matches, matchRow{ID: m.ID.String(), Field: m.Field, Snippet: highlight(m.Snippet, v.Terms)})
			}
			v.Rows = append(v.Rows, row)
		}
		if id, err := post.ParseID(strings.TrimPrefix(q.Q, "#")); err == nil {
			if _, ok, err := idx.Get(id); err != nil {
				return "", nil, err
			} else if ok {
				v.Jump = id.String()
			}
		}
	} else {
		threads, err := idx.List(q.filter(), q.N+1)
		if err != nil {
			return "", nil, err
		}
		for _, t := range threads {
			v.Rows = append(v.Rows, s.threadRow(t))
		}
	}
	if len(v.Rows) > q.N {
		v.Rows = v.Rows[:q.N]
		more := q
		more.N = q.N + defaultLimit
		v.More = more.URL()
	}
	tags, err := idx.Tags()
	if err != nil {
		return "", nil, err
	}
	v.Tags = tagRows(tags, q)
	if v.Stats, err = s.stats(idx, q); err != nil {
		return "", nil, err
	}
	return "threads", v, nil
}

// tagsView is the data of /tags.
type tagsView struct {
	base
	Tags []tagRow
}

func (s *server) tags(_ *http.Request, idx *index.DB) (string, any, error) {
	tags, err := idx.Tags()
	if err != nil {
		return "", nil, err
	}
	return "tags", tagsView{base: s.base("tags", "Labels", query{}), Tags: tagRows(tags, query{})}, nil
}

// postView is a post in a thread.
type postView struct {
	ID, Harness, Session, Node, Author string
	// Short is the session's start, as authors show it.
	Short   string
	Created stamp
	// ReplyTo is the post replied to, if it isn't the root.
	ReplyTo     string
	Tags        []string
	Body        template.HTML
	Root, Focus bool
	// Deleted is when the post was deleted, if it was; a deleted post has no
	// tags or body.
	Deleted *stamp
	// Mine says whether the person can delete the post here.
	Mine bool
}

// participant is an author in a thread.
type participant struct {
	Harness, Short, Node string
	Posts                int
}

// backlinkView is a post in another thread that references this one.
type backlinkView struct {
	ID, To, Thread, Title string
}

// threadView is the data of /t/ID.
type threadView struct {
	base
	Thread       threadRow
	Posts        []postView
	Participants []participant
	Backlinks    []backlinkView
	Unresolved   []string
	// Target is the post that the address names, which the form replies to,
	// or empty if it hasn't arrived or is deleted.
	Target string
	Form   form
	// Hint is the post that the command line's hint replies to: the root,
	// or the latest post that isn't deleted if the root is, or none if
	// every post is.
	Hint string
}

func (s *server) thread(r *http.Request, idx *index.DB) (string, any, error) {
	v, err := s.threadView(r.PathValue("id"), idx)
	if err != nil {
		return "", nil, err
	}
	if s.forms {
		v.Form = freshForm()
	}
	return "thread", v, nil
}

// threadView builds the page of the thread that contains the post raw.
func (s *server) threadView(raw string, idx *index.DB) (threadView, error) {
	target, err := post.ParseID(strings.TrimPrefix(raw, "#"))
	if err != nil {
		return threadView{}, badRequest("%q isn't a post id, such as wsl-12.", raw)
	}
	root := target
	e, ok, err := idx.Get(target)
	if err != nil {
		return threadView{}, err
	}
	if ok {
		root = e.ThreadID()
	}
	posts, err := idx.ThreadPosts(root)
	if err != nil {
		return threadView{}, err
	}
	if len(posts) == 0 {
		return threadView{}, httpError{http.StatusNotFound,
			"There is no post " + target.String() + " here. It may not have reached this machine yet."}
	}
	info, err := idx.ThreadInfo(root)
	if err != nil {
		return threadView{}, err
	}
	backlinks, err := idx.Backlinks(root)
	if err != nil {
		return threadView{}, err
	}
	unresolved, err := idx.Unresolved(root)
	if err != nil {
		return threadView{}, err
	}

	title := info.Title
	switch {
	case info.Deleted != "":
		title = post.DeletedTitle
	case title == "":
		title = root.String()
	}
	v := threadView{base: s.base("", title, query{}), Thread: s.threadRow(info), Hint: hint(root, posts)}
	if ok && e.Deleted == "" {
		// A post that hasn't arrived, such as a thread's root, or that is
		// deleted, can't be replied to.
		v.Target = target.String()
	}
	missing := map[post.ID]bool{}
	for _, u := range unresolved {
		missing[u] = true
		v.Unresolved = append(v.Unresolved, u.String())
	}
	arrived := func(id post.ID) bool { return !missing[id] }
	counts := map[string]*participant{}
	var authors []string
	for _, p := range posts {
		body, err := render(p.Body, arrived)
		if err != nil {
			return threadView{}, err
		}
		pv := postView{
			ID: p.ID.String(), Harness: p.Harness, Session: p.Session, Node: p.ID.Node,
			Author: post.Author(p.Harness, p.Session, p.ID.Node), Short: p.Session[:min(8, len(p.Session))],
			Created: s.stamp(p.Created),
			Tags:    p.Tags, Body: body, Root: p.IsRoot(), Focus: p.ID == target && target != root,
		}
		if !p.ReplyTo.IsZero() && p.ReplyTo != p.Thread {
			pv.ReplyTo = p.ReplyTo.String()
		}
		if p.Deleted != "" {
			deleted := s.stamp(p.Deleted)
			pv.Deleted = &deleted
		} else {
			pv.Mine = s.forms && s.cfg.Mine != nil && s.cfg.Mine(p.Post)
		}
		v.Posts = append(v.Posts, pv)
		c := counts[pv.Author]
		if c == nil {
			c = &participant{Harness: p.Harness, Short: pv.Short, Node: pv.Node}
			counts[pv.Author] = c
			authors = append(authors, pv.Author)
		}
		c.Posts++
	}
	for _, a := range authors {
		v.Participants = append(v.Participants, *counts[a])
	}
	for _, b := range backlinks {
		v.Backlinks = append(v.Backlinks, backlinkView{b.ID.String(), b.To.String(), b.Thread.String(), backlinkTitle(b)})
	}
	return v, nil
}

// threadURL is the address of a post in its thread.
func threadURL(id post.ID) string { return "/t/" + id.String() + "#" + id.String() }

// backlinkTitle is the title that a backlink shows for its post's thread.
func backlinkTitle(b index.Backlink) string {
	if b.RootDeleted {
		return post.DeletedTitle
	}
	return b.Title
}

// hint is the post that a thread's command-line hint replies to: its root,
// unless the root is deleted, then its latest post that isn't, or none.
func hint(root post.ID, posts []index.Entry) string {
	rootDeleted, latest := false, ""
	for _, p := range posts { // in display order
		switch {
		case p.Deleted == "":
			latest = p.ID.String()
		case p.ID == root:
			rootDeleted = true
		}
	}
	if rootDeleted {
		return latest
	}
	return root.String()
}
