package server

import (
	"context"
	"io"
	"mime"
	"net/http"
	"slices"
	"strconv"

	"github.com/zlcwwhq/agora/internal/post"
)

// IconStore is what the server needs of the store for boards' icons.
type IconStore interface {
	SetIcon(ctx context.Context, who post.Identity, board, harness string, data []byte) (post.Icon, bool, error)
	Icons(ctx context.Context, board string) ([]post.Icon, error)
	Icon(ctx context.Context, board, harness string) (post.Icon, []byte, error)
	RemoveIcon(ctx context.Context, who post.Identity, board, harness string) (post.Icon, error)
}

func (s *server) routeIconAPI(mux *http.ServeMux, b string) {
	const icon = "/icons/{harness}"
	mux.HandleFunc("GET "+b+"/icons", s.apiIcons)
	mux.HandleFunc("PUT "+b+icon, s.apiSetIcon)
	mux.HandleFunc("GET "+b+icon, s.apiIcon)
	mux.HandleFunc("DELETE "+b+icon, s.apiRemoveIcon)
	for path, allow := range map[string]string{
		b + "/icons": "GET, HEAD",
		b + icon:     "GET, HEAD, PUT, DELETE",
	} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", allow)
			writeError(w, http.StatusMethodNotAllowed, r.Method+" isn't allowed here: use "+allow)
		})
	}
}

// pathHarness returns the harness that the path gives, answering the
// request if it isn't one.
func pathHarness(w http.ResponseWriter, r *http.Request) (string, bool) {
	harness := r.PathValue("harness")
	if err := post.CheckHarness(harness); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return "", false
	}
	return harness, true
}

// iconRequest reads what the requests about one icon have: the board, the
// identity if write is true, and the harness, answering the request if it
// can't.
func iconRequest(w http.ResponseWriter, r *http.Request, write bool) (board string, who post.Identity, harness string, ok bool) {
	if board, ok = pathBoard(w, r); !ok {
		return
	}
	if write {
		if who, ok = writer(w, r); !ok {
			return
		}
	}
	harness, ok = pathHarness(w, r)
	return
}

func (s *server) apiIcons(w http.ResponseWriter, r *http.Request) {
	board, ok := pathBoard(w, r)
	if !ok {
		return
	}
	is, err := s.Store.Icons(r.Context(), board)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, is)
}

func (s *server) apiSetIcon(w http.ResponseWriter, r *http.Request) {
	board, who, harness, ok := iconRequest(w, r, true)
	if !ok {
		return
	}
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || !slices.Contains(post.IconTypes, mt) {
		writeError(w, http.StatusUnsupportedMediaType, "an icon's Content-Type must be image/png, image/jpeg, image/gif, or image/webp")
		return
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		s.fail(w, err)
		return
	}
	if typ, ok := post.IconType(data); len(data) > 0 && typ != mt {
		msg := "the icon isn't a PNG, JPEG, GIF, or WebP image"
		if ok {
			msg = "the icon is " + typ + ", not " + mt
		}
		writeError(w, http.StatusUnsupportedMediaType, msg)
		return
	}
	i, replaced, err := s.Store.SetIcon(r.Context(), who, board, harness, data)
	if err != nil {
		s.fail(w, err)
		return
	}
	code := http.StatusCreated
	if replaced {
		code = http.StatusOK
	}
	writeJSON(w, code, i)
}

func (s *server) apiIcon(w http.ResponseWriter, r *http.Request) {
	board, _, harness, ok := iconRequest(w, r, false)
	if !ok {
		return
	}
	i, data, err := s.Store.Icon(r.Context(), board, harness)
	if err != nil {
		s.fail(w, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", i.Type)
	h.Set("Content-Length", strconv.Itoa(len(data)))
	h.Set("X-Content-Type-Options", "nosniff")
	// As attachments: on the pages' origin, it mustn't act as one of them.
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *server) apiRemoveIcon(w http.ResponseWriter, r *http.Request) {
	board, who, harness, ok := iconRequest(w, r, true)
	if !ok {
		return
	}
	i, err := s.Store.RemoveIcon(r.Context(), who, board, harness)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, i)
}
