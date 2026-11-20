package server

import (
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zlcwwhq/agora/internal/post"
)

//go:embed templates static
var files embed.FS

// security is the Content-Security-Policy of every page: pages run no
// scripts and load nothing from other origins.
const security = "default-src 'none'; style-src 'self'; img-src 'self' data:; form-action 'self'; " +
	"frame-ancestors 'none'; base-uri 'none'"

// pages serves the web interface of api/web.md.
type pages struct {
	s *server
	// forms says whether pages offer forms, which act as s.Person.
	forms  bool
	tmpl   map[string]*template.Template
	static http.Handler
}

func newPages(s *server) *pages {
	static, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	}
	return &pages{
		s:      s,
		forms:  !s.Person.IsZero(),
		tmpl:   parseTemplates(),
		static: http.StripPrefix("/static/", http.FileServerFS(static)),
	}
}

// parseTemplates parses each page's template with the layout.
func parseTemplates() map[string]*template.Template {
	layout := template.Must(template.New("").Funcs(funcs).ParseFS(files, "templates/layout.html"))
	out := map[string]*template.Template{}
	for _, name := range []string{"threads", "thread", "tags", "compose", "error"} {
		t := template.Must(layout.Clone())
		out[name] = template.Must(t.ParseFS(files, "templates/"+name+".html"))
	}
	return out
}

// handlers maps methods to the handlers of one address. HEAD is GET's.
type handlers map[string]http.HandlerFunc

func (p *pages) route(mux *http.ServeMux) {
	mux.HandleFunc("/{$}", p.handle(handlers{http.MethodGet: p.threads}))
	mux.HandleFunc("/tags", p.handle(handlers{http.MethodGet: p.tags}))
	thread := handlers{http.MethodGet: p.thread}
	if p.forms {
		thread[http.MethodPost] = p.reply
		mux.HandleFunc("/new", p.handle(handlers{http.MethodGet: p.newForm, http.MethodPost: p.newThread}))
		mux.HandleFunc("/t/{id}/edit", p.handle(handlers{http.MethodGet: p.editForm, http.MethodPost: p.edit}))
		mux.HandleFunc("/t/{id}/delete", p.handle(handlers{http.MethodPost: p.remove}))
	}
	mux.HandleFunc("/t/{id}", p.handle(thread))
	mux.HandleFunc("/static/", p.handle(handlers{http.MethodGet: p.file}))
	mux.HandleFunc("/", p.handle(handlers{}))
}

// handle returns the handler of an address: it sets every page's headers,
// answers other methods with 405 and addresses without handlers with 404,
// and refuses POSTs from other origins.
func (p *pages) handle(hs handlers) http.HandlerFunc {
	var allow []string
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		if hs[m] != nil {
			allow = append(allow, m)
			if m == http.MethodGet {
				allow = append(allow, http.MethodHead)
			}
		}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", security)
		h.Set("X-Content-Type-Options", "nosniff")
		// With no-referrer, browsers would send forms with "Origin: null".
		h.Set("Referrer-Policy", "same-origin")
		if len(hs) == 0 {
			p.fail(w, r, http.StatusNotFound, "There is no such page.")
			return
		}
		method := r.Method
		if method == http.MethodHead {
			method = http.MethodGet
		}
		f := hs[method]
		if f == nil {
			h.Set("Allow", strings.Join(allow, ", "))
			msg := "This page only shows the board: agents post with the agora command."
			if hs[http.MethodPost] != nil {
				msg = "This address takes its form's POST."
			}
			p.fail(w, r, http.StatusMethodNotAllowed, msg)
			return
		}
		if r.Method == http.MethodPost && !strings.EqualFold(r.Header.Get("Origin"), "http://"+r.Host) {
			p.fail(w, r, http.StatusForbidden, "This form didn't come from these pages: load the page again and send it from there.")
			return
		}
		f(w, r)
	}
}

func (p *pages) file(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(r.URL.Path, "/") {
		p.fail(w, r, http.StatusNotFound, "There is no such page.")
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	p.static.ServeHTTP(w, r)
}

// base is what every page's layout shows.
type base struct {
	// Nav is the current section: threads, tags, new, or empty.
	Nav, Title string
	// Query fills the search form.
	Query query
	// Forms says whether pages offer forms, which act as Author.
	Forms   bool
	Author  string
	Version string
}

func (p *pages) base(nav, title string, q query) base {
	b := base{Nav: nav, Title: title, Query: q, Forms: p.forms, Version: p.s.Version}
	if p.forms {
		b.Author = p.s.Person.Author()
	}
	return b
}

// form is what a form holds: what was typed, why it was refused, and the
// nonce that keeps it from posting twice.
type form struct {
	Title, Tags, Body, Reason, Nonce string
	// HasTitle says whether the form has a title field.
	HasTitle bool
}

// newNonce returns a random nonce for a form.
func newNonce() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

// readForm reads what a form sent, answering the request if it can't.
func (p *pages) readForm(w http.ResponseWriter, r *http.Request) (form, bool) {
	if err := r.ParseForm(); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			p.fail(w, r, http.StatusRequestEntityTooLarge, "This is larger than 1 MiB, which is more than a form may send.")
		} else {
			p.fail(w, r, http.StatusBadRequest, "Can't read the form: "+err.Error())
		}
		return form{}, false
	}
	f := r.PostForm
	_, hasTitle := f["title"]
	return form{Title: f.Get("title"), Tags: f.Get("tags"), Body: f.Get("body"), Nonce: f.Get("nonce"), HasTitle: hasTitle}, true
}

