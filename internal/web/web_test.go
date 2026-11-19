package web

import (
	"net"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zlcwwhq/agora/internal/post"
)

func renderString(t *testing.T, body string, missing ...string) string {
	t.Helper()
	gone := map[post.ID]bool{}
	for _, m := range missing {
		id, err := post.ParseID(m)
		if err != nil {
			t.Fatal(err)
		}
		gone[id] = true
	}
	html, err := render(body, func(id post.ID) bool { return !gone[id] })
	if err != nil {
		t.Fatal(err)
	}
	return string(html)
}

func TestRenderReferences(t *testing.T) {
	for _, c := range []struct {
		body string
		refs []string
	}{
		{"see #wsl-12 and #mac-3.", []string{"wsl-12", "mac-3"}},
		{"#wsl-12 starts a line", []string{"wsl-12"}},
		{"见#wsl-12的讨论", []string{"wsl-12"}},
		{"**#wsl-12**", []string{"wsl-12"}},
		{"page#sec-2 and &#x-1 and a#wsl-1 and #wsl-1x", nil},
		{"`#wsl-12` in code", nil},
		{"```\n#wsl-12\n```", nil},
		{"    #wsl-12 indented code", nil},
		{"[see #wsl-12](https://example.com)", nil},
		{"# Heading about #wsl-4", []string{"wsl-4"}},
	} {
		html := renderString(t, c.body)
		var got []string
		for _, part := range strings.Split(html, `class="ref"`)[1:] {
			_, rest, _ := strings.Cut(part, ">#")
			id, _, _ := strings.Cut(rest, "<")
			got = append(got, id)
		}
		if !reflect.DeepEqual(got, c.refs) {
			t.Errorf("%q: links %v, want %v\n%s", c.body, got, c.refs, html)
		}
		for _, id := range c.refs {
			if !strings.Contains(html, `href="/t/`+id+`#`+id+`"`) {
				t.Errorf("%q: no link to %s in its thread\n%s", c.body, id, html)
			}
		}
	}
}

func TestRenderReferenceInLink(t *testing.T) {
	html := renderString(t, "[see #wsl-12](https://example.com) and [#wsl-3](/t/wsl-3)")
	for _, want := range []string{`<a href="https://example.com">see #wsl-12</a>`, `<a href="/t/wsl-3">#wsl-3</a>`} {
		if !strings.Contains(html, want) {
			t.Errorf("no %s in\n%s", want, html)
		}
	}
}

func TestRenderMissingReference(t *testing.T) {
	html := renderString(t, "after #gpu-9 and #wsl-1", "gpu-9")
	if !strings.Contains(html, `<a href="/t/gpu-9#gpu-9" class="ref missing"`) {
		t.Errorf("a reference to a post that hasn't arrived isn't marked:\n%s", html)
	}
	if !strings.Contains(html, `class="ref">#wsl-1<`) {
		t.Errorf("a reference to a post that arrived is marked:\n%s", html)
	}
}

func TestRenderIsSafe(t *testing.T) {
	html := renderString(t, strings.Join([]string{
		`<script>alert(1)</script>`,
		``,
		`<img src=x onerror=alert(1)>`,
		``,
		`[js](javascript:alert(1)) [vb](VBScript:x) [file](file:///etc/passwd) [ok](https://example.com/a?b=1) [mail](mailto:a@example.com) [here](/t/wsl-1)`,
		``,
		`<ftp://example.com/x> ![diagram](https://example.com/d.png) [![nested](https://example.com/i.png)](https://example.com)`,
	}, "\n"))
	for _, bad := range []string{"<script", "<img", "onerror", "javascript:", "VBScript:", "file:", `href="ftp:`, `href=""`} {
		if strings.Contains(html, bad) {
			t.Errorf("rendered %q:\n%s", bad, html)
		}
	}
	for _, good := range []string{
		`<a href="https://example.com/a?b=1">ok</a>`,
		`<a href="mailto:a@example.com">mail</a>`,
		`<a href="/t/wsl-1">here</a>`,
		`<a href="https://example.com/d.png" class="image">diagram</a>`,
		`ftp://example.com/x`,
		`<a href="https://example.com">nested</a>`,
	} {
		if !strings.Contains(html, good) {
			t.Errorf("didn't render %s:\n%s", good, html)
		}
	}
}

func TestRenderMarkdown(t *testing.T) {
	html := renderString(t, "| a | b | c |\n|---|:-:|--:|\n| 1 | 2 | 3 |\n\n- [x] done\n\n~~old~~ https://example.com\n")
	for _, want := range []string{
		"<table>", "<td>1</td>", `<td align="center">2</td>`, `<td align="right">3</td>`,
		`type="checkbox"`, "<del>old</del>", `<a href="https://example.com">`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("no %s in\n%s", want, html)
		}
	}
	// The content security policy blocks style attributes.
	if strings.Contains(html, " style=") {
		t.Errorf("rendered a style attribute:\n%s", html)
	}
}

func TestTerms(t *testing.T) {
	for q, want := range map[string][]string{
		`conflict star`:             {"conflict", "star"},
		`"star edges"  warn`:        {"star edges", "warn"},
		`冲突`:                        {"冲突"},
		`"" "  " x`:                 {"x"},
		`  `:                        nil,
		`unclosed "quote here`:      {"unclosed", "quote here"},
		"tab\tseparated\nand lines": {"tab", "separated", "and", "lines"},
	} {
		if got := terms(q); !reflect.DeepEqual(got, want) {
			t.Errorf("terms(%q) = %q, want %q", q, got, want)
		}
	}
}

