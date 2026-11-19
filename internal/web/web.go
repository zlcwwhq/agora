// Package web serves a web interface to a board, which api/web.md specifies:
// its threads with their tags as labels, searches, and whole threads, and
// forms that post as a person. It reads the board only through the index
// that its caller brings up to date for every page, and posts only through
// the function that its caller passes it.
package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/zlcwwhq/agora/internal/index"
	"github.com/zlcwwhq/agora/internal/post"
)

//go:embed templates static
var files embed.FS

// Config is what the interface needs of its board.
type Config struct {
	// Open returns the board's index, brought up to date with the board's
	// files. It is called for every page, which closes the index after use.
	Open func() (*index.DB, error)
	// Board is the board's directory, and Node this machine's node name,
	// empty if it has none.
	Board, Node string
	// Now is the current time, which pages show other times relative to.
	Now func() time.Time
	// Logf reports errors that keep pages from being shown.
	Logf func(format string, args ...any)
	// Post publishes a draft as Author, as agora post or agora reply does,
	// and returns the new post's id; a *FormError says why it refuses one.
	// Pages offer forms only if it isn't nil and ln is a loopback address.
	Post func(Draft) (post.ID, error)
	// Author is whom Post posts as, as post.Author shows authors.
	Author string
}

// Draft is what a form sends: a new thread if ReplyTo is zero, else a reply
// to ReplyTo.
type Draft struct {
	ReplyTo     post.ID
	Title, Body string
	Tags        []string
}

// FormError is why Config.Post refuses a draft: status 400 for a post that
// the command line rejects, which the form shows again with Reason, or
// another status, such as 404 for a reply to a post that isn't here.
type FormError struct {
	Status int
	Reason string
}

func (e *FormError) Error() string { return e.Reason }

// Refused is a draft that the command line rejects, for reason, which the
// form shows again.
func Refused(reason string) error { return &FormError{Status: http.StatusBadRequest, Reason: reason} }

// Missing is a reply to a post that isn't here, for reason.
func Missing(reason string) error { return &FormError{Status: http.StatusNotFound, Reason: reason} }

// Serve serves the interface on ln until ctx is done, then shuts down and
// returns nil.
func Serve(ctx context.Context, ln net.Listener, cfg Config) error {
	srv := &http.Server{
		Handler:           newHandler(cfg, IsLoopback(ln.Addr())),
		ReadHeaderTimeout: 10 * time.Second,
	}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := srv.Shutdown(shutdown)
	if err != nil {
		_ = srv.Close()
	}
	if serveErr := <-served; !errors.Is(serveErr, http.ErrServerClosed) {
		return serveErr
	}
	return err
}

// IsLoopback reports whether addr is a loopback address, the only kind on
// which pages offer forms.
func IsLoopback(addr net.Addr) bool { return isLoopback(addr) }

// isLoopback reports whether addr is a loopback address.
func isLoopback(addr net.Addr) bool {
	tcp, ok := addr.(*net.TCPAddr)
	return ok && tcp.IP.IsLoopback()
}

// localHost reports whether a request's Host names this machine: localhost or
// a loopback address, with any port.
func localHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// server is the interface's handler.
type server struct {
	cfg Config
	// local restricts requests to those whose Host is localHost.
	local bool
	// forms says whether pages offer forms, which must carry token.
	forms bool
	token string
	mux   *http.ServeMux
	pages map[string]*template.Template
	// mu serializes pages, which bring the same index up to date, and posts.
	mu sync.Mutex
}

// maxForm is the largest request body that a form may send.
const maxForm = 1 << 20

// newToken returns a random value for forms to carry, which other web sites
// can't know.
func newToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}

// security is the Content-Security-Policy of every response: pages run no
// scripts and load nothing from other origins.
const security = "default-src 'none'; style-src 'self'; img-src 'self'; form-action 'self'; " +
	"frame-ancestors 'none'; base-uri 'none'"

