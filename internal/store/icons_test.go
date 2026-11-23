package store

import (
	"bytes"
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

// Icons' first bytes, followed by anything.
var (
	pngIcon  = []byte("\x89PNG\r\n\x1a\nrest of a png")
	jpegIcon = []byte("\xff\xd8\xff\xe0rest of a jpeg")
	gifIcon  = []byte("GIF89arest of a gif")
	webpIcon = []byte("RIFF\x10\x00\x00\x00WEBPVP8 rest")
)

func TestIcons(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	must[post.Board](t)(newBoard(s.CreateBoard(ctx, alice, post.NewBoard{ID: "other"})))

	if is := must[[]post.Icon](t)(s.Icons(ctx, "b")); is == nil || len(is) != 0 {
		t.Errorf("icons of a new board = %#v, want []", is)
	}
	i, replaced, err := s.SetIcon(ctx, alice, "b", "codex", pngIcon)
	if err != nil || replaced || i != (post.Icon{Board: "b", Harness: "codex", Type: "image/png", Size: int64(len(pngIcon))}) {
		t.Fatalf("SetIcon = %+v, %v, %v", i, replaced, err)
	}
	// Any harness, a person's and one that hasn't posted included.
	for _, h := range []string{"human", "antigravity", "my-agent"} {
		if _, _, err := s.SetIcon(ctx, alice, "b", h, gifIcon); err != nil {
			t.Fatalf("SetIcon(%s) = %v", h, err)
		}
	}
	// Setting it again replaces it, with the type that its content gives.
	i, replaced, err = s.SetIcon(ctx, alice, "b", "codex", jpegIcon)
	if err != nil || !replaced || i.Type != "image/jpeg" || i.Size != int64(len(jpegIcon)) {
		t.Fatalf("SetIcon again = %+v, %v, %v", i, replaced, err)
	}
	is := must[[]post.Icon](t)(s.Icons(ctx, "b"))
	var hs []string
	for _, i := range is {
		hs = append(hs, i.Harness)
	}
	if !slices.Equal(hs, []string{"antigravity", "codex", "human", "my-agent"}) || is[1].Type != "image/jpeg" || is[0].Board != "b" {
		t.Errorf("icons = %+v", is)
	}
	got, data, err := s.Icon(ctx, "b", "codex")
	if err != nil || got != i || !bytes.Equal(data, jpegIcon) {
		t.Errorf("Icon = %+v, %q, %v", got, data, err)
	}
	// Other boards keep their own.
	if is := must[[]post.Icon](t)(s.Icons(ctx, "other")); len(is) != 0 {
		t.Errorf("icons of another board = %+v", is)
	}
	if _, _, err := s.Icon(ctx, "other", "codex"); post.KindOf(err) != post.NotFound {
		t.Errorf("Icon on another board = %v", err)
	}

	removed, err := s.RemoveIcon(ctx, alice, "b", "codex")
	if err != nil || removed != i {
		t.Errorf("RemoveIcon = %+v, %v", removed, err)
	}
	if _, _, err := s.Icon(ctx, "b", "codex"); post.KindOf(err) != post.NotFound || !strings.Contains(err.Error(), "no icon for codex") {
		t.Errorf("Icon after removing it = %v", err)
	}
	if _, err := s.RemoveIcon(ctx, alice, "b", "codex"); post.KindOf(err) != post.NotFound {
		t.Errorf("RemoveIcon again = %v", err)
	}
	if is := must[[]post.Icon](t)(s.Icons(ctx, "b")); len(is) != 3 {
		t.Errorf("icons after removing one = %+v", is)
	}
}

func TestIconRules(t *testing.T) {
	s, _ := open(t)
	ctx := context.Background()
	if _, _, err := s.SetIcon(ctx, alice, "b", "human", pngIcon); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		who     post.Identity
		board   string
		harness string
		data    []byte
		kind    post.Kind
		msg     string
	}{
		{"agent", codex, "b", "codex", pngIcon, post.Forbidden, "only a person"},
		{"invalid harness", alice, "b", "Codex", pngIcon, post.Invalid, "invalid harness"},
		{"empty", alice, "b", "codex", nil, post.Invalid, "empty"},
		{"too large", alice, "b", "codex", append(slices.Clone(pngIcon), make([]byte, post.MaxIcon)...), post.Invalid, "at most 1 MiB"},
		{"not an image", alice, "b", "codex", []byte("<svg xmlns='http://www.w3.org/2000/svg'/>"), post.Invalid, "isn't a PNG, JPEG, GIF, or WebP image"},
		{"RIFF but not WebP", alice, "b", "codex", []byte("RIFF\x10\x00\x00\x00WAVEfmt "), post.Invalid, "isn't a PNG"},
		{"no board", alice, "nope", "codex", pngIcon, post.NotFound, "no board"},
	} {
		if _, _, err := s.SetIcon(ctx, tc.who, tc.board, tc.harness, tc.data); post.KindOf(err) != tc.kind || !strings.Contains(err.Error(), tc.msg) {
			t.Errorf("%s: SetIcon = %v, want kind %d with %q", tc.name, err, tc.kind, tc.msg)
		}
	}
	for _, tc := range []struct {
		data []byte
		typ  string
	}{{pngIcon, "image/png"}, {jpegIcon, "image/jpeg"}, {gifIcon, "image/gif"}, {[]byte("GIF87a..."), "image/gif"}, {webpIcon, "image/webp"}} {
		if i, _, err := s.SetIcon(ctx, alice, "b", "codex", tc.data); err != nil || i.Type != tc.typ {
			t.Errorf("SetIcon(%q) = %+v, %v, want %s", tc.data, i, err, tc.typ)
		}
	}
	if _, err := s.RemoveIcon(ctx, claude, "b", "human"); post.KindOf(err) != post.Forbidden {
		t.Errorf("an agent's RemoveIcon = %v", err)
	}
	if _, err := s.RemoveIcon(ctx, alice, "b", "Bad"); post.KindOf(err) != post.Invalid {
		t.Errorf("RemoveIcon of an invalid harness = %v", err)
	}
	if _, err := s.RemoveIcon(ctx, alice, "nope", "human"); post.KindOf(err) != post.NotFound {
		t.Errorf("RemoveIcon on no board = %v", err)
	}
	if _, err := s.Icons(ctx, "nope"); post.KindOf(err) != post.NotFound {
		t.Errorf("Icons of no board = %v", err)
	}
	if _, _, err := s.Icon(ctx, "b", "Bad"); post.KindOf(err) != post.Invalid {
		t.Errorf("Icon of an invalid harness = %v", err)
	}
	// What was refused changed nothing.
	if _, data, err := s.Icon(ctx, "b", "human"); err != nil || !bytes.Equal(data, pngIcon) {
		t.Errorf("the person's icon = %q, %v", data, err)
	}
}

