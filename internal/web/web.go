// Package web serves a read-only web interface to a board, which api/web.md
// specifies: its threads with their tags as labels, searches, and whole
// threads. It reads the board only through the index that its caller brings
// up to date for every page.
package web

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zlcwwhq/agora/internal/index"
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
}

// Serve serves the interface on ln until ctx is done, then shuts down and
// returns nil.
func Serve(ctx context.Context, ln net.Listener, cfg Config) error {
	srv := &http.Server{
		Handler:           newHandler(cfg, isLoopback(ln.Addr())),
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
	mux   *http.ServeMux
	pages map[string]*template.Template
	// mu serializes pages, which bring the same index up to date.
	mu sync.Mutex
}

// security is the Content-Security-Policy of every response: pages run no
// scripts and load nothing from other origins.
const security = "default-src 'none'; style-src 'self'; img-src 'self'; form-action 'self'; " +
	"frame-ancestors 'none'; base-uri 'none'"

func newHandler(cfg Config, local bool) *server {
	s := &server{cfg: cfg, local: local, pages: parsePages()}
	static, err := fs.Sub(files, "static")
	if err != nil {
		panic(err)
	}
	assets := http.StripPrefix("/static/", http.FileServerFS(static))
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("/{$}", s.page(s.threads))
	s.mux.HandleFunc("/tags", s.page(s.tags))
	s.mux.HandleFunc("/t/{id}", s.page(s.thread))
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
	h.Set("Referrer-Policy", "no-referrer")
	if s.local && !localHost(r.Host) {
		s.fail(w, http.StatusForbidden, "This board is served to this machine only: open it at localhost or 127.0.0.1.")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		h.Set("Allow", "GET, HEAD")
		s.fail(w, http.StatusMethodNotAllowed, "Pages only read the board. Post and reply with the agora command.")
		return
	}
	s.mux.ServeHTTP(w, r)
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
	for _, name := range []string{"threads", "thread", "tags", "error"} {
		t := template.Must(layout.Clone())
		pages[name] = template.Must(t.ParseFS(files, "templates/"+name+".html"))
	}
	return pages
}
