package store

import (
	"context"
	"testing"

	"github.com/zlcwwhq/agora/internal/post"
)

// A lock stops agents' edits of every kind, single tags included, but not
// their reactions, which api/cli.md leaves open to anyone.
func TestLockedThreadReactionsAndSingleTags(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	root := newThread(t, s, codex, "Locked", "body")
	rep := reply(t, s, claude, root, "a reply")
	must[post.Thread](t)(s.ChangeThread(ctx, alice, "b", root, post.ThreadChange{Locked: valuePtr(true)}))

	add := []string{"decided"}
	if _, err := s.Edit(ctx, claude, "b", rep, post.Edit{AddTags: &add}); post.KindOf(err) != post.Forbidden {
		t.Errorf("an agent adds a tag in a locked thread: %v", err)
	}
	if h := must[post.History](t)(s.History(ctx, "b", rep)); len(h.Versions) != 1 {
		t.Errorf("a refused edit kept a version: %+v", h.Versions)
	}

	r := must[post.Reacted](t)(s.React(ctx, codex, "b", rep, post.React{Reaction: "ack"}))
	if len(r.Reactions) != 1 || r.Reactions[0].Reaction != "eyes" {
		t.Errorf("an agent's reaction in a locked thread: %+v", r.Reactions)
	}

	p := must[post.Post](t)(s.Edit(ctx, alice, "b", rep, post.Edit{AddTags: &add}))
	if len(p.Tags) != 1 || p.Tags[0] != "decided" || p.Versions != 2 {
		t.Errorf("a person adds a tag in a locked thread: tags %v, versions %d", p.Tags, p.Versions)
	}
}
