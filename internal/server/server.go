// Package server serves a server's boards: the HTTP API of api/http.md,
// which the command line uses, and the web pages of api/web.md. It reaches
// the boards only through Store, which the command line fills with the
// store.
package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/zlcwwhq/agora/internal/post"
)

// Store is what the server needs of the boards' store.
type Store interface {
	Boards(ctx context.Context) ([]post.Board, error)
	Board(ctx context.Context, id string) (post.Board, error)
	CreateBoard(ctx context.Context, who post.Identity, req post.NewBoard) (post.Board, bool, error)
	Post(ctx context.Context, who post.Identity, board string, req post.NewThread) (post.Published, bool, error)
	Reply(ctx context.Context, who post.Identity, board string, to int64, req post.NewReply) (post.Published, bool, error)
	Edit(ctx context.Context, who post.Identity, board string, id int64, e post.Edit) (post.Post, error)
	Delete(ctx context.Context, who post.Identity, board string, id int64) (post.Removed, error)
	Get(ctx context.Context, board string, id int64) (post.Post, error)
	Show(ctx context.Context, board string, id int64) (post.Show, error)
	List(ctx context.Context, board string, f post.Filter) ([]post.Thread, error)
	Search(ctx context.Context, board string, q post.Query) ([]post.Result, error)
	Tags(ctx context.Context, board string) ([]post.TagCount, error)
	Harnesses(ctx context.Context, board string) ([]post.HarnessCount, error)
	Unread(ctx context.Context, who post.Identity, board string, req post.UnreadRequest) (post.Unread, error)
	History(ctx context.Context, board string, id int64) (post.History, error)
	React(ctx context.Context, who post.Identity, board string, id int64, req post.React) (post.Reacted, error)
	Vote(ctx context.Context, who post.Identity, board string, id int64, req post.Vote) (post.Voted, error)
	// Given returns what who gave each post of the thread that contains id,
	// leaving out the posts that it gave nothing.
	Given(ctx context.Context, who post.Identity, board string, id int64) (map[int64]post.Given, error)
	ThreadStore
	IconStore
	NameStore
}

// Options configure a server.
type Options struct {
	Store Store
	// Person is the identity that the pages' forms act as, a person's, or
	// zero for pages that only read.
	Person post.Identity
	// Version is agora's version, which the pages show.
	Version string
	// Now gives the time that pages show times relative to.
	Now func() time.Time
	// Logf reports failures of the server itself, such as the database's.
	Logf func(format string, args ...any)
}

// maxBody bounds request bodies.
const maxBody = 1 << 20

type server struct {
	Options
	pages *pages
}

// New returns the server's handler.
func New(o Options) http.Handler {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Logf == nil {
		o.Logf = func(string, ...any) {}
	}
	s := &server{Options: o}
	mux := http.NewServeMux()
	s.routeAPI(mux)
	s.pages = newPages(s)
	s.pages.route(mux)
	return s.guard(s.resolve(mux))
}

// Run serves the handler of o on ln until ctx is done, then shuts down,
// letting requests in progress finish.
func Run(ctx context.Context, ln net.Listener, o Options) error {
	// Requests that wait for posts answer at once when the server shuts
	// down, rather than keeping it up.
	waits, stopWaits := context.WithCancel(context.Background())
	defer stopWaits()
	srv := &http.Server{
		Handler:           New(o),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext: func(net.Listener) context.Context {
			return context.WithValue(context.Background(), waitsKey{}, waits)
		},
	}
	srv.RegisterOnShutdown(stopWaits)
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ln) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := srv.Shutdown(shutdown)
		if serr := <-done; !errors.Is(serr, http.ErrServerClosed) && err == nil {
			err = serr
		}
		return err
	}
}

// guard refuses requests whose Host isn't allowed, and bounds bodies.
func (s *server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !AllowedHost(r.Host) {
			msg := "agora answers only requests to localhost or an IP address, not " + r.Host +
				": forward a port, or use the server's IP address"
			if strings.HasPrefix(r.URL.Path, "/api/") {
				writeError(w, http.StatusForbidden, msg)
			} else {
				http.Error(w, msg, http.StatusForbidden)
			}
			return
		}
		r.Body = &limitedBody{w: w, body: r.Body, limit: maxBody}
		next.ServeHTTP(w, r)
	})
}

// AllowedHost reports whether a request's Host, with or without a port, is
// localhost, a name in .localhost, or an IP address. Other names could
// resolve to this machine from another web site, which DNS rebinding would
// let read and write the board.
func AllowedHost(host string) bool {
	h := host
	if hh, _, err := net.SplitHostPort(host); err == nil {
		h = hh
	}
	h = strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")
	if net.ParseIP(h) != nil {
		return true
	}
	h = strings.ToLower(strings.TrimSuffix(h, "."))
	return h == "localhost" || strings.HasSuffix(h, ".localhost")
}

// status returns the HTTP status for an error of the board.
func status(err error) int {
	var mbe *http.MaxBytesError
	switch {
	case errors.As(err, &mbe):
		return http.StatusRequestEntityTooLarge
	}
	switch post.KindOf(err) {
	case post.Invalid:
		return http.StatusBadRequest
	case post.Forbidden:
		return http.StatusForbidden
	case post.NotFound:
		return http.StatusNotFound
	case post.Conflict:
		return http.StatusConflict
	}
	return http.StatusInternalServerError
}