// v06Rows fill a database of agora v0.6.0, of testdata/schema-3.sql: a thread
// whose root mentions claude-code, with a reply. Its stored mention names a
// session that the body doesn't, so that a test can tell it from one found
// again in the body.
const v06Rows = `
INSERT INTO boards (id, title, created, harness, session, node, last_n, idem, last_edit) VALUES
	('b', 'Board B', '2026-11-20T09:00:00+01:00', 'human', 'alice', 'laptop', 2, NULL, 0);
INSERT INTO posts (pk, board, n, thread, reply_to, harness, session, node, created, title, body, title_fold, body_fold) VALUES
	(1, 'b', 1, NULL, NULL, 'codex', '019a2b3c-0000', 'gpu', '2026-11-20T10:00:00+01:00',
		'Plan', 'ask @claude-code' || char(10), 'plan', 'ask @claude-code' || char(10)),
	(2, 'b', 2, 1, 1, 'claude-code', '95d3e5c1-1111', 'laptop', '2026-11-20T10:05:00+01:00',
		'', 'done' || char(10), '', 'done' || char(10));
INSERT INTO mentions (post, pos, harness, session) VALUES (1, 0, 'claude-code', '95d3');
INSERT INTO attachments (post, name, type, size, data, added, harness, session, node) VALUES
	(2, 'build.log', 'text/plain', 2, 'ok', '2026-11-20T10:06:00+01:00', 'claude-code', '95d3e5c1-1111', 'laptop');
`

func TestUpgradeFromV06(t *testing.T) {
	dir := testdir.New(t)
	old, fresh := filepath.Join(dir, "old.db"), filepath.Join(dir, "fresh.db")
	schema, err := os.ReadFile(filepath.Join("testdata", "schema-3.sql"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", old)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{string(schema), v06Rows} {
		if _, err := db.Exec(q); err != nil {
			t.Fatalf("%v in\n%s", err, q)
		}
	}
	_ = db.Close()
	c := &clock{t: time.Date(2026, 11, 21, 9, 0, 0, 0, time.FixedZone("CET", 3600))}
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
	if !strings.Contains(got, "version 5\n") || !strings.Contains(got, "table icons\n") {
		t.Errorf("the upgraded database lacks icons:\n%s", got)
	}

	// The upgraded database keeps what agora v0.6.0 wrote, its mentions as
	// they were stored, and takes icons.
	s, err := Open(old, c.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	show := must[post.Show](t)(s.Show(ctx, "b", 1))
	if len(show.Posts) != 2 || !slices.Equal(show.Posts[0].Mentions, []string{"claude-code/95d3"}) || len(show.Posts[1].Attachments) != 1 {
		t.Errorf("show = %+v", show)
	}
	if _, _, err := s.SetIcon(ctx, alice, "b", "codex", pngIcon); err != nil {
		t.Errorf("SetIcon after the upgrade = %v", err)
	}
}
