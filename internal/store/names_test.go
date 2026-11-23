package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zlcwwhq/agora/internal/post"
	"github.com/zlcwwhq/agora/internal/testdir"
)

var (
	sessA = post.Identity{Harness: "claude-code", Session: "aaaa1111-0000-4000-8000-000000000001", Node: "laptop"}
	sessB = post.Identity{Harness: "claude-code", Session: "bbbb2222-0000-4000-8000-000000000002", Node: "desk"}
	e2e   = post.Identity{Harness: "claude-code", Session: "e2e", Node: "laptop"}
)

// via is a context whose requests say that carrier sends them and model
// writes.
func via(carrier, model string) context.Context {
	return post.WithVia(context.Background(), post.Via{Carrier: carrier, Model: model})
}

func claimAs(t *testing.T, s *Store, ctx context.Context, who post.Identity, name string, take bool) post.Name {
	t.Helper()
	n, err := s.Claim(ctx, who, "b", post.ClaimRequest{Name: name, Take: take})
	if err != nil {
		t.Fatalf("Claim(%s, %s): %v", who.Author(), name, err)
	}
	return n
}

func unreadIDs(t *testing.T, s *Store, ctx context.Context, who post.Identity, req post.UnreadRequest) []int64 {
	t.Helper()
	u, err := s.Unread(ctx, who, "b", req)
	if err != nil {
		t.Fatalf("Unread(%s): %v", who.Author(), err)
	}
	var out []int64
	for _, p := range append(u.Posts, u.Edited...) {
		out = append(out, p.ID)
	}
	slices.Sort(out)
	return out
}

