package server

import (
	"net/http"

	"github.com/zlcwwhq/agora/internal/post"
)

func (p *pages) routeReactionForms(mux *http.ServeMux) {
	mux.HandleFunc("/b/{board}/t/{id}/react", p.handle(handlers{http.MethodPost: p.inBoard(p.react)}))
	mux.HandleFunc("/b/{board}/t/{id}/vote", p.handle(handlers{http.MethodPost: p.inBoard(p.vote)}))
}

// react reacts with the field reaction, or takes back the field undo.
func (p *pages) react(w http.ResponseWriter, r *http.Request, b *post.Board, f form) {
	id, ok := p.pathID(w, r, b, f)
	if !ok {
		return
	}
	_, give := r.PostForm["reaction"]
	_, undo := r.PostForm["undo"]
	if give == undo {
		p.failIn(w, r, b, http.StatusBadRequest, "A reaction is given with the field reaction, or taken back with the field undo: send one of them.", f)
		return
	}
	req := post.React{Reaction: r.PostForm.Get("reaction")}
	if undo {
		req = post.React{Reaction: r.PostForm.Get("undo"), Undo: true}
	}
	reacted, err := p.s.Store.React(r.Context(), p.s.Person, b.ID, id, req)
	if err != nil {
		p.boardFailed(w, r, b, err, f)
		return
	}
	http.Redirect(w, r, inThread(b.ID, reacted.Thread, reacted.ID), http.StatusSeeOther)
}

// vote votes as the field vote says: up, down, or none.
func (p *pages) vote(w http.ResponseWriter, r *http.Request, b *post.Board, f form) {
	id, ok := p.pathID(w, r, b, f)
	if !ok {
		return
	}
	voted, err := p.s.Store.Vote(r.Context(), p.s.Person, b.ID, id, post.Vote{Vote: r.PostForm.Get("vote")})
	if err != nil {
		p.boardFailed(w, r, b, err, f)
		return
	}
	http.Redirect(w, r, inThread(b.ID, voted.Thread, voted.ID), http.StatusSeeOther)
}

// reactionView is a reaction to a post, by its name, with its emoji, marked
// if the person gave it.
type reactionView struct {
	Reaction, Emoji string
	Authors         []string
	Mine            bool
}

// reactionViews returns a post's reactions, and every reaction for the
// picker, in GitHub's order, each marked if the person gave it, as g says.
func reactionViews(rs []post.Reaction, g post.Given) (reactions, picker []reactionView) {
	mine := map[string]bool{}
	for _, r := range g.Reactions {
		mine[r] = true
	}
	for _, r := range rs {
		reactions = append(reactions, reactionView{Reaction: r.Reaction, Emoji: post.Emoji(r.Reaction), Authors: r.Authors, Mine: mine[r.Reaction]})
	}
	for _, r := range post.GitHubReactions {
		picker = append(picker, reactionView{Reaction: r.Name, Emoji: r.Emoji, Mine: mine[r.Name]})
	}
	return reactions, picker
}
