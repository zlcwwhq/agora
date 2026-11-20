package server

import (
	"context"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/zlcwwhq/agora/internal/post"
)

// ThreadStore is what the server needs of the store for threads' states,
// attachments, moves, and backups.
type ThreadStore interface {
	ChangeThread(ctx context.Context, who post.Identity, board string, id int64, c post.ThreadChange) (post.Thread, error)
	Attach(ctx context.Context, who post.Identity, board string, id int64, name, typ string, data []byte) (post.Attachment, bool, error)
	Attachment(ctx context.Context, board string, id int64, name string) (post.Attachment, []byte, error)
	Detach(ctx context.Context, who post.Identity, board string, id int64, name string) (post.Attachment, error)
	Move(ctx context.Context, who post.Identity, board string, id int64, m post.Move) (post.Moved, error)
	// Backup returns a consistent copy of the database and its size.
	Backup(ctx context.Context) (io.ReadCloser, int64, error)
}

// backupType is the media type of backups.
const backupType = "application/vnd.sqlite3"

func (s *server) routeThreadAPI(mux *http.ServeMux, b string) {
	const file = "/posts/{id}/attachments/{name}"
	mux.HandleFunc("PATCH "+b+"/threads/{id}", s.apiChangeThread)
	mux.HandleFunc("POST "+b+"/posts/{id}/move", s.apiMove)
	mux.HandleFunc("PUT "+b+file, s.apiAttach)
	mux.HandleFunc("GET "+b+file, s.apiAttachment)
	mux.HandleFunc("DELETE "+b+file, s.apiDetach)
	mux.HandleFunc("GET /api/backup", s.apiBackup)
	for path, allow := range map[string]string{
		b + "/threads/{id}":    "PATCH",
		b + "/posts/{id}/move": "POST",
		b + file:               "GET, HEAD, PUT, DELETE",
		"/api/backup":          "GET, HEAD",
	} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", allow)
			writeError(w, http.StatusMethodNotAllowed, r.Method+" isn't allowed here: use "+allow)
		})
	}
}

// postRequest reads what the requests about one post have: the board, the
// identity if write is true, and the post, answering the request if it
// can't.
func postRequest(w http.ResponseWriter, r *http.Request, write bool) (board string, who post.Identity, id int64, ok bool) {
	if board, ok = pathBoard(w, r); !ok {
		return
	}
	if write {
		if who, ok = writer(w, r); !ok {
			return
		}
	}
	id, ok = pathID(w, r)
	return
}

// pathName returns the attachment's name that the path gives, answering the
// request if it isn't one.
func pathName(w http.ResponseWriter, r *http.Request) (string, bool) {
	name := r.PathValue("name")
	if err := post.CheckAttachmentName(name); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	return name, true
}

func (s *server) apiChangeThread(w http.ResponseWriter, r *http.Request) {
	board, who, id, ok := postRequest(w, r, true)
	if !ok {
		return
	}
	var c post.ThreadChange
	if !decode(w, r, &c) {
		return
	}
	t, err := s.Store.ChangeThread(r.Context(), who, board, id, c)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *server) apiMove(w http.ResponseWriter, r *http.Request) {
	board, who, id, ok := postRequest(w, r, true)
	if !ok {
		return
	}
	var m post.Move
	if !decode(w, r, &m) {
		return
	}
	moved, err := s.Store.Move(r.Context(), who, board, id, m)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, moved)
}

// mediaType returns the media type that a Content-Type gives an attachment:
// application/octet-stream if it is empty.
func mediaType(ctype string) (string, error) {
	if ctype == "" {
		return "application/octet-stream", nil
	}
	mt, params, err := mime.ParseMediaType(ctype)
	if err == nil {
		if t := mime.FormatMediaType(mt, params); t != "" {
			return t, nil
		}
	}
	return "", post.Errorf(post.Invalid, "invalid media type %q", ctype)
}

func (s *server) apiAttach(w http.ResponseWriter, r *http.Request) {
	board, who, id, ok := postRequest(w, r, true)
	if !ok {
		return
	}
	name, ok := pathName(w, r)
	if !ok {
		return
	}
	typ, err := mediaType(r.Header.Get("Content-Type"))
	if err != nil {
		s.fail(w, err)
		return
	}
	allowBody(r, maxAttachmentBody)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		s.fail(w, err)
		return
	}
	a, replaced, err := s.Store.Attach(r.Context(), who, board, id, name, typ, data)
	if err != nil {
		s.fail(w, err)
		return
	}
	code := http.StatusCreated
	if replaced {
		code = http.StatusOK
	}
	writeJSON(w, code, a)
}

func (s *server) apiAttachment(w http.ResponseWriter, r *http.Request) {
	board, _, id, ok := postRequest(w, r, false)
	if !ok {
		return
	}
	name, ok := pathName(w, r)
	if !ok {
		return
	}
	a, data, err := s.Store.Attachment(r.Context(), board, id, name)
	if err != nil {
		s.fail(w, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", a.Type)
	h.Set("Content-Length", strconv.Itoa(len(data)))
	h.Set("X-Content-Type-Options", "nosniff")
	// The API is on the pages' origin, so that what a browser shows of it
	// mustn't act as one of them.
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *server) apiDetach(w http.ResponseWriter, r *http.Request) {
	board, who, id, ok := postRequest(w, r, true)
	if !ok {
		return
	}
	name, ok := pathName(w, r)
	if !ok {
		return
	}
	a, err := s.Store.Detach(r.Context(), who, board, id, name)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *server) apiBackup(w http.ResponseWriter, r *http.Request) {
	if _, ok := writer(w, r); !ok {
		return
	}
	rc, size, err := s.Store.Backup(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	defer func() { _ = rc.Close() }()
	h := w.Header()
	h.Set("Content-Type", backupType)
	h.Set("Content-Length", strconv.FormatInt(size, 10))
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, rc); err != nil {
		// The client sees a body shorter than its length.
		s.Logf("agora: sending a backup: %v", err)
	}
}
