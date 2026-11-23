package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/zlcwwhq/agora/internal/post"
)

// AuthorHeader carries the identity of API requests.
const AuthorHeader = "Agora-Author"

func (s *server) routeAPI(mux *http.ServeMux) {
	const b = "/api/boards/{board}"
	mux.HandleFunc("GET /api/boards", s.apiBoards)
	mux.HandleFunc("POST /api/boards", s.apiCreateBoard)
	mux.HandleFunc("GET "+b+"/threads", s.apiList)
	mux.HandleFunc("GET "+b+"/search", s.apiSearch)
	mux.HandleFunc("GET "+b+"/tags", s.apiTags)
	mux.HandleFunc("GET "+b+"/posts/{id}", s.apiShow)
	mux.HandleFunc("POST "+b+"/threads", s.apiPost)
	mux.HandleFunc("POST "+b+"/posts/{id}/replies", s.apiReply)
	mux.HandleFunc("PATCH "+b+"/posts/{id}", s.apiEdit)
	mux.HandleFunc("DELETE "+b+"/posts/{id}", s.apiDelete)
	mux.HandleFunc("POST "+b+"/unread", s.apiUnread)
	mux.HandleFunc("GET "+b+"/posts/{id}/history", s.apiHistory)
	mux.HandleFunc("POST "+b+"/posts/{id}/reactions", s.apiReact)
	s.routeThreadAPI(mux, b)
	s.routeIconAPI(mux, b)
	s.routeNameAPI(mux, b)
	// Known paths with other methods.
	for path, allow := range map[string]string{
		"/api/boards":             "GET, HEAD, POST",
		b + "/threads":            "GET, HEAD, POST",
		b + "/search":             "GET, HEAD",
		b + "/tags":               "GET, HEAD",
		b + "/posts/{id}":         "GET, HEAD, PATCH, DELETE",
		b + "/posts/{id}/replies": "POST",
		b + "/unread":             "POST",

		b + "/posts/{id}/history":   "GET, HEAD",
		b + "/posts/{id}/reactions": "POST",
	} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", allow)
			writeError(w, http.StatusMethodNotAllowed, r.Method+" isn't allowed here: use "+allow)
		})
	}
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "no such endpoint: "+r.URL.Path)
	})
}

type apiError struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apiError{Error: msg})
}

// fail answers an error of the board.
func (s *server) fail(w http.ResponseWriter, err error) {
	code := status(err)
	msg := err.Error()
	switch code {
	case http.StatusInternalServerError:
		s.Logf("agora: %v", err)
		msg = "the server failed: " + msg
	case http.StatusRequestEntityTooLarge:
		msg = tooLarge(err)
	}
	writeError(w, code, msg)
}

// identity returns the request's Agora-Author.
func identity(r *http.Request) (post.Identity, error) {
	h := r.Header.Get(AuthorHeader)
	if h == "" {
		return post.Identity{}, post.Errorf(post.Invalid, "the request needs an %s header, HARNESS/SESSION@NODE", AuthorHeader)
	}
	return post.ParseHeader(h)
}

// decode reads a JSON body into v. A body of another type gets 415, which
// browsers can't send across origins without asking first.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "the request's body must be application/json")
		return false
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, "the request is larger than 1 MiB")
		} else {
			writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		}
		return false
	}
	if _, err := dec.Token(); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid JSON: more than one value")
		return false
	}
	return true
}

// writer returns the identity of a request that writes, answering it if it
// has none.
func writer(w http.ResponseWriter, r *http.Request) (post.Identity, bool) {
	who, err := identity(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return who, false
	}
	return who, true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := post.ParseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return 0, false
	}
	return id, true
}

// pathBoard returns the board that the path names, answering the request if
// it isn't a board's id.
func pathBoard(w http.ResponseWriter, r *http.Request) (string, bool) {
	board := r.PathValue("board")
	if err := post.CheckBoard(board); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	return board, true
}

func (s *server) apiBoards(w http.ResponseWriter, r *http.Request) {
	bs, err := s.Store.Boards(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bs)
}