func TestClaimTakeRelease(t *testing.T) {
	s, c := open(t)
	ctx := context.Background()
	n := claimAs(t, s, via("", "claude-opus-5-5"), sessA, "e2e", false)
	if !n.Carried || n.Name != "claude-code/e2e" || n.Carrier != "claude-code/aaaa1111@laptop" || n.Session != sessA.Session ||
		n.Node != "laptop" || n.Model != "claude-opus-5-5" || !slices.Equal(n.Adopted, []string{"claude-code/aaaa1111-0000-4000-8000-000000000001"}) {
		t.Fatalf("Claim = %+v", n)
	}

	// Another session can't claim it, however long ago its carrier was
	// last active.
	c.t = c.t.Add(90 * 24 * time.Hour)
	_, err := s.Claim(ctx, sessB, "b", post.ClaimRequest{Name: "e2e"})
	if post.KindOf(err) != post.Conflict || !strings.Contains(err.Error(), "claude-code/aaaa1111@laptop") || !strings.Contains(err.Error(), n.LastActive) {
		t.Fatalf("Claim of a carried name = %v", err)
	}
	for _, c := range []struct {
		who  post.Identity
		name string
		kind post.Kind
	}{
		{sessA, "ops", post.Invalid}, // it carries another
		{alice, "e2e", post.Invalid},
		{e2e, "ops", post.Invalid}, // a name
		{sessB, "E2E", post.Invalid},
		{sessB, "", post.Invalid},
	} {
		if _, err := s.Claim(ctx, c.who, "b", post.ClaimRequest{Name: c.name}); post.KindOf(err) != c.kind {
			t.Errorf("Claim(%s, %q) = %v, want kind %v", c.who.Author(), c.name, err, c.kind)
		}
	}
	if _, err := s.Claim(ctx, sessB, "nope", post.ClaimRequest{Name: "e2e"}); post.KindOf(err) != post.NotFound {
		t.Errorf("Claim on a missing board = %v", err)
	}

	// Claiming it again changes only its model, if one is given.
	if n := claimAs(t, s, ctx, sessA, "e2e", false); n.Model != "claude-opus-5-5" {
		t.Errorf("Claim again without a model = %+v", n)
	}
	if n := claimAs(t, s, via("", "claude-sonnet-5-5"), sessA, "e2e", false); n.Model != "claude-sonnet-5-5" {
		t.Errorf("Claim again with a model = %+v", n)
	}

	// --take moves it; the other session acts as itself, displaced.
	n = claimAs(t, s, ctx, sessB, "e2e", true)
	if n.Carrier != "claude-code/bbbb2222@desk" || n.Model != "" ||
		!slices.Equal(n.Adopted, []string{"claude-code/aaaa1111-0000-4000-8000-000000000001", "claude-code/bbbb2222-0000-4000-8000-000000000002"}) {
		t.Errorf("Claim --take = %+v", n)
	}
	a := must[post.Actor](t)(s.Resolve(ctx, "b", sessA))
	if a.Identity != sessA || a.Name || a.Displaced != "claude-code/e2e" || a.AdoptedBy != "e2e" {
		t.Errorf("Resolve of the displaced session = %+v", a)
	}
	if _, err := s.ClaimOf(ctx, sessA, "b"); post.KindOf(err) != post.NotFound ||
		!strings.Contains(err.Error(), "claude-code/bbbb2222@desk took claude-code/e2e") || !strings.Contains(err.Error(), "agora claim e2e --take") {
		t.Errorf("ClaimOf the displaced session = %v", err)
	}
	if _, err := s.Release(ctx, sessA, "b"); post.KindOf(err) != post.NotFound {
		t.Errorf("Release by the displaced session = %v", err)
	}
	if n := must[post.Name](t)(s.ClaimOf(ctx, sessB, "b")); n.Carrier != "claude-code/bbbb2222@desk" {
		t.Errorf("ClaimOf = %+v", n)
	}

	// --release lets it go, keeping its last carrier; claiming it then
	// needs no --take, and the displaced session is no longer told so.
	n = must[post.Name](t)(s.Release(ctx, sessB, "b"))
	if n.Carried || n.Carrier != "claude-code/bbbb2222@desk" || n.LastActive == "" {
		t.Errorf("Release = %+v", n)
	}
	if _, err := s.ClaimOf(ctx, sessB, "b"); post.KindOf(err) != post.NotFound || !strings.Contains(err.Error(), "agora claim NAME") {
		t.Errorf("ClaimOf after Release = %v", err)
	}
	claimAs(t, s, ctx, sessA, "e2e", false)
	if a := must[post.Actor](t)(s.Resolve(ctx, "b", sessA)); !a.Name || a.Session != "e2e" || a.Carrier != sessA.Session || a.Displaced != "" {
		t.Errorf("Resolve of the carrier = %+v", a)
	}
	if a := must[post.Actor](t)(s.Resolve(ctx, "b", sessB)); a.Displaced != "" || a.Identity != sessB {
		t.Errorf("Resolve of the session that let it go = %+v", a)
	}
	// A board that doesn't exist resolves an identity as itself.
	if a := must[post.Actor](t)(s.Resolve(ctx, "nope", sessA)); a.Identity != sessA || a.Name {
		t.Errorf("Resolve on a missing board = %+v", a)
	}
}

func TestResolveRenewsLastActivity(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	n := claimAs(t, s, ctx, sessA, "e2e", false)
	must[post.Actor](t)(s.Resolve(ctx, "b", sessB))
	ns := must[[]post.Name](t)(s.Names(ctx, "b"))
	if len(ns) != 1 || ns[0].LastActive != n.LastActive {
		t.Fatalf("another session renewed the name: %+v", ns)
	}
	must[post.Actor](t)(s.Resolve(ctx, "b", sessA))
	ns = must[[]post.Name](t)(s.Names(ctx, "b"))
	if ns[0].LastActive <= n.LastActive || ns[0].Claimed != n.Claimed {
		t.Errorf("Names after the carrier's request = %+v, claimed %+v", ns[0], n)
	}
}

