package server

import (
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
	mux.HandleFunc("GET /api/threads", s.apiList)
	mux.HandleFunc("GET /api/search", s.apiSearch)
	mux.HandleFunc("GET /api/tags", s.apiTags)
	mux.HandleFunc("GET /api/posts/{id}", s.apiShow)
	mux.HandleFunc("POST /api/threads", s.apiPost)
	mux.HandleFunc("POST /api/posts/{id}/replies", s.apiReply)
	mux.HandleFunc("PATCH /api/posts/{id}", s.apiEdit)
	mux.HandleFunc("DELETE /api/posts/{id}", s.apiDelete)
	mux.HandleFunc("POST /api/unread", s.apiUnread)
	// Known paths with other methods.
	for path, allow := range map[string]string{
		"/api/threads":            "GET, HEAD, POST",
		"/api/search":             "GET, HEAD",
		"/api/tags":               "GET, HEAD",
		"/api/posts/{id}":         "GET, HEAD, PATCH, DELETE",
		"/api/posts/{id}/replies": "POST",
		"/api/unread":             "POST",
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
		msg = "the request is larger than 1 MiB"
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

// filterParams reads the parameters that narrow lists of threads.
func filterParams(r *http.Request) (post.Filter, error) {
	q := r.URL.Query()
	f := post.Filter{Tags: q["t"], Author: q.Get("author"), Limit: post.DefaultLimit}
	if n := q.Get("n"); n != "" {
		v, err := strconv.Atoi(n)
		if err != nil || v < 1 {
			return f, post.Errorf(post.Invalid, "invalid n %q: want a number of at least 1", n)
		}
		f.Limit = v
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
	f, err := filterParams(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	ts, err := s.Board.List(r.Context(), f)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ts)
}

func (s *server) apiSearch(w http.ResponseWriter, r *http.Request) {
	f, err := filterParams(r)
	if err != nil {
		s.fail(w, err)
		return
	}
	q := post.Query{Terms: r.URL.Query()["q"], Field: r.URL.Query().Get("in"), Filter: f}
	rs, err := s.Board.Search(r.Context(), q)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rs)
}

func (s *server) apiTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.Board.Tags(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tags)
}

func (s *server) apiShow(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	show, err := s.Board.Show(r.Context(), id)
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
	who, ok := writer(w, r)
	if !ok {
		return
	}
	var req post.NewThread
	if !decode(w, r, &req) {
		return
	}
	pub, created, err := s.Board.Post(r.Context(), who, req)
	if err != nil {
		s.fail(w, err)
		return
	}
	published(w, pub, created)
}

func (s *server) apiReply(w http.ResponseWriter, r *http.Request) {
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
	pub, created, err := s.Board.Reply(r.Context(), who, id, req)
	if err != nil {
		s.fail(w, err)
		return
	}
	published(w, pub, created)
}

func (s *server) apiEdit(w http.ResponseWriter, r *http.Request) {
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
	p, err := s.Board.Edit(r.Context(), who, id, e)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *server) apiDelete(w http.ResponseWriter, r *http.Request) {
	who, ok := writer(w, r)
	if !ok {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	removed, err := s.Board.Delete(r.Context(), who, id)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, removed)
}

func (s *server) apiUnread(w http.ResponseWriter, r *http.Request) {
	who, ok := writer(w, r)
	if !ok {
		return
	}
	var req post.UnreadRequest
	if !decode(w, r, &req) {
		return
	}
	u, err := s.Board.Unread(r.Context(), who, req)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}
