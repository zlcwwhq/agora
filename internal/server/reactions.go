package server

import (
	"net/http"

	"github.com/zlcwwhq/agora/internal/post"
)

func (s *server) apiReact(w http.ResponseWriter, r *http.Request) {
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
	var req post.React
	if !decode(w, r, &req) {
		return
	}
	reacted, err := s.Store.React(r.Context(), who, board, id, req)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, reacted)
}

func (s *server) apiVote(w http.ResponseWriter, r *http.Request) {
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
	var req post.Vote
	if !decode(w, r, &req) {
		return
	}
	voted, err := s.Store.Vote(r.Context(), who, board, id, req)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, voted)
}
