package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zlcwwhq/agora/internal/post"
)

func TestIconsCommand(t *testing.T) {
	e := threadsEnv(t)
	png := "\x89PNG\r\n\x1a\nrest of a png"
	e.writeTree(t, map[string]string{"logo.jpg": png, "logo.svg": "<svg/>", "empty.png": ""})
	big := filepath.Join(e.dir, "big.png")
	if err := os.WriteFile(big, append([]byte(png), make([]byte, post.MaxIcon)...), 0o644); err != nil {
		t.Fatal(err)
	}

	var is []post.Icon
	if e.json(t, &is, "icons"); is == nil || len(is) != 0 {
		t.Errorf("icons of a new board = %#v, want []", is)
	}
	if out := e.ok(t, "icons"); !strings.Contains(out, "no icons") {
		t.Errorf("icons = %q", out)
	}
	// Only people may, and the file is checked before anything is sent.
	e.mustFail(t, 1, "only a person", "icons", "set", "codex", "logo.jpg")
	e.mustFail(t, 2, "invalid harness", "icons", "set", "Codex", "logo.jpg", "--as", "human/alice")
	e.mustFail(t, 1, "isn't a PNG, JPEG, GIF, or WebP image", "icons", "set", "codex", "logo.svg", "--as", "human/alice")
	e.mustFail(t, 1, "is empty", "icons", "set", "codex", "empty.png", "--as", "human/alice")
	e.mustFail(t, 1, "at most 1 MiB", "icons", "set", "codex", "big.png", "--as", "human/alice")
	e.mustFail(t, 1, "missing.png", "icons", "set", "codex", "missing.png", "--as", "human/alice")
	e.mustFail(t, 1, "not a file", "icons", "set", "codex", ".", "--as", "human/alice")
	for _, args := range [][]string{{"icons", "set", "codex"}, {"icons", "remove"}, {"icons", "get", "codex"}, {"icons", "set", "a", "b", "c"}} {
		e.mustFail(t, 2, "usage: agora icons [set HARNESS FILE | remove HARNESS]", args...)
	}
	if e.json(t, &is, "icons"); len(is) != 0 {
		t.Errorf("refused icons were set: %+v", is)
	}

	// Its type is its content's, whatever its name.
	var i post.Icon
	e.json(t, &i, "icons", "set", "codex", "logo.jpg", "--as", "human/alice")
	if i != (post.Icon{Board: "proj", Harness: "codex", Type: "image/png", Size: int64(len(png))}) {
		t.Errorf("icons set = %+v", i)
	}
	// From standard input, for a harness that hasn't posted.
	var out, errb bytes.Buffer
	gif := "GIF89a and the rest"
	if code := Run(e.env([]string{"icons", "set", "human", "-", "--as", "human/alice"}, strings.NewReader(gif), &out, &errb)); code != 0 ||
		!strings.Contains(out.String(), "set the icon of human on board proj: image/gif, 19 B") || !strings.Contains(out.String(), "/b/proj/icons") {
		t.Errorf("icons set from standard input: exit %d, %q, %q", code, out.String(), errb.String())
	}
	e.json(t, &is, "icons")
	if len(is) != 2 || is[0].Harness != "codex" || is[1].Harness != "human" || is[1].Type != "image/gif" {
		t.Errorf("icons = %+v", is)
	}
	if out := e.ok(t, "icons"); !strings.Contains(out, "human") || !strings.Contains(out, "image/gif, 19 B") {
		t.Errorf("icons = %q", out)
	}

	e.mustFail(t, 1, "only a person", "icons", "remove", "codex")
	e.json(t, &i, "icons", "remove", "codex", "--as", "human/alice")
	if i.Harness != "codex" || i.Type != "image/png" {
		t.Errorf("icons remove = %+v", i)
	}
	e.mustFail(t, 1, "no icon for codex", "icons", "remove", "codex", "--as", "human/alice")
	e.mustFail(t, 2, "invalid harness", "icons", "remove", "Bad", "--as", "human/alice")
}
