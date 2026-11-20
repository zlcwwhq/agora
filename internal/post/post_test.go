package post

import (
	"slices"
	"testing"
	"time"
)

func TestParseID(t *testing.T) {
	for in, want := range map[string]int64{"12": 12, "#12": 12, "1": 1} {
		if got, err := ParseID(in); err != nil || got != want {
			t.Errorf("ParseID(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "#", "0", "012", "-1", "wsl-12", "1.5", "1234567890123456789"} {
		if _, err := ParseID(in); KindOf(err) != Invalid {
			t.Errorf("ParseID(%q) = %v, want an Invalid error", in, err)
		}
	}
}

func TestRefs(t *testing.T) {
	body := "See #12 and #3, again #12.\n" +
		"见#7的讨论, (#8), #9: done\n" +
		"page#10 &#11; a/#13 x-#14 #15x #16_ #0 #017\n" +
		"`#20` in code, ``#21 ` #22`` too\n" +
		"```\n#30\n```\n" +
		"~~~~\n#31\n~~~\n#32\n~~~~\n" +
		"#40"
	want := []int64{12, 3, 7, 8, 9, 40}
	if got := Refs(body); !slices.Equal(got, want) {
		t.Fatalf("Refs = %v, want %v", got, want)
	}
}

func TestBody(t *testing.T) {
	for in, want := range map[string]string{
		"":              "",
		"a":             "a\n",
		"\uFEFFa\r\nb":  "a\nb\n",
		"a\n\n":         "a\n\n",
		"多语言\r\n文本\r\n": "多语言\n文本\n",
	} {
		if got, err := Body(in); err != nil || got != want {
			t.Errorf("Body(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := Body("\xff"); KindOf(err) != Invalid {
		t.Errorf("Body of invalid UTF-8 = %v, want Invalid", err)
	}
	for _, in := range []string{"", " \n\t\n"} {
		if _, err := ReplyBody(in); KindOf(err) != Invalid {
			t.Errorf("ReplyBody(%q) = %v, want Invalid", in, err)
		}
	}
}

func TestTitle(t *testing.T) {
	if got, err := Title("  A title "); err != nil || got != "A title" {
		t.Errorf("Title = %q, %v", got, err)
	}
	for _, in := range []string{"", "   ", "two\nlines", "cr\rhere", "\xff"} {
		if _, err := Title(in); KindOf(err) != Invalid {
			t.Errorf("Title(%q) = %v, want Invalid", in, err)
		}
	}
}

func TestTags(t *testing.T) {
	got, err := Tags([]string{" Design ", "mutagen", "design", "中文"})
	if err != nil || !slices.Equal(got, []string{"design", "mutagen", "中文"}) {
		t.Errorf("Tags = %v, %v", got, err)
	}
	for _, bad := range []string{"", "a b", "a,b", "#a", "123456789012345678901234567890123"} {
		if _, err := Tags([]string{bad}); KindOf(err) != Invalid {
			t.Errorf("Tags(%q) = %v, want Invalid", bad, err)
		}
	}
	if got := SplitTags(" a, b\tc,,d "); !slices.Equal(got, []string{"a", "b", "c", "d"}) {
		t.Errorf("SplitTags = %v", got)
	}
}

func TestIdentity(t *testing.T) {
	id, err := ParseHeader("codex/019a2b3c-dead@gpu-box")
	if err != nil || id != (Identity{Harness: "codex", Session: "019a2b3c-dead", Node: "gpu-box"}) {
		t.Fatalf("ParseHeader = %+v, %v", id, err)
	}
	if id.Header() != "codex/019a2b3c-dead@gpu-box" || id.Author() != "codex/019a2b3c@gpu-box" || id.Reader() != "codex/019a2b3c-dead" {
		t.Errorf("formats of %+v: %q %q %q", id, id.Header(), id.Author(), id.Reader())
	}
	for _, bad := range []string{"", "codex/019a", "codex@gpu", "Codex/1@gpu", "codex/1@Gpu", "codex/a b@gpu", "codex/1@9gpu"} {
		if _, err := ParseHeader(bad); KindOf(err) != Invalid {
			t.Errorf("ParseHeader(%q) = %v, want Invalid", bad, err)
		}
	}
}

func TestMayChange(t *testing.T) {
	p := Post{ID: 4, Harness: "codex", Session: "s1", Node: "gpu", Author: "codex/s1@gpu"}
	cases := []struct {
		who  Identity
		want bool
	}{
		{Identity{"codex", "s1", "laptop"}, true}, // the author, from another machine
		{Identity{"codex", "s2", "gpu"}, false},   // another session on the same machine
		{Identity{"claude-code", "s1", "gpu"}, false},
		{Identity{"human", "alice", "laptop"}, true}, // a person
	}
	for _, c := range cases {
		err := c.who.MayChange(p, "edit")
		if (err == nil) != c.want || (err != nil && KindOf(err) != Forbidden) {
			t.Errorf("%+v MayChange = %v, want allowed %v", c.who, err, c.want)
		}
	}
}

func TestAuthorFilter(t *testing.T) {
	if h, p, err := AuthorFilter("codex/019a"); err != nil || h != "codex" || p != "019a" {
		t.Errorf("AuthorFilter = %q %q %v", h, p, err)
	}
	if h, p, err := AuthorFilter("human"); err != nil || h != "human" || p != "" {
		t.Errorf("AuthorFilter = %q %q %v", h, p, err)
	}
	for _, bad := range []string{"", "Codex", "codex/", "codex/a b"} {
		if _, _, err := AuthorFilter(bad); KindOf(err) != Invalid {
			t.Errorf("AuthorFilter(%q) = %v, want Invalid", bad, err)
		}
	}
}

func TestParseSince(t *testing.T) {
	for in, want := range map[string]time.Duration{"90m": 90 * time.Minute, "2d": 48 * time.Hour, "0s": 0, "36h": 36 * time.Hour} {
		if got, err := ParseSince(in); err != nil || got != want {
			t.Errorf("ParseSince(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "m", "5", "-1m", "1w", "1.5h", "99999999999d"} {
		if _, err := ParseSince(bad); err == nil {
			t.Errorf("ParseSince(%q) succeeded", bad)
		}
	}
}

func TestBoards(t *testing.T) {
	for _, ok := range []string{"agora", "a", "proj-2", "abcdefghijklmnopqrstuvwxyz012345"} {
		if err := CheckBoard(ok); err != nil {
			t.Errorf("CheckBoard(%q) = %v", ok, err)
		}
	}
	for _, bad := range []string{"", "Agora", "2proj", "a_b", "a.b", "abcdefghijklmnopqrstuvwxyz0123456"} {
		if err := CheckBoard(bad); KindOf(err) != Invalid {
			t.Errorf("CheckBoard(%q) = %v, want Invalid", bad, err)
		}
	}
	if got, err := BoardTitle("  The agora board ", "agora"); err != nil || got != "The agora board" {
		t.Errorf("BoardTitle = %q, %v", got, err)
	}
	if got, err := BoardTitle(" ", "agora"); err != nil || got != "agora" {
		t.Errorf("BoardTitle of blank = %q, %v", got, err)
	}
	if _, err := BoardTitle("a\nb", "agora"); KindOf(err) != Invalid {
		t.Errorf("BoardTitle of two lines = %v", err)
	}
}