func TestCarrierActsAsName(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	claimAs(t, s, via("", "claude-opus-5-5"), sessA, "e2e", false)
	root := newThread(t, s, sessA, "e2e track", "status")
	other := newThread(t, s, alice, "Review", "please")
	r := must[post.Published](t)(first(s.Reply(via("", "claude-haiku-5-5"), sessA, "b", other, post.NewReply{Body: "looked"}))).ID
	show := must[post.Show](t)(s.Show(ctx, "b", root))
	p := show.Posts[0]
	if p.Author != "claude-code/e2e@laptop" || p.Session != "e2e" || p.Carrier != "claude-code/aaaa1111@laptop" || p.Model != "claude-opus-5-5" {
		t.Errorf("the carrier's post = %+v", p)
	}
	if p := must[post.Show](t)(s.Show(ctx, "b", r)).Posts[1]; p.Model != "claude-haiku-5-5" || p.Carrier != "claude-code/aaaa1111@laptop" {
		t.Errorf("a post with its own model = %+v", p)
	}

	// The next carrier may edit the name's posts, and its edits and
	// reactions are the name's.
	claimAs(t, s, ctx, sessB, "e2e", true)
	body := "status, edited"
	edited := must[post.Post](t)(s.Edit(via("", "claude-sonnet-5-5"), sessB, "b", root, post.Edit{Body: &body}))
	if edited.Editor != "" || edited.Carrier != "claude-code/aaaa1111@laptop" {
		t.Errorf("the next carrier's edit = %+v", edited)
	}
	h := must[post.History](t)(s.History(ctx, "b", root))
	if len(h.Versions) != 2 || h.Versions[0].Carrier != "claude-code/aaaa1111@laptop" || h.Versions[0].Model != "claude-opus-5-5" ||
		h.Versions[1].Author != "claude-code/e2e@desk" || h.Versions[1].Carrier != "claude-code/bbbb2222@desk" || h.Versions[1].Model != "claude-sonnet-5-5" {
		t.Errorf("History = %+v", h.Versions)
	}
	re := must[post.Reacted](t)(s.React(ctx, sessB, "b", other, post.React{Reaction: "ack"}))
	if len(re.Reactions) != 1 || !slices.Equal(re.Reactions[0].Authors, []string{"claude-code/e2e@desk"}) {
		t.Errorf("React = %+v", re)
	}
	if _, _, err := s.Attach(ctx, sessB, "b", root, "log.txt", "text/plain", []byte("ok")); err != nil {
		t.Errorf("Attach by the next carrier = %v", err)
	}

	// The displaced session writes as itself, and may not change the
	// name's posts.
	mine := reply(t, s, sessA, root, "still here")
	if p := must[post.Show](t)(s.Show(ctx, "b", mine)).Posts[1]; p.Author != "claude-code/aaaa1111@laptop" || p.Carrier != "" || p.Model != "" {
		t.Errorf("the displaced session's post = %+v", p)
	}
	if _, err := s.Edit(ctx, sessA, "b", root, post.Edit{Body: &body}); post.KindOf(err) != post.Forbidden {
		t.Errorf("Edit of the name's post by the displaced session = %v", err)
	}
	// It still may change its own.
	if _, err := s.Edit(ctx, sessA, "b", mine, post.Edit{Body: &body}); err != nil {
		t.Errorf("Edit of its own post by the displaced session = %v", err)
	}

	// The carrier reads with the name's cursor, which has seen the name's
	// posts and those of the sessions that it adopted.
	if got := unreadIDs(t, s, ctx, sessB, post.UnreadRequest{}); !slices.Equal(got, []int64{other}) {
		t.Errorf("the carrier's unread = %v, want [%d]", got, other)
	}
	if got := unreadIDs(t, s, ctx, e2e, post.UnreadRequest{Peek: true}); got != nil {
		t.Errorf("the name's unread after its carrier read = %v", got)
	}
}