func (s *server) apiCreateBoard(w http.ResponseWriter, r *http.Request) {
	who, ok := writer(w, r)
	if !ok {
		return
	}
	var req post.NewBoard
	if !decode(w, r, &req) {
		return
	}
	b, created, err := s.Store.CreateBoard(r.Context(), who, req)
	if err != nil {
		s.fail(w, err)
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	writeJSON(w, status, b)
}

// filterParams reads the parameters that narrow lists of threads.
func filterParams(r *http.Request) (post.Filter, error) {
	q := r.URL.Query()
	f := post.Filter{Tags: q["t"], Author: q.Get("author"), State: q.Get("state"), Limit: post.DefaultLimit}
	if n := q.Get("n"); n != "" {
		v, err := strconv.Atoi(n)
		if err != nil || v < 1 {
			return f, post.Errorf(post.Invalid, "invalid n %q: want a number of at least 1", n)
		}
		f.Limit = v
	}
	if k := q.Get("skip"); k != "" {
		v, err := strconv.Atoi(k)
		if err != nil || v < 0 {
			return f, post.Errorf(post.Invalid, "invalid skip %q: want a number of at least 0", k)
		}
		f.Skip = v
	}
	if f.State != "" {
		if err := post.CheckState(f.State); err != nil {
			return f, err
		}
	}
	if _, err := post.Tags(f.Tags); err != nil {
		return f, err
	}
	if f.Author != "" {
		if _, _, err := post.AuthorFilter(f.Author); err != nil {
			return f, err
		}
	}
	return f, nil
}

func (s *server) apiList(w http.ResponseWriter, r *http.Request) {
	board, ok := pathBoard(w, r)
	if !ok {
		return
	}
	f, err := filterParams(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	ts, err := s.Store.List(r.Context(), board, f)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ts)
}

func (s *server) apiSearch(w http.ResponseWriter, r *http.Request) {
	board, ok := pathBoard(w, r)
	if !ok {
		return
	}
	f, err := filterParams(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	q := post.Query{Terms: r.URL.Query()["q"], Field: r.URL.Query().Get("in"), Filter: f}
	rs, err := s.Store.Search(r.Context(), board, q)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rs)
}

func (s *server) apiTags(w http.ResponseWriter, r *http.Request) {
	board, ok := pathBoard(w, r)
	if !ok {
		return
	}
	tags, err := s.Store.Tags(r.Context(), board)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tags)
}

func (s *server) apiShow(w http.ResponseWriter, r *http.Request) {
	board, ok := pathBoard(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	show, err := s.Store.Show(r.Context(), board, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, show)
}

func published(w http.ResponseWriter, pub post.Published, created bool) {
	code := http.StatusOK
	if created {
		code = http.StatusCreated
	}
	writeJSON(w, code, pub)
}

func (s *server) apiPost(w http.ResponseWriter, r *http.Request) {
	board, ok := pathBoard(w, r)
	if !ok {
		return
	}
	who, ok := writer(w, r)
	if !ok {
		return
	}
	var req post.NewThread
	if !decode(w, r, &req) {
		return
	}
	pub, created, err := s.Store.Post(r.Context(), who, board, req)
	if err != nil {
		s.fail(w, err)
		return
	}
	published(w, pub, created)
}

func (s *server) apiReply(w http.ResponseWriter, r *http.Request) {
	board, ok := pathBoard(w, r)
	if !ok {
		return
	}
	who, ok := writer(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req post.NewReply
	if !decode(w, r, &req) {
		return
	}
	pub, created, err := s.Store.Reply(r.Context(), who, board, id, req)
	if err != nil {
		s.fail(w, err)
		return
	}
	published(w, pub, created)
}

func (s *server) apiEdit(w http.ResponseWriter, r *http.Request) {
	board, ok := pathBoard(w, r)
	if !ok {
		return
	}
	who, ok := writer(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var e post.Edit
	if !decode(w, r, &e) {
		return
	}
	p, err := s.Store.Edit(r.Context(), who, board, id, e)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *server) apiDelete(w http.ResponseWriter, r *http.Request) {
	board, ok := pathBoard(w, r)
	if !ok {
		return
	}
	who, ok := writer(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	removed, err := s.Store.Delete(r.Context(), who, board, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, removed)
}

// waitsKey is the key of the context, in requests' contexts, that is done
// when requests that wait for posts must answer.
type waitsKey struct{}

func (s *server) apiUnread(w http.ResponseWriter, r *http.Request) {
	board, ok := pathBoard(w, r)
	if !ok {
		return
	}
	who, ok := writer(w, r)
	if !ok {
		return
	}
	var req post.UnreadRequest
	if !decode(w, r, &req) {
		return
	}
	ctx := r.Context()
	if req.Wait > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		if waits, ok := ctx.Value(waitsKey{}).(context.Context); ok {
			defer context.AfterFunc(waits, cancel)()
		}
	}
	u, err := s.Store.Unread(ctx, who, board, req)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}