func TestHighlight(t *testing.T) {
	got := highlight("Star 冲突 STAR star", []string{"star", "冲突"})
	want := []piece{{"Star", true}, {" ", false}, {"冲突", true}, {" ", false}, {"STAR", true}, {" ", false}, {"star", true}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("highlight = %+v, want %+v", got, want)
	}
}

func TestLocalHost(t *testing.T) {
	for host, want := range map[string]bool{
		"localhost:7777": true, "LOCALHOST": true, "127.0.0.1:80": true, "127.1.2.3": true,
		"[::1]:7777": true, "::1": true, "evil.example": false, "evil.example:7777": false,
		"localhost.evil.example": false, "10.0.0.1:7777": false, "": false,
	} {
		if got := localHost(host); got != want {
			t.Errorf("localHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestIsLoopback(t *testing.T) {
	for addr, want := range map[string]bool{"127.0.0.1": true, "::1": true, "0.0.0.0": false, "::": false, "192.168.1.2": false} {
		if got := isLoopback(&net.TCPAddr{IP: net.ParseIP(addr), Port: 7777}); got != want {
			t.Errorf("isLoopback(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestParseQuery(t *testing.T) {
	q, err := parseQuery(url.Values{"q": {" star "}, "t": {"Mutagen", "design", "mutagen"}, "author": {"codex/019a"}, "n": {"5"}, "in": {"title"}})
	if err != nil {
		t.Fatal(err)
	}
	want := query{Q: "star", In: "title", Author: "codex/019a", Tags: []string{"mutagen", "design"}, N: 5}
	if !reflect.DeepEqual(q, want) {
		t.Errorf("parseQuery = %+v, want %+v", q, want)
	}
	if got := q.URL(); got != "/?author=codex%2F019a&in=title&n=5&q=star&t=mutagen&t=design" {
		t.Errorf("URL = %s", got)
	}
	if got := q.ToggleTag("mutagen"); got != "/?author=codex%2F019a&in=title&q=star&t=design" {
		t.Errorf("ToggleTag = %s", got)
	}
	for _, bad := range []url.Values{
		{"t": {"two words"}}, {"t": {"a,b"}}, {"author": {"Not A Harness"}},
		{"n": {"0"}}, {"n": {"x"}}, {"in": {"tags"}},
	} {
		if _, err := parseQuery(bad); err == nil {
			t.Errorf("parseQuery(%v) succeeded", bad)
		}
	}
}

func TestRelative(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	for d, want := range map[time.Duration]string{
		30 * time.Second: "just now", -30 * time.Second: "just now", time.Minute: "1 minute ago",
		5 * time.Minute: "5 minutes ago", 3 * time.Hour: "3 hours ago", 30 * time.Hour: "yesterday",
		4 * 24 * time.Hour: "4 days ago", 90 * 24 * time.Hour: "Aug 20, 2026", -2 * time.Hour: "Nov 18, 2026",
	} {
		if got := relative(now.Add(-d), now); got != want {
			t.Errorf("relative(now - %v) = %q, want %q", d, got, want)
		}
	}
}

func TestIcon(t *testing.T) {
	for harness, want := range map[string]string{
		"claude-code": `class="hicon claude-code"`,
		"codex":       `class="hicon codex"`,
		"gemini-cli":  `>G</text>`,
		"":            `>?</text>`,
		"<x>":         `aria-label="&lt;x&gt;"`,
	} {
		if got := string(icon(harness)); !strings.Contains(got, want) {
			t.Errorf("icon(%q) = %s, want %s in it", harness, got, want)
		}
	}
	if strings.Contains(string(icon("<x>")), "<x>") {
		t.Error("icon doesn't escape the harness")
	}
}

func TestIconAntigravity(t *testing.T) {
	got := string(icon("antigravity"))
	if !strings.Contains(got, `class="hicon antigravity"`) || strings.Contains(got, "<text") {
		t.Errorf("icon(%q) = %s, want Antigravity's own icon", "antigravity", got)
	}
}

func TestIconHuman(t *testing.T) {
	got := string(icon("human"))
	if !strings.Contains(got, `class="hicon human"`) || strings.Contains(got, "<text") {
		t.Errorf("icon(%q) = %s, want a person's own icon", "human", got)
	}
}

func TestSplitTags(t *testing.T) {
	for in, want := range map[string][]string{
		"":                    nil,
		"  ":                  nil,
		"design":              {"design"},
		"People, design  web": {"People", "design", "web"},
		"a,b,,c\n d\t":        {"a", "b", "c", "d"},
		"a#b":                 {"a#b"},
	} {
		if got := splitTags(in); !reflect.DeepEqual(got, want) && (len(got) != 0 || len(want) != 0) {
			t.Errorf("splitTags(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormPath(t *testing.T) {
	for path, want := range map[string]bool{
		"/new": true, "/t/wsl-12": true, "/t/x": true,
		"/": false, "/tags": false, "/t/": false, "/t/wsl-12/more": false, "/new/": false, "/static/style.css": false,
	} {
		if got := formPath(path); got != want {
			t.Errorf("formPath(%q) = %v, want %v", path, got, want)
		}
	}
}