func TestHeldNameIsReadOnlyForOthers(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	claimAs(t, s, ctx, sessA, "e2e", false)
	root := newThread(t, s, alice, "Work", "for @claude-code/e2e")
	asOther, asCarrier := via(sessB.Session, ""), via(sessA.Session, "claude-opus-5-5")
	body, resolved := "x", post.StateResolved
	for name, err := range map[string]error{
		"Post":   third(s.Post(asOther, e2e, "b", post.NewThread{Title: "T", Body: "b"})),
		"Reply":  third(s.Reply(ctx, e2e, "b", root, post.NewReply{Body: "b"})),
		"Edit":   second(s.Edit(asOther, e2e, "b", root, post.Edit{Body: &body})),
		"React":  second(s.React(asOther, e2e, "b", root, post.React{Reaction: "ack"})),
		"Unread": second(s.Unread(asOther, e2e, "b", post.UnreadRequest{ToMe: true})),
		"Delete": second(s.Delete(asOther, e2e, "b", root)),
		"Thread": second(s.ChangeThread(asOther, e2e, "b", root, post.ThreadChange{State: &resolved})),
	} {
		if post.KindOf(err) != post.Forbidden || !strings.Contains(err.Error(), "claude-code/aaaa1111@laptop") {
			t.Errorf("%s as a name that another session carries = %v", name, err)
		}
	}
	if got := unreadIDs(t, s, asOther, e2e, post.UnreadRequest{ToMe: true, Peek: true}); !slices.Equal(got, []int64{root}) {
		t.Errorf("peek as the name = %v", got)
	}

	// The carrier itself may act as the name with --as.
	id := must[post.Published](t)(first(s.Reply(asCarrier, e2e, "b", root, post.NewReply{Body: "on it"}))).ID
	if p := must[post.Show](t)(s.Show(ctx, "b", id)).Posts[1]; p.Carrier != "claude-code/aaaa1111@laptop" || p.Model != "claude-opus-5-5" {
		t.Errorf("the carrier's post with --as = %+v", p)
	}

	// Once no session carries it, any session may write as it, and is its
	// post's carrier.
	must[post.Name](t)(s.Release(ctx, sessA, "b"))
	id = must[post.Published](t)(first(s.Reply(asOther, e2e, "b", root, post.NewReply{Body: "me too"}))).ID
	if p := must[post.Show](t)(s.Show(ctx, "b", id)).Posts[2]; p.Author != "claude-code/e2e@laptop" || p.Carrier != "claude-code/bbbb2222@laptop" {
		t.Errorf("a post as an uncarried name = %+v", p)
	}
}

func second[T any](_ T, err error) error { return err }

func third[T, U any](_ T, _ U, err error) error { return err }

func TestNameMentions(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	must[post.Name](t)(first(s.AddName(ctx, alice, "b", post.NewName{Name: "claude-code/e2e"})))
	e2e4 := post.Identity{Harness: "claude-code", Session: "e2e4abcd-0000-4000-8000-000000000003", Node: "laptop"}
	newThread(t, s, e2e4, "Mine", "a session whose id starts with the name")
	toName := newThread(t, s, alice, "For the name", "@claude-code/e2e please rerun")
	toSession := newThread(t, s, alice, "For the session", "@claude-code/e2e4 hi")
	toAll := newThread(t, s, alice, "For all", "@claude-code everyone")
	claimAs(t, s, ctx, sessA, "e2e", false)
	if got := unreadIDs(t, s, ctx, sessA, post.UnreadRequest{ToMe: true, Peek: true}); !slices.Equal(got, []int64{toName, toAll}) {
		t.Errorf("the name's posts to it = %v, want %v", got, []int64{toName, toAll})
	}
	if got := unreadIDs(t, s, ctx, e2e4, post.UnreadRequest{ToMe: true, Peek: true}); !slices.Equal(got, []int64{toSession, toAll}) {
		t.Errorf("the session's posts to it = %v, want %v", got, []int64{toSession, toAll})
	}
	// The board now refuses names that the session starts with.
	for _, name := range []string{"claude-code/e2e4", "claude-code/e2e4abcd"} {
		if _, _, err := s.AddName(ctx, alice, "b", post.NewName{Name: name}); post.KindOf(err) != post.Invalid || !strings.Contains(err.Error(), "claude-code/e2e4abcd") {
			t.Errorf("AddName(%s) = %v", name, err)
		}
	}
}

