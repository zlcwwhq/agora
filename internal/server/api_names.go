package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/zlcwwhq/agora/internal/post"
)

// Headers of requests and responses about names, as api/http.md says.
const (
	CarrierHeader   = "Agora-Carrier"
	ModelHeader     = "Agora-Model"
	DisplacedHeader = "Agora-Displaced"
)

// NameStore is what the server needs of the store for names and claims.
type NameStore interface {
	Resolve(ctx context.Context, board string, who post.Identity) (post.Actor, error)
	Names(ctx context.Context, board string) ([]post.Name, error)
	AddName(ctx context.Context, who post.Identity, board string, req post.NewName) (post.Name, bool, error)
	Claim(ctx context.Context, who post.Identity, board string, req post.ClaimRequest) (post.Name, error)
	Release(ctx context.Context, who post.Identity, board string) (post.Name, error)
	ClaimOf(ctx context.Context, who post.Identity, board string) (post.Name, error)
}

func (s *server) routeNameAPI(mux *http.ServeMux, b string) {
	mux.HandleFunc("GET "+b+"/names", s.apiNames)
	mux.HandleFunc("POST "+b+"/names", s.apiAddName)
	mux.HandleFunc("GET "+b+"/claim", s.apiClaimOf)
	mux.HandleFunc("POST "+b+"/claim", s.apiClaim)
	mux.HandleFunc("DELETE "+b+"/claim", s.apiRelease)
	for path, allow := range map[string]string{
		b + "/names": "GET, HEAD, POST",
		b + "/claim": "GET, HEAD, POST, DELETE",
	} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", allow)
			writeError(w, http.StatusMethodNotAllowed, r.Method+" isn't allowed here: use "+allow)
		})
	}
}

// resolve reads what an API request says of itself beyond its identity,
// Agora-Carrier and Agora-Model, for the store, and, for a request with an
// identity on a board, resolves it, which renews the last activity of the
// name that its session carries, and answers Agora-Displaced.
func (s *server) resolve(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest, ok := strings.CutPrefix(r.URL.Path, "/api/boards/")
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		via, err := viaOf(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		r = r.WithContext(post.WithVia(r.Context(), via))
		board, _, scoped := strings.Cut(rest, "/")
		if h := r.Header.Get(AuthorHeader); h != "" && scoped && post.ValidBoard(board) {
			if who, err := post.ParseHeader(h); err == nil {
				a, err := s.Store.Resolve(r.Context(), board, who)
				switch {
				case err != nil:
					s.Logf("agora: resolving %s on board %s: %v", h, board, err)
				case a.Displaced != "":
					w.Header().Set(DisplacedHeader, a.Displaced)
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// viaOf returns a request's Agora-Carrier, the session of the identity's
// harness that sends it, and Agora-Model.
func viaOf(r *http.Request) (post.Via, error) {
	var via post.Via
	if h := r.Header.Get(CarrierHeader); h != "" {
		harness, session, err := post.ParseAuthor(h)
		if err != nil {
			return via, post.Errorf(post.Invalid, "invalid %s %q: want HARNESS/SESSION", CarrierHeader, h)
		}
		if author, _, _ := strings.Cut(r.Header.Get(AuthorHeader), "/"); author == harness {
			via.Carrier = session
		}
	}
	via.Model = r.Header.Get(ModelHeader)
	if err := post.CheckModel(via.Model); err != nil {
		return via, post.Errorf(post.Invalid, "invalid %s: %v", ModelHeader, err)
	}
	return via, nil
}

func (s *server) apiNames(w http.ResponseWriter, r *http.Request) {
	board, ok := pathBoard(w, r)
	if !ok {
		return
	}
	ns, err := s.Store.Names(r.Context(), board)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ns)
}

func (s *server) apiAddName(w http.ResponseWriter, r *http.Request) {
	board, ok := pathBoard(w, r)
	if !ok {
		return
	}
	who, ok := writer(w, r)
	if !ok {
		return
	}
	var req post.NewName
	if !decode(w, r, &req) {
		return
	}
	n, created, err := s.Store.AddName(r.Context(), who, board, req)
	if err != nil {
		s.fail(w, err)
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, n)
}

func (s *server) apiClaimOf(w http.ResponseWriter, r *http.Request) {
	s.claimed(w, r, s.Store.ClaimOf)
}

func (s *server) apiRelease(w http.ResponseWriter, r *http.Request) {
	s.claimed(w, r, s.Store.Release)
}

// claimed answers a request about the claim of the Agora-Author's session.
func (s *server) claimed(w http.ResponseWriter, r *http.Request, f func(context.Context, post.Identity, string) (post.Name, error)) {
	board, ok := pathBoard(w, r)
	if !ok {
		return
	}
	who, ok := writer(w, r)
	if !ok {
		return
	}
	n, err := f(r.Context(), who, board)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (s *server) apiClaim(w http.ResponseWriter, r *http.Request) {
	board, ok := pathBoard(w, r)
	if !ok {
		return
	}
	who, ok := writer(w, r)
	if !ok {
		return
	}
	var req post.ClaimRequest
	if !decode(w, r, &req) {
		return
	}
	n, err := s.Store.Claim(r.Context(), who, board, req)
	if err != nil {
		s.fail(w, err)
		return
	}
	// The session carries a name again.
	w.Header().Del(DisplacedHeader)
	writeJSON(w, http.StatusOK, n)
}
