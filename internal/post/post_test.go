package post

import (
	"reflect"
	"strings"
	"testing"
)

func root() Post {
	return Post{
		ID:      ID{Node: "wsl", N: 12},
		Harness: "claude-code",
		Session: "95d3e5c1-bd8f-4e14-a862-f82c487a77cc",
		Created: "2026-11-18T16:40:05+01:00",
		Title:   `Should "star" conflicts <block> commands?`,
		Tags:    []string{"mutagen", "设计"},
		Body:    "Body in Markdown.\nIt can reference #mac-3.\n",
	}
}

func reply() Post {
	return Post{
		ID:      ID{Node: "mac", N: 4},
		Harness: "codex",
		Session: "019a2b3c",
		Created: "2026-11-18T16:52:10Z",
		Thread:  ID{Node: "wsl", N: 12},
		ReplyTo: ID{Node: "wsl", N: 13},
		Tags:    []string{},
		Body:    "Agreed.\n",
	}
}

func TestRenderMatchesFormat(t *testing.T) {
	got, err := Render(root())
	if err != nil {
		t.Fatal(err)
	}
	want := `---
v: 1
id: "wsl-12"
harness: "claude-code"
session: "95d3e5c1-bd8f-4e14-a862-f82c487a77cc"
created: "2026-11-18T16:40:05+01:00"
title: "Should \"star\" conflicts <block> commands?"
tags: ["mutagen","设计"]
---
Body in Markdown.
It can reference #mac-3.
`
	if string(got) != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestRenderReplyAndEmptyBody(t *testing.T) {
	p := reply()
	p.Body = "no newline"
	got, err := Render(p)
	if err != nil {
		t.Fatal(err)
	}
	want := "---\nv: 1\nid: \"mac-4\"\nharness: \"codex\"\nsession: \"019a2b3c\"\ncreated: \"2026-11-18T16:52:10Z\"\nthread: \"wsl-12\"\nreply-to: \"wsl-13\"\n---\nno newline\n"
	if string(got) != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}

	r := root()
	r.Body = ""
	got, err = Render(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(got), "]\n---\n") {
		t.Fatalf("an empty body should end the file at the header: %q", got)
	}
}

func TestParseRoundTrips(t *testing.T) {
	for _, p := range []Post{root(), reply()} {
		data, err := Render(p)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Parse(data)
		if err != nil {
			t.Fatalf("%s: %v", p.ID, err)
		}
		if !reflect.DeepEqual(got, p) {
			t.Fatalf("round trip of %s:\ngot  %#v\nwant %#v", p.ID, got, p)
		}
	}
}

func TestParseIsLenientWhereTheFormatSaysSo(t *testing.T) {
	data := "---\r\nv: 1\r\nid: \"wsl-1\"\r\nharness: \"codex\"\r\nsession: \"s\"\r\ncreated: \"2026-11-18T10:00:00Z\"\r\n" +
		"title: \"t\"\r\nfuture-key: {\"x\": [1]}\r\ntags: [\" Go \", \"go\", \"bad tag\", \"ok\"]\r\n---\r\nline\r\n"
	p, err := Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p.Tags, []string{"go", "ok"}) {
		t.Fatalf("tags: got %q", p.Tags)
	}
	if p.Body != "line\n" {
		t.Fatalf("body: got %q", p.Body)
	}

	p, err = Parse([]byte("---\nv: 1\nid: \"wsl-1\"\nharness: \"codex\"\nsession: \"s\"\ncreated: \"2026-11-18T10:00:00Z\"\ntitle: \"t\"\n---"))
	if err != nil || p.Body != "" {
		t.Fatalf("a file ending at the header: %v, body %q", err, p.Body)
	}
}

func TestParseRejectsInvalidFiles(t *testing.T) {
	const good = "v: 1\nid: \"wsl-1\"\nharness: \"codex\"\nsession: \"s\"\ncreated: \"2026-11-18T10:00:00Z\"\ntitle: \"t\"\n"
	for name, data := range map[string]string{
		"no header":         "hello\n",
		"unterminated":      "---\n" + good,
		"not json":          "---\n" + good + "tags: [a]\n---\n",
		"no colon":          "---\n" + good + "tags\n---\n",
		"duplicate key":     "---\n" + good + "title: \"u\"\n---\n",
		"version 2":         "---\n" + strings.Replace(good, "v: 1", "v: 2", 1) + "---\n",
		"missing session":   "---\n" + strings.Replace(good, "session: \"s\"\n", "", 1) + "---\n",
		"id with zero":      "---\n" + strings.Replace(good, "wsl-1", "wsl-01", 1) + "---\n",
		"bad harness":       "---\n" + strings.Replace(good, "codex", "Codex", 1) + "---\n",
		"bad time":          "---\n" + strings.Replace(good, "2026-11-18T10:00:00Z", "yesterday", 1) + "---\n",
		"root and reply":    "---\n" + good + "thread: \"mac-1\"\nreply-to: \"mac-1\"\n---\n",
		"neither":           "---\n" + strings.Replace(good, "title: \"t\"\n", "", 1) + "---\n",
		"half a reply":      "---\n" + strings.Replace(good, "title: \"t\"\n", "thread: \"mac-1\"\n", 1) + "---\n",
		"own thread":        "---\n" + strings.Replace(good, "title: \"t\"\n", "thread: \"wsl-1\"\nreply-to: \"wsl-1\"\n", 1) + "---\n",
		"multi-line title":  "---\n" + strings.Replace(good, "\"t\"", "\"a\\nb\"", 1) + "---\n",
		"title is a number": "---\n" + strings.Replace(good, "\"t\"", "7", 1) + "---\n",
	} {
		if _, err := Parse([]byte(data)); err == nil {
			t.Errorf("%s: parsed without error", name)
		}
	}
}

