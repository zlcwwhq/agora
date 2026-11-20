package store

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/zlcwwhq/agora/internal/post"
)

// attachmentNames returns the names of a post's attachments, in order.
func attachmentNames(t *testing.T, s *Store, id int64) []string {
	t.Helper()
	names := []string{}
	for _, a := range must[post.Post](t)(s.Get(context.Background(), "b", id)).Attachments {
		names = append(names, a.Name)
	}
	return names
}

func TestAttachments(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	root := newThread(t, s, codex, "Logs", "see the files")
	rep := reply(t, s, claude, root, "no files here")

	if p := must[post.Post](t)(s.Get(ctx, "b", root)); p.Attachments == nil || len(p.Attachments) != 0 {
		t.Fatalf("a post without attachments has %#v", p.Attachments)
	}
	log := []byte("line 1\nline 2\n")
	a, replaced, err := s.Attach(ctx, codex, "b", root, "build.log", "text/plain; charset=utf-8", log)
	if err != nil || replaced || a != (post.Attachment{Name: "build.log", Type: "text/plain; charset=utf-8", Size: int64(len(log))}) {
		t.Fatalf("Attach = %+v, %v, %v", a, replaced, err)
	}
	if a, _, err := s.Attach(ctx, codex, "b", root, "shot.png", "", []byte{0x89, 'P', 'N', 'G'}); err != nil || a.Type != "application/octet-stream" {
		t.Errorf("Attach without a type = %+v, %v", a, err)
	}
	// Names are ordered as bytes, so capitals come first.
	if _, _, err := s.Attach(ctx, alice, "b", root, "Z.txt", "text/plain", nil); err != nil {
		t.Errorf("a person attaches to another's post: %v", err)
	}
	if got := attachmentNames(t, s, root); !slices.Equal(got, []string{"Z.txt", "build.log", "shot.png"}) {
		t.Errorf("attachments = %v", got)
	}

	// Replacing keeps the name; attaching isn't an edit.
	a, replaced, err = s.Attach(ctx, codex, "b", root, "build.log", "text/x-log", []byte("new"))
	if err != nil || !replaced || a.Size != 3 {
		t.Errorf("Attach again = %+v, %v, %v", a, replaced, err)
	}
	p := must[post.Post](t)(s.Get(ctx, "b", root))
	if p.Edited != "" || p.Editor != "" || p.Attachments[1] != (post.Attachment{Name: "build.log", Type: "text/x-log", Size: 3}) {
		t.Errorf("after attaching: %+v", p)
	}
	got, data, err := s.Attachment(ctx, "b", root, "build.log")
	if err != nil || got.Type != "text/x-log" || !bytes.Equal(data, []byte("new")) {
		t.Errorf("Attachment = %+v, %q, %v", got, data, err)
	}
	if got, data, err := s.Attachment(ctx, "b", root, "Z.txt"); err != nil || got.Size != 0 || data == nil || len(data) != 0 {
		t.Errorf("an empty attachment = %+v, %#v, %v", got, data, err)
	}

	_, _, err = s.Attach(ctx, claude, "b", root, "x.txt", "", []byte("x"))
	wantAttachmentErr(t, "another author attaches", err, post.Forbidden)
	_, _, err = s.Attach(ctx, codex, "b", root, "a/b", "", []byte("x"))
	wantAttachmentErr(t, "an invalid name", err, post.Invalid)
	_, _, err = s.Attach(ctx, codex, "b", root, "big", "", make([]byte, post.MaxAttachment+1))
	wantAttachmentErr(t, "an attachment larger than 10 MiB", err, post.Invalid)
	_, _, err = s.Attach(ctx, codex, "b", 99, "x.txt", "", []byte("x"))
	wantAttachmentErr(t, "attaching to a missing post", err, post.NotFound)
	_, _, err = s.Attach(ctx, codex, "nope", root, "x.txt", "", []byte("x"))
	wantAttachmentErr(t, "attaching on a missing board", err, post.NotFound)
	_, _, err = s.Attachment(ctx, "b", root, "nope.txt")
	wantAttachmentErr(t, "a missing attachment", err, post.NotFound)
	_, _, err = s.Attachment(ctx, "b", 99, "build.log")
	wantAttachmentErr(t, "an attachment of a missing post", err, post.NotFound)
	_, _, err = s.Attachment(ctx, "b", root, "..")
	wantAttachmentErr(t, "an invalid name to get", err, post.Invalid)
	_, err = s.Detach(ctx, claude, "b", root, "build.log")
	wantAttachmentErr(t, "another author detaches", err, post.Forbidden)
	_, err = s.Detach(ctx, claude, "b", rep, "build.log")
	wantAttachmentErr(t, "detaching a missing attachment", err, post.NotFound)
	if _, _, err := s.Attach(ctx, codex, "b", root, "max", "", make([]byte, post.MaxAttachment)); err != nil {
		t.Errorf("an attachment of 10 MiB: %v", err)
	}

	a, err = s.Detach(ctx, codex, "b", root, "shot.png")
	if err != nil || a.Name != "shot.png" || a.Size != 4 {
		t.Errorf("Detach = %+v, %v", a, err)
	}
	if got := attachmentNames(t, s, root); !slices.Equal(got, []string{"Z.txt", "build.log", "max"}) {
		t.Errorf("after detaching: %v", got)
	}
	if p := must[post.Post](t)(s.Get(ctx, "b", root)); p.Edited != "" {
		t.Errorf("detaching edited the post at %s", p.Edited)
	}
}

// wantAttachmentErr checks that err is of kind k.
func wantAttachmentErr(t *testing.T, what string, err error, k post.Kind) {
	t.Helper()
	if post.KindOf(err) != k {
		t.Errorf("%s: %v, want an error of kind %d", what, err, k)
	}
}

func TestDeleteRemovesAttachments(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	root := newThread(t, s, codex, "T", "body")
	rep := reply(t, s, claude, root, "reply")
	other := newThread(t, s, codex, "Other", "body")
	for _, id := range []int64{root, rep, other} {
		if _, _, err := s.Attach(ctx, alice, "b", id, "f.txt", "", []byte("data")); err != nil {
			t.Fatal(err)
		}
	}
	must[post.Removed](t)(s.Delete(ctx, claude, "b", rep))
	if n := count(t, s, "SELECT count(*) FROM attachments"); n != 2 {
		t.Errorf("after deleting a reply, %d attachments", n)
	}
	must[post.Removed](t)(s.Delete(ctx, codex, "b", root))
	if n := count(t, s, "SELECT count(*) FROM attachments"); n != 1 {
		t.Errorf("after deleting a thread, %d attachments", n)
	}
	if got := attachmentNames(t, s, other); !slices.Equal(got, []string{"f.txt"}) {
		t.Errorf("another thread's attachments = %v", got)
	}
}