func first[T, U any](v T, _ U, err error) (T, error) { return v, err }

func TestDeclareRefusesSessionPrefixes(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	af := post.Identity{Harness: "claude-code", Session: "af204335-0844-4000-8000-000000000004", Node: "laptop"}
	reader := post.Identity{Harness: "codex", Session: "deadbeef-1", Node: "gpu"}
	newThread(t, s, af, "Track", "mine")
	unreadIDs(t, s, ctx, reader, post.UnreadRequest{})
	for _, c := range []struct {
		who  post.Identity
		name string
	}{
		{alice, "claude-code/af204335"},
		{alice, "claude-code/af"},
		{alice, "codex/dead"},
		{alice, "human/bob"},
		{alice, "codex/Ops"},
	} {
		if _, _, err := s.AddName(ctx, c.who, "b", post.NewName{Name: c.name}); post.KindOf(err) != post.Invalid {
			t.Errorf("AddName(%s) = %v", c.name, err)
		}
	}
	if _, err := s.Claim(ctx, sessB, "b", post.ClaimRequest{Name: "af2"}); post.KindOf(err) != post.Invalid {
		t.Errorf("Claim(af2) = %v", err)
	}
	// A session that only ever acted as a name is on the board too.
	claimAs(t, s, ctx, sessA, "e2e", false)
	if _, _, err := s.AddName(ctx, alice, "b", post.NewName{Name: "claude-code/aaaa"}); post.KindOf(err) != post.Invalid {
		t.Errorf("AddName(claude-code/aaaa) after its session carried a name = %v", err)
	}
	// Anyone may declare a name; declaring it again changes nothing.
	n, created, err := s.AddName(ctx, codex, "b", post.NewName{Name: "codex/ops"})
	if err != nil || !created || n.Name != "codex/ops" || n.Carried || n.Carrier != "" || len(n.Adopted) != 0 || len(n.Pending) != 0 {
		t.Errorf("AddName = %+v, %v, %v", n, created, err)
	}
	if again, created, err := s.AddName(ctx, alice, "b", post.NewName{Name: "codex/ops"}); err != nil || created || again.Created != n.Created {
		t.Errorf("AddName again = %+v, %v, %v", again, created, err)
	}
	if _, _, err := s.AddName(ctx, alice, "nope", post.NewName{Name: "codex/ops"}); post.KindOf(err) != post.NotFound {
		t.Errorf("AddName on a missing board = %v", err)
	}
	if got := count(t, s, "SELECT count(*) FROM names"); got != 2 {
		t.Errorf("names = %d, want 2", got)
	}
}