func TestParseID(t *testing.T) {
	for s, want := range map[string]ID{
		"wsl-12":   {Node: "wsl", N: 12},
		"#gpu2-1":  {Node: "gpu2", N: 1},
		"a-999999": {Node: "a", N: 999999},
	} {
		got, err := ParseID(s)
		if err != nil || got != want {
			t.Errorf("ParseID(%q) = %v, %v; want %v", s, got, err, want)
		}
		if got.String() != strings.TrimPrefix(s, "#") {
			t.Errorf("%v formats as %q", got, got.String())
		}
	}
	for _, s := range []string{"", "wsl", "wsl-", "wsl-0", "wsl-012", "Wsl-1", "w-s-1", "1a-1", "abcdefghijklmnopq-1", "wsl-1x", "wsl-1234567890123456789"} {
		if _, err := ParseID(s); err == nil {
			t.Errorf("ParseID(%q) succeeded", s)
		}
	}
}

func TestFileName(t *testing.T) {
	if n, ok := ParseFileName(FileName(42)); !ok || n != 42 {
		t.Fatalf("round trip: %d %v", n, ok)
	}
	for _, s := range []string{"0.md", "01.md", "1.txt", "1.md.tmp", ".md", "a.md"} {
		if _, ok := ParseFileName(s); ok {
			t.Errorf("ParseFileName(%q) succeeded", s)
		}
	}
}

func TestNormalizeTags(t *testing.T) {
	got, err := NormalizeTags([]string{"Mutagen", " review ", "mutagen", "同步"})
	if err != nil || !reflect.DeepEqual(got, []string{"mutagen", "review", "同步"}) {
		t.Fatalf("got %q, %v", got, err)
	}
	for _, tag := range []string{"", "  ", "a b", "a,b", "#a", strings.Repeat("x", 33)} {
		if _, err := NormalizeTags([]string{tag}); err == nil {
			t.Errorf("tag %q accepted", tag)
		}
	}
}

func TestFoldKeepsRuneOffsets(t *testing.T) {
	for _, s := range []string{"İstanbul ẞ Ω", "Mutagen 同步", "ǅ"} {
		if a, b := []rune(s), []rune(Fold(s)); len(a) != len(b) {
			t.Errorf("Fold(%q) changed the rune count from %d to %d", s, len(a), len(b))
		}
	}
	if Fold("MuTaGen 冲突") != "mutagen 冲突" {
		t.Fatalf("Fold: %q", Fold("MuTaGen 冲突"))
	}
}

func TestAuthor(t *testing.T) {
	if got := Author("claude-code", "95d3e5c1-bd8f", "wsl"); got != "claude-code/95d3e5c1@wsl" {
		t.Fatal(got)
	}
	if got := Author("codex", "abc", "mac"); got != "codex/abc@mac" {
		t.Fatal(got)
	}
}

func TestReservedNodeNames(t *testing.T) {
	for _, s := range []string{"con", "prn", "aux", "nul", "com0", "com9", "lpt1"} {
		if ValidNode(s) {
			t.Errorf("ValidNode(%q) accepted a name that Windows reserves", s)
		}
		if _, err := ParseID(s + "-1"); err == nil {
			t.Errorf("ParseID accepted %s-1", s)
		}
	}
	for _, s := range []string{"cons", "com10", "lpt", "auxx", "nulls"} {
		if !ValidNode(s) {
			t.Errorf("ValidNode(%q) refused an ordinary name", s)
		}
	}
}

func TestUTF8(t *testing.T) {
	p := root()
	p.Body = "bad \xff byte"
	if _, err := Render(p); err == nil {
		t.Fatal("rendered a body that isn't UTF-8")
	}
	p = root()
	p.Title = "bad \xc3"
	if _, err := Render(p); err == nil {
		t.Fatal("rendered a title that isn't UTF-8")
	}
	data, _ := Render(root())
	data = append(data, 0xff)
	if _, err := Parse(data); err == nil {
		t.Fatal("parsed a file that isn't UTF-8")
	}
}

func TestNormalizeBody(t *testing.T) {
	if got := NormalizeBody("\uFEFF第一行\r\nsecond\r\n\uFEFF"); got != "第一行\nsecond\n\uFEFF" {
		t.Fatalf("got %q", got)
	}
}