func newHandler(cfg Config, local bool) *server {
	s := &server{cfg: cfg, local: local, forms: cfg.Post != nil && local, pages: parsePages()}
	static, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	}
	assets := http.StripPrefix("/static/", http.FileServerFS(static))
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("/{$}", s.page(s.threads))
	s.mux.HandleFunc("/tags", s.page(s.tags))
	s.mux.HandleFunc("/t/{id}", s.page(s.thread))
	if s.forms {
		s.token = newToken()
		s.mux.HandleFunc("GET /new", s.page(s.newThread))
		s.mux.HandleFunc("POST /new", s.submit)
		s.mux.HandleFunc("POST /t/{id}", s.submit)
	}
	s.mux.HandleFunc("/static/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/") {
			s.fail(w, http.StatusNotFound, "There is no such page.")
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		assets.ServeHTTP(w, r)
	})
	s.mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		s.fail(w, http.StatusNotFound, "There is no such page.")
	})
	return s
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Security-Policy", security)
	h.Set("X-Content-Type-Options", "nosniff")
	// Other origins get no referrer. With no-referrer, browsers would send
	// forms' POSTs with "Origin: null", which genuine refuses.
	h.Set("Referrer-Policy", "same-origin")
	if s.local && !localHost(r.Host) {
		s.fail(w, http.StatusForbidden, "This board is served to this machine only: open it at localhost or 127.0.0.1.")
		return
	}
	form := s.forms && formPath(r.URL.Path)
	if r.Method != http.MethodGet && r.Method != http.MethodHead && (r.Method != http.MethodPost || !form) {
		if form {
			h.Set("Allow", "GET, HEAD, POST")
			s.fail(w, http.StatusMethodNotAllowed, "This page answers GET and HEAD, and its form POST.")
			return
		}
		h.Set("Allow", "GET, HEAD")
		s.fail(w, http.StatusMethodNotAllowed, "Pages only read the board. Post and reply with the agora command.")
		return
	}
	s.mux.ServeHTTP(w, r)
}

// formPath reports whether a path is a form's address: /new or /t/ID.
func formPath(path string) bool {
	rest, ok := strings.CutPrefix(path, "/t/")
	return path == "/new" || ok && rest != "" && !strings.Contains(rest, "/")
}

// submit publishes what a form sent, and leads to the new post, or shows the
// form again with what was typed and why the post was refused.
func (s *server) submit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxForm)
	if err := r.ParseForm(); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			s.fail(w, http.StatusRequestEntityTooLarge, "This post is larger than 1 MiB, which is more than a form may send.")
			return
		}
		s.fail(w, http.StatusBadRequest, "Can't read the form: "+err.Error())
		return
	}
	if !s.genuine(r) {
		s.fail(w, http.StatusForbidden, "This form didn't come from these pages, or agora serve has restarted since it was loaded: "+
			"load the page again and post from it.")
		return
	}
	f := form{Title: r.PostForm.Get("title"), Tags: r.PostForm.Get("tags"), Body: r.PostForm.Get("body")}
	d := Draft{Title: f.Title, Body: f.Body, Tags: splitTags(f.Tags)}
	raw := r.PathValue("id")
	if raw != "" {
		target, err := post.ParseID(strings.TrimPrefix(raw, "#"))
		if err != nil {
			s.fail(w, http.StatusBadRequest, fmt.Sprintf("%q isn't a post id, such as wsl-12.", raw))
			return
		}
		d.ReplyTo = target
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := s.cfg.Post(d)
	var refused *FormError
	switch {
	case errors.As(err, &refused) && refused.Status == http.StatusBadRequest:
		f.Reason = refused.Reason
		s.refill(w, raw, f)
	case errors.As(err, &refused):
		s.fail(w, refused.Status, refused.Reason)
	case err != nil:
		s.cfg.Logf("serve: posting: %v", err)
		s.fail(w, http.StatusInternalServerError, "Can't post: "+err.Error())
	default:
		http.Redirect(w, r, threadURL(id), http.StatusSeeOther)
	}
}