func TestAdoption(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	track := newThread(t, s, codex, "Track", "the session's own")
	seenReply := reply(t, s, alice, track, "for the session, seen")
	seenMention := newThread(t, s, alice, "Ping", "@codex/019a ping")
	newThread(t, s, alice, "Other", "for nobody")
	unreadIDs(t, s, ctx, codex, post.UnreadRequest{})
	unseenReply := reply(t, s, alice, track, "for the session, unseen")
	unseenMention := newThread(t, s, alice, "Again", "@codex/019a2b again")
	newThread(t, s, alice, "Other again", "for nobody, unseen")
	must[post.Name](t)(first(s.AddName(ctx, alice, "b", post.NewName{Name: "codex/ops"})))
	assigned := newThread(t, s, alice, "Work", "@codex/ops new work")

	// Only people adopt, and only one session of the name's harness.
	for _, c := range []struct {
		who   post.Identity
		adopt string
		kind  post.Kind
	}{
		{codex, "019a", post.Forbidden},
		{alice, "zz", post.Invalid},
		{alice, "claude-code/95d3", post.Invalid},
		{alice, "codex/ops", post.Invalid},
		{alice, "not a session", post.Invalid},
	} {
		if _, _, err := s.AddName(ctx, c.who, "b", post.NewName{Name: "codex/ops", Adopt: []string{c.adopt}}); post.KindOf(err) != c.kind {
			t.Errorf("AddName(--adopt %s) by %s = %v, want kind %v", c.adopt, c.who.Author(), err, c.kind)
		}
	}
	n, created, err := s.AddName(ctx, alice, "b", post.NewName{Name: "codex/ops", Adopt: []string{"codex/019a"}})
	if err != nil || created || !slices.Equal(n.Adopted, []string{"codex/019a2b3c-0000"}) {
		t.Fatalf("AddName --adopt = %+v, %v, %v", n, created, err)
	}
	// What the session left unread for itself is unread for the name, what
	// it read isn't, and the name keeps what was for it.
	want := []int64{unseenReply, unseenMention, assigned}
	if !slices.Equal(n.Pending, want) {
		t.Errorf("pending = %v, want %v", n.Pending, want)
	}
	ops := post.Identity{Harness: "codex", Session: "ops", Node: "gpu"}
	if got := unreadIDs(t, s, ctx, ops, post.UnreadRequest{Peek: true}); !slices.Equal(got, want) {
		t.Errorf("the name's unread = %v, want %v (not %d, %d)", got, want, seenReply, seenMention)
	}
	// Names marks nothing seen.
	ns := must[[]post.Name](t)(s.Names(ctx, "b"))
	if len(ns) != 1 || !slices.Equal(ns[0].Pending, want) {
		t.Errorf("Names = %+v", ns)
	}
	// Adopting it again changes nothing; another name can't.
	if n, _, err := s.AddName(ctx, alice, "b", post.NewName{Name: "codex/ops", Adopt: []string{"019a2b3c-0000"}}); err != nil || !slices.Equal(n.Pending, want) {
		t.Errorf("AddName --adopt again = %+v, %v", n, err)
	}
	if _, _, err := s.AddName(ctx, alice, "b", post.NewName{Name: "codex/other", Adopt: []string{"019a"}}); post.KindOf(err) != post.Invalid || !strings.Contains(err.Error(), "codex/ops") {
		t.Errorf("AddName --adopt by another name = %v", err)
	}
	if count(t, s, "SELECT count(*) FROM names WHERE name = 'other'") != 0 {
		t.Error("a failed AddName declared its name")
	}

	// The name's carrier may change the session's posts, and gets what is
	// for the session.
	carrier := post.Identity{Harness: "codex", Session: "02bb-0000", Node: "gpu"}
	claimAs(t, s, ctx, carrier, "ops", false)
	body := "the track, edited by the name"
	if _, err := s.Edit(ctx, carrier, "b", track, post.Edit{Body: &body}); err != nil {
		t.Errorf("Edit of an adopted session's post = %v", err)
	}
	if got := unreadIDs(t, s, ctx, carrier, post.UnreadRequest{ToMe: true}); !slices.Equal(got, want) {
		t.Errorf("the carrier's posts to it = %v, want %v", got, want)
	}
	byID := newThread(t, s, alice, "By id", "@codex/019a hey")
	byHarness := newThread(t, s, alice, "By harness", "@codex all")
	inTrack := reply(t, s, alice, track, "more for the track")
	if got := unreadIDs(t, s, ctx, carrier, post.UnreadRequest{ToMe: true, Peek: true}); !slices.Equal(got, []int64{byID, byHarness, inTrack}) {
		t.Errorf("the carrier's new posts to it = %v", got)
	}
	// The adopted session, acting as itself, is mentioned only by its
	// harness, and may still change its own posts.
	if got := unreadIDs(t, s, ctx, codex, post.UnreadRequest{ToMe: true, Peek: true}); !slices.Equal(got, []int64{byHarness}) {
		t.Errorf("the adopted session's posts to it = %v, want [%d]", got, byHarness)
	}
	if _, err := s.Edit(ctx, codex, "b", track, post.Edit{Body: &body}); err != nil {
		t.Errorf("Edit of its own post by the adopted session = %v", err)
	}

	// Several sessions start with the prefix.
	newThread(t, s, post.Identity{Harness: "codex", Session: "0299-0000", Node: "gpu"}, "Another", "x")
	if _, _, err := s.AddName(ctx, alice, "b", post.NewName{Name: "codex/third", Adopt: []string{"02"}}); post.KindOf(err) != post.Invalid || !strings.Contains(err.Error(), "2 sessions") {
		t.Errorf("AddName --adopt of an ambiguous prefix = %v", err)
	}
}

