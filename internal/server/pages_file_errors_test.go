package server

import (
	"context"
	"net/url"
	"testing"

	"github.com/zlcwwhq/agora/internal/post"
)

// Files that a form can't attach, by a name that isn't allowed or that two
// files share, are what api/web.md's forms reject: the form comes again,
// with what was typed and why, and nothing is written.
func TestFormsGiveBackRefusedFiles(t *testing.T) {
	s := serve(t, alice)
	root := s.thread(alice, "Plan", "old words")
	dup := []formFile{{"a.txt", []byte("a")}, {"a.txt", []byte("b")}}
	bad := []formFile{{"..", []byte("a")}}

	r := s.postFiles("/b/b/new", url.Values{"title": {"Kept title"}, "tags": {"kept"}, "body": {"kept words"}}, dup...)
	expect(t, r, 400, "Two files are named a.txt", "Choose the files again",
		`<input id="f-title" type="text" name="title" value="Kept title"`, `value="kept"`, "kept words</textarea>")
	r = s.postFiles("/b/b/new", url.Values{"title": {"Kept title"}, "body": {"kept words"}}, bad...)
	expect(t, r, 400, "Choose the files again", `id="f-title"`, "kept words</textarea>")
	if ts := s.threads(); len(ts) != 1 {
		t.Errorf("a refused new thread posted: %+v", ts)
	}

	for _, files := range [][]formFile{dup, bad} {
		r = s.postFiles("/b/b/t/1", url.Values{"body": {"kept reply"}}, files...)
		expect(t, r, 400, "Choose the files again", `<textarea id="reply-body"`, "kept reply</textarea>")
	}
	if show, _ := s.store.Show(context.Background(), "b", root); len(show.Posts) != 1 {
		t.Errorf("a refused reply posted: %+v", show.Posts)
	}

	for _, files := range [][]formFile{dup, bad} {
		r = s.postFiles("/b/b/t/1/edit", url.Values{"title": {"New title"}, "tags": {""}, "body": {"new words"}}, files...)
		expect(t, r, 400, "Choose the files again", `value="New title"`, "new words</textarea>", `action="/b/b/t/1/edit"`)
	}
	p, err := s.store.Get(context.Background(), "b", root)
	if err != nil || p.Title != "Plan" || p.Body != "old words\n" || p.Edited != "" || len(p.Attachments) != 0 {
		t.Errorf("a refused edit changed the post: %+v, %v", p, err)
	}
	if _, err := s.store.Get(context.Background(), "b", root+1); post.KindOf(err) != post.NotFound {
		t.Errorf("a refused form wrote a post: %v", err)
	}
}