// pathID returns the post that the address names, answering the request if
// it isn't an id.
func (p *pages) pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := r.PathValue("id")
	id, err := post.ParseID(raw)
	if err != nil {
		p.fail(w, r, http.StatusBadRequest, fmt.Sprintf("%q isn't a post id, such as 12.", raw))
		return 0, false
	}
	return id, true
}

// postURL is the address of a post in its thread.
func postURL(id int64) string { return fmt.Sprintf("/t/%d#%d", id, id) }

// inThread is the address of a post within the page of its thread.
func inThread(thread, id int64) string { return fmt.Sprintf("/t/%d#%d", thread, id) }

func (p *pages) threads(w http.ResponseWriter, r *http.Request) {
	q, bad := parseQuery(r.URL.Query())
	if bad != "" {
		p.fail(w, r, http.StatusBadRequest, bad)
		return
	}
	v, err := p.threadsView(r, q)
	if err != nil {
		p.boardFailed(w, r, err, form{})
		return
	}
	p.render(w, r, http.StatusOK, "threads", v)
}

func (p *pages) tags(w http.ResponseWriter, r *http.Request) {
	tags, err := p.s.Board.Tags(r.Context())
	if err != nil {
		p.boardFailed(w, r, err, form{})
		return
	}
	p.render(w, r, http.StatusOK, "tags", tagsView{base: p.base("tags", "Labels", query{}), Tags: tagRows(tags, query{})})
}

func (p *pages) thread(w http.ResponseWriter, r *http.Request) {
	id, ok := p.pathID(w, r)
	if !ok {
		return
	}
	f := form{}
	if p.forms {
		f.Nonce = newNonce()
	}
	p.showThread(w, r, http.StatusOK, id, f)
}

// showThread shows the thread that contains id, with the reply form f.
func (p *pages) showThread(w http.ResponseWriter, r *http.Request, status int, id int64, f form) {
	show, err := p.s.Board.Show(r.Context(), id)
	if err != nil {
		p.boardFailed(w, r, err, f)
		return
	}
	v, err := p.threadView(show)
	if err != nil {
		p.boardFailed(w, r, err, f)
		return
	}
	v.Form = f
	p.render(w, r, status, "thread", v)
}

func (p *pages) reply(w http.ResponseWriter, r *http.Request) {
	id, ok := p.pathID(w, r)
	if !ok {
		return
	}
	f, ok := p.readForm(w, r)
	if !ok {
		return
	}
	pub, _, err := p.s.Board.Reply(r.Context(), p.s.Person, id, post.NewReply{Tags: post.SplitTags(f.Tags), Body: f.Body, Nonce: f.Nonce})
	if post.KindOf(err) == post.Invalid {
		f.Reason = sentence(err.Error())
		p.showThread(w, r, http.StatusBadRequest, id, f)
		return
	}
	if err != nil {
		p.boardFailed(w, r, err, f)
		return
	}
	http.Redirect(w, r, inThread(pub.Thread, pub.ID), http.StatusSeeOther)
}

func (p *pages) newForm(w http.ResponseWriter, r *http.Request) {
	p.render(w, r, http.StatusOK, "compose", composeView{
		base: p.base("new", "New thread", query{}),
		Form: form{Nonce: newNonce(), HasTitle: true},
	})
}

func (p *pages) newThread(w http.ResponseWriter, r *http.Request) {
	f, ok := p.readForm(w, r)
	if !ok {
		return
	}
	f.HasTitle = true
	pub, _, err := p.s.Board.Post(r.Context(), p.s.Person, post.NewThread{
		Title: f.Title, Tags: post.SplitTags(f.Tags), Body: f.Body, Nonce: f.Nonce,
	})
	if post.KindOf(err) == post.Invalid {
		f.Reason = sentence(err.Error())
		p.render(w, r, http.StatusBadRequest, "compose", composeView{base: p.base("new", "New thread", query{}), Form: f})
		return
	}
	if err != nil {
		p.boardFailed(w, r, err, f)
		return
	}
	http.Redirect(w, r, inThread(pub.Thread, pub.ID), http.StatusSeeOther)
}