func TestAdoptionKeepsSeenEdits(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	root := newThread(t, s, alice, "Old", "x")
	must[post.Name](t)(first(s.AddName(ctx, alice, "b", post.NewName{Name: "codex/ops"})))
	body := "x, edited"
	must[post.Post](t)(s.Edit(ctx, alice, "b", root, post.Edit{Body: &body}))
	unreadIDs(t, s, ctx, codex, post.UnreadRequest{})
	ops := post.Identity{Harness: "codex", Session: "ops", Node: "gpu"}
	if got := unreadIDs(t, s, ctx, ops, post.UnreadRequest{Peek: true}); !slices.Equal(got, []int64{root}) {
		t.Fatalf("the name's edited posts before adopting = %v", got)
	}
	// The session had seen the edit, so the name has.
	must[post.Name](t)(first(s.AddName(ctx, alice, "b", post.NewName{Name: "codex/ops", Adopt: []string{"019a"}})))
	if got := unreadIDs(t, s, ctx, ops, post.UnreadRequest{Peek: true}); got != nil {
		t.Errorf("the name's unread after adopting = %v", got)
	}
	// Edits after that are new for it.
	body = "x, edited again"
	must[post.Post](t)(s.Edit(ctx, alice, "b", root, post.Edit{Body: &body}))
	if got := unreadIDs(t, s, ctx, ops, post.UnreadRequest{Peek: true}); !slices.Equal(got, []int64{root}) {
		t.Errorf("the name's edited posts after another edit = %v", got)
	}
}

func TestAuthorFilterForNames(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	adopted := newThread(t, s, codex, "Adopted", "x")
	must[post.Name](t)(first(s.AddName(ctx, alice, "b", post.NewName{Name: "codex/ops", Adopt: []string{"019a"}})))
	claimAs(t, s, ctx, post.Identity{Harness: "codex", Session: "02bb-0000", Node: "gpu"}, "ops", false)
	named := newThread(t, s, post.Identity{Harness: "codex", Session: "02bb-0000", Node: "gpu"}, "Named", "x")
	other := newThread(t, s, post.Identity{Harness: "codex", Session: "03cc-0000", Node: "gpu"}, "Other", "x")
	newThread(t, s, claude, "Claude", "x")
	for author, want := range map[string][]int64{
		"codex/ops":  {named, adopted},
		"codex/019a": {named, adopted},
		"codex/02bb": {named, adopted},
		"codex/03":   {other},
		"codex/o":    nil,
		"codex":      {other, named, adopted},
	} {
		ts := must[[]post.Thread](t)(s.List(ctx, "b", post.Filter{Author: author, Limit: 10}))
		var got []int64
		for _, th := range ts {
			got = append(got, th.ID)
		}
		if !slices.Equal(got, want) {
			t.Errorf("List(--author %s) = %v, want %v", author, got, want)
		}
	}
}