// genuine reports whether a form's request came from these pages: it
// carries their token and, if it names its origin, names this one.
func (s *server) genuine(r *http.Request) bool {
	if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("token")), []byte(s.token)) != 1 {
		return false
	}
	origin := r.Header.Get("Origin")
	return origin == "" || strings.EqualFold(origin, "http://"+r.Host)
}

// refill shows a refused form again with status 400: the new thread's form,
// or the thread of the post replied to, raw, with the reply's form.
func (s *server) refill(w http.ResponseWriter, raw string, f form) {
	if raw == "" {
		s.render(w, http.StatusBadRequest, "new", newView{base: s.base("new", "New thread", query{}), Form: f})
		return
	}
	idx, err := s.cfg.Open()
	if err != nil {
		s.cfg.Logf("serve: %v", err)
		s.fail(w, http.StatusInternalServerError, "Can't read the board: "+err.Error())
		return
	}
	defer func() { _ = idx.Close() }()
	v, err := s.threadView(raw, idx)
	var he httpError
	switch {
	case errors.As(err, &he):
		s.fail(w, he.status, he.msg)
	case err != nil:
		s.cfg.Logf("serve: %v", err)
		s.fail(w, http.StatusInternalServerError, "Can't show this page: "+err.Error())
	default:
		v.Form = f
		s.render(w, http.StatusBadRequest, "thread", v)
	}
}

// splitTags splits a form's tags at whitespace and commas, which tags can't
// contain.
func splitTags(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == ',' })
}

// httpError is an error that a page shows with its status.
type httpError struct {
	status int
	msg    string
}

func (e httpError) Error() string { return e.msg }

func badRequest(format string, args ...any) error {
	return httpError{http.StatusBadRequest, fmt.Sprintf(format, args...)}
}

// pageFunc builds a page from the index, returning its template and data.
type pageFunc func(r *http.Request, idx *index.DB) (string, any, error)

func (s *server) page(f pageFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		idx, err := s.cfg.Open()
		if err != nil {
			s.cfg.Logf("serve: %v", err)
			s.fail(w, http.StatusInternalServerError, "Can't read the board: "+err.Error())
			return
		}
		defer func() { _ = idx.Close() }()
		name, data, err := f(r, idx)
		var he httpError
		switch {
		case errors.As(err, &he):
			s.fail(w, he.status, he.msg)
		case err != nil:
			s.cfg.Logf("serve %s: %v", r.URL.Path, err)
			s.fail(w, http.StatusInternalServerError, "Can't show this page: "+err.Error())
		default:
			s.render(w, http.StatusOK, name, data)
		}
	}
}

// errorView is the data of the error page.
type errorView struct {
	base
	Status int
	Text   string
}

func (s *server) fail(w http.ResponseWriter, status int, msg string) {
	s.render(w, status, "error", errorView{
		base:   s.base("", http.StatusText(status), query{}),
		Status: status, Text: msg,
	})
}

func (s *server) render(w http.ResponseWriter, status int, name string, data any) {
	var buf bytes.Buffer
	if err := s.pages[name].ExecuteTemplate(&buf, "layout", data); err != nil {
		s.cfg.Logf("serve: rendering %s: %v", name, err)
		http.Error(w, "can't render this page", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// parsePages parses each page's template with the layout.
func parsePages() map[string]*template.Template {
	layout := template.Must(template.New("").Funcs(funcs).ParseFS(files, "templates/layout.html"))
	pages := map[string]*template.Template{}
	for _, name := range []string{"threads", "thread", "tags", "error", "new"} {
		t := template.Must(layout.Clone())
		pages[name] = template.Must(t.ParseFS(files, "templates/"+name+".html"))
	}
	return pages
}