func (p *pages) editForm(w http.ResponseWriter, r *http.Request) {
	id, ok := p.pathID(w, r)
	if !ok {
		return
	}
	cur, err := p.s.Board.Get(r.Context(), id)
	if err != nil {
		p.boardFailed(w, r, err, form{})
		return
	}
	f := form{Title: cur.Title, Tags: strings.Join(cur.Tags, " "), Body: cur.Body, HasTitle: cur.IsRoot()}
	p.showEdit(w, r, http.StatusOK, cur, f)
}

// showEdit shows the form that edits cur, holding f.
func (p *pages) showEdit(w http.ResponseWriter, r *http.Request, status int, cur post.Post, f form) {
	v := composeView{base: p.base("", fmt.Sprintf("Edit #%d", cur.ID), query{}), Form: f, Editing: &editing{
		ID: cur.ID, Thread: cur.Thread, Root: cur.IsRoot(), Author: cur.Author, Harness: cur.Harness,
	}}
	if !cur.IsRoot() {
		if show, err := p.s.Board.Show(r.Context(), cur.Thread); err == nil {
			v.Editing.ThreadTitle = show.Thread.Title
		}
	}
	p.render(w, r, status, "compose", v)
}

func (p *pages) edit(w http.ResponseWriter, r *http.Request) {
	id, ok := p.pathID(w, r)
	if !ok {
		return
	}
	f, ok := p.readForm(w, r)
	if !ok {
		return
	}
	var e post.Edit
	if f.HasTitle {
		e.Title = &f.Title
	}
	if _, ok := r.PostForm["tags"]; ok {
		tags := post.SplitTags(f.Tags)
		if tags == nil {
			tags = []string{}
		}
		e.Tags = &tags
	}
	if _, ok := r.PostForm["body"]; ok {
		e.Body = &f.Body
	}
	edited, err := p.s.Board.Edit(r.Context(), p.s.Person, id, e)
	if post.KindOf(err) == post.Invalid {
		cur, gerr := p.s.Board.Get(r.Context(), id)
		if gerr != nil {
			p.boardFailed(w, r, gerr, f)
			return
		}
		f.Reason = sentence(err.Error())
		f.HasTitle = cur.IsRoot()
		p.showEdit(w, r, http.StatusBadRequest, cur, f)
		return
	}
	if err != nil {
		p.boardFailed(w, r, err, f)
		return
	}
	http.Redirect(w, r, inThread(edited.Thread, edited.ID), http.StatusSeeOther)
}

func (p *pages) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := p.pathID(w, r)
	if !ok {
		return
	}
	if _, ok := p.readForm(w, r); !ok {
		return
	}
	removed, err := p.s.Board.Delete(r.Context(), p.s.Person, id)
	if err != nil {
		p.boardFailed(w, r, err, form{})
		return
	}
	if removed.ID == removed.Thread {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, inThread(removed.Thread, removed.Thread), http.StatusSeeOther)
}

// errorView is the data of the error page.
type errorView struct {
	base
	Status int
	Text   string
	// Typed is what a refused form held, shown so that it isn't lost.
	Typed form
}

// fail shows the error page.
func (p *pages) fail(w http.ResponseWriter, r *http.Request, status int, msg string) {
	p.failTyped(w, r, status, msg, form{})
}

func (p *pages) failTyped(w http.ResponseWriter, r *http.Request, status int, msg string, typed form) {
	p.render(w, r, status, "error", errorView{
		base:   p.base("", http.StatusText(status), query{}),
		Status: status, Text: msg, Typed: typed,
	})
}

// boardFailed shows an error of the board, with what a form held.
func (p *pages) boardFailed(w http.ResponseWriter, r *http.Request, err error, typed form) {
	switch post.KindOf(err) {
	case post.NotFound:
		p.failTyped(w, r, http.StatusNotFound, sentence(err.Error()), typed)
	case post.Invalid:
		p.failTyped(w, r, http.StatusBadRequest, sentence(err.Error()), typed)
	case post.Forbidden:
		p.failTyped(w, r, http.StatusForbidden, sentence(err.Error()), typed)
	default:
		p.s.Logf("agora serve %s %s: %v", r.Method, r.URL.Path, err)
		p.failTyped(w, r, http.StatusInternalServerError, "The board failed: "+err.Error(), typed)
	}
}

// sentence makes an error's message, which starts in lower case as Go's
// do, a sentence for a page.
func sentence(msg string) string {
	if msg == "" {
		return msg
	}
	r, n := utf8.DecodeRuneInString(msg)
	msg = string(unicode.ToUpper(r)) + msg[n:]
	if !strings.HasSuffix(msg, ".") && !strings.HasSuffix(msg, "?") && !strings.HasSuffix(msg, "!") {
		msg += "."
	}
	return msg
}

func (p *pages) render(w http.ResponseWriter, _ *http.Request, status int, name string, data any) {
	var buf bytes.Buffer
	if err := p.tmpl[name].ExecuteTemplate(&buf, "layout", data); err != nil {
		p.s.Logf("agora serve: rendering %s: %v", name, err)
		http.Error(w, "can't render this page", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}