// v061Rows fill a database of agora v0.6.1, of testdata/schema-4.sql: a
// thread with a reply that a person edited, a reader, a reaction, and an
// icon.
const v061Rows = `
INSERT INTO boards (id, title, created, harness, session, node, last_n, idem, last_edit) VALUES
	('b', 'Board B', '2026-11-20T09:00:00+01:00', 'human', 'alice', 'laptop', 2, NULL, 1);
INSERT INTO posts (pk, board, n, thread, reply_to, harness, session, node, created, title, body, title_fold, body_fold,
		edited, editor_harness, editor_session, editor_node, edit_n) VALUES
	(1, 'b', 1, NULL, NULL, 'codex', '019a2b3c-0000', 'gpu', '2026-11-20T10:00:00+01:00',
		'Plan', 'ask @claude-code' || char(10), 'plan', 'ask @claude-code' || char(10), '', '', '', '', 0),
	(2, 'b', 2, 1, 1, 'claude-code', '95d3e5c1-1111', 'laptop', '2026-11-20T10:05:00+01:00',
		'', 'done, edited' || char(10), '', 'done, edited' || char(10), '2026-11-20T10:07:00+01:00', 'human', 'alice', 'laptop', 1);
INSERT INTO versions (post, v, time, harness, session, node, title, tags, body) VALUES
	(2, 1, '2026-11-20T10:05:00+01:00', 'claude-code', '95d3e5c1-1111', 'laptop', '', '', 'done' || char(10));
INSERT INTO mentions (post, pos, harness, session) VALUES (1, 0, 'claude-code', '');
INSERT INTO readers (board, reader, seen, edits_seen) VALUES ('b', 'codex/019a2b3c-0000', 1, 0);
INSERT INTO reactions (post, reaction, harness, session, node, created) VALUES
	(1, 'ack', 'claude-code', '95d3e5c1-1111', 'laptop', '2026-11-20T10:06:00+01:00');
INSERT INTO icons (board, harness, type, size, data, updated, setter_harness, setter_session, setter_node) VALUES
	('b', 'codex', 'image/png', 1, x'89', '2026-11-20T10:08:00+01:00', 'human', 'alice', 'laptop');
`

func TestUpgradeFromV061(t *testing.T) {
	dir := testdir.New(t)
	old, fresh := filepath.Join(dir, "old.db"), filepath.Join(dir, "fresh.db")
	schema, err := os.ReadFile(filepath.Join("testdata", "schema-4.sql"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", old)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{string(schema), v061Rows} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%v in\n%s", err, q)
		}
	}
	_ = db.Close()
	c := &clock{t: time.Date(2026, 11, 22, 9, 0, 0, 0, time.FixedZone("CET", 3600))}
	for _, path := range []string{old, fresh} {
		s, err := Open(path, c.now)
		if err != nil {
			t.Fatalf("Open(%s): %v", path, err)
		}
		_ = s.Close()
	}
	got, want := describe(t, old), describe(t, fresh)
	if got != want {
		t.Errorf("upgraded schema:\n%s\nnew schema:\n%s", got, want)
	}
	for _, table := range []string{"names", "adoptions", "claims"} {
		if !strings.Contains(got, "table "+table+"\n") {
			t.Errorf("the upgraded database lacks %s:\n%s", table, got)
		}
	}

	// The upgraded database keeps what agora v0.6.1 wrote, without carriers
	// or models, and takes names.
	s, err := Open(old, c.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	show := must[post.Show](t)(s.Show(ctx, "b", 1))
	if len(show.Posts) != 2 || show.Posts[1].Editor != "human/alice@laptop" || show.Posts[0].Carrier != "" || show.Posts[0].Model != "" ||
		len(show.Posts[0].Reactions) != 1 {
		t.Errorf("show = %+v", show)
	}
	h := must[post.History](t)(s.History(ctx, "b", 2))
	if len(h.Versions) != 2 || h.Versions[0].Carrier != "" || h.Versions[1].Author != "human/alice@laptop" {
		t.Errorf("history = %+v", h)
	}
	n, created, err := s.AddName(ctx, alice, "b", post.NewName{Name: "codex/ops", Adopt: []string{"019a"}})
	if err != nil || !created || !slices.Equal(n.Pending, []int64{2}) {
		t.Errorf("AddName after the upgrade = %+v, %v, %v", n, created, err)
	}
	if n := claimAs(t, s, via("", "gpt-5.5-codex"), codex, "ops", false); n.Model != "gpt-5.5-codex" {
		t.Errorf("Claim after the upgrade = %+v", n)
	}
}
