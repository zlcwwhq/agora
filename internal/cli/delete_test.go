package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type deletedJSON struct{ ID, Deleted, Board string }

type shownJSON struct {
	Thread threadJSON
	Posts  []postJSON
}

func TestDelete(t *testing.T) {
	m := newMachine(t, "wsl")
	root := m.post("--title", "The secret plan", "-t", "plan", "-m", "The secret is swordfish.")
	mine := m.reply(root, "-t", "oops", "-m", "Another secret: marlin.")
	other := m.reply(root, "--as", "codex/other", "-m", "Not yours to delete.")

	if _, errOut, code := m.run("delete", other); code != 1 || !strings.Contains(errOut, "codex/other@wsl's, not codex/wsl-sess@wsl's") {
		t.Errorf("deleting another author's post: status %d\n%s", code, errOut)
	}
	if _, errOut, code := m.run("delete", "wsl-99"); code != 1 || !strings.Contains(errOut, "unknown post wsl-99") {
		t.Errorf("deleting a post that isn't here: status %d\n%s", code, errOut)
	}

	// A reply keeps its place without its text.
	var d deletedJSON
	m.json(&d, "delete", mine)
	if d.ID != mine || d.Deleted == "" || d.Board == "" {
		t.Fatalf("delete --json: %+v", d)
	}
	file := filepath.Join(m.root, "posts", "wsl", strings.TrimPrefix(mine, "wsl-")+".md")
	stub, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stub), "marlin") || !strings.Contains(string(stub), `deleted: "`+d.Deleted+`"`) {
		t.Fatalf("the reply's file after delete:\n%s", stub)
	}
	var shown shownJSON
	m.json(&shown, "show", root)
	if len(shown.Posts) != 3 || shown.Posts[1].ID != mine || shown.Posts[1].Deleted != d.Deleted ||
		shown.Posts[1].Body != "" || len(shown.Posts[1].Tags) != 0 || shown.Posts[1].Author != "codex/wsl-sess@wsl" {
		t.Fatalf("show after deleting a reply: %+v", shown.Posts)
	}
	if shown.Thread.Replies != 1 || shown.Thread.Deleted != "" || shown.Thread.Title != "The secret plan" {
		t.Errorf("the thread after deleting a reply: %+v", shown.Thread)
	}
	var found []threadJSON
	if m.json(&found, "search", "marlin"); len(found) != 0 {
		t.Errorf("search finds the deleted reply: %+v", found)
	}
	if out := m.ok("tags"); strings.Contains(out, "oops") {
		t.Errorf("tags keeps the deleted reply's tag:\n%s", out)
	}
	if _, errOut, code := m.run("reply", mine, "-m", "To a deleted post."); code != 1 || !strings.Contains(errOut, "can't reply to "+mine+", which is deleted") {
		t.Errorf("replying to a deleted post: status %d\n%s", code, errOut)
	}

	// Deleting it again changes nothing.
	var again deletedJSON
	m.json(&again, "delete", mine)
	if again.Deleted != d.Deleted {
		t.Errorf("deleting again reports %q, want the first time, %q", again.Deleted, d.Deleted)
	}
	if after, _ := os.ReadFile(file); string(after) != string(stub) {
		t.Error("deleting again changed the stub")
	}
	if _, errOut, code := m.run("delete", mine); code != 0 || !strings.Contains(errOut, "already deleted") {
		t.Errorf("deleting again: status %d\n%s", code, errOut)
	}

	// A thread whose root is deleted stays while a reply is left.
	m.ok("delete", root)
	var list []threadJSON
	m.json(&list, "list")
	if len(list) != 1 || list[0].ID != root || list[0].Title != "" || list[0].Deleted == "" || list[0].Replies != 1 {
		t.Fatalf("list after deleting the root: %+v", list)
	}
	if out := m.ok("list"); strings.Join(strings.Fields(strings.SplitN(out, "\n", 2)[0]), " ") != root+" (deleted)" {
		t.Errorf("list doesn't show the deleted root:\n%s", out)
	}
	if m.json(&found, "search", "swordfish"); len(found) != 0 {
		t.Errorf("search finds the deleted root: %+v", found)
	}

	// A thread whose every post is deleted is left out, but can be shown.
	m.ok("delete", other, "--as", "codex/other")
	if m.json(&list, "list"); len(list) != 0 {
		t.Errorf("list shows a thread whose every post is deleted: %+v", list)
	}
	m.json(&shown, "show", root)
	for _, p := range shown.Posts {
		if p.Deleted == "" {
			t.Errorf("%s isn't deleted", p.ID)
		}
	}
	if out := m.ok("show", root); strings.Contains(out, "swordfish") || !strings.Contains(out, "deleted ") ||
		strings.Contains(strings.SplitN(out, "\n\n", 2)[0], "active") {
		t.Errorf("show of a deleted thread, which has no activity left:\n%s", out)
	}
	if shown.Thread.LastActivity != "" || shown.Thread.Replies != 0 {
		t.Errorf("a thread whose every post is deleted: %+v", shown.Thread)
	}
}

func TestDeleteOnAnotherMachine(t *testing.T) {
	mac, wsl := newMachine(t, "mac"), newMachine(t, "wsl")
	id := mac.post("--title", "From mac", "-m", "Text from mac.")
	copyPosts(t, mac, wsl)

	// Only the machine that wrote a post deletes it, whoever asks.
	if _, errOut, code := wsl.run("delete", id, "--as", "codex/mac-session"); code != 1 || !strings.Contains(errOut, "machine mac wrote it") {
		t.Errorf("deleting another machine's post: status %d\n%s", code, errOut)
	}

	// The stub replaces the post on the other machines, as Mutagen brings it.
	mac.ok("delete", id)
	rel := filepath.Join("posts", "mac", "1.md")
	stub, err := os.ReadFile(filepath.Join(mac.root, rel))
	if err != nil {
		t.Fatal(err)
	}
	var before []threadJSON
	if wsl.json(&before, "search", "Text"); len(before) != 1 {
		t.Fatalf("wsl doesn't find the post before the stub arrives: %+v", before)
	}
	if err := os.WriteFile(filepath.Join(wsl.root, rel), stub, 0o644); err != nil {
		t.Fatal(err)
	}
	var shown shownJSON
	wsl.json(&shown, "show", id)
	if len(shown.Posts) != 1 || shown.Posts[0].Deleted == "" || shown.Thread.Title != "" {
		t.Errorf("wsl shows the deleted post as %+v", shown)
	}
	var found []threadJSON
	if wsl.json(&found, "search", "Text"); len(found) != 0 {
		t.Errorf("wsl still finds the deleted post: %+v", found)
	}
}

func TestUnreadSkipsDeletedPosts(t *testing.T) {
	m := newMachine(t, "wsl")
	m.ok("unread", "--as", "codex/reader")
	gone := m.post("--title", "Posted by mistake", "-m", "Oops.")
	m.ok("delete", gone)
	kept := m.post("--title", "Meant to post", "-m", "Yes.")

	var unread struct{ Posts []postJSON }
	m.json(&unread, "unread", "--as", "codex/reader")
	if len(unread.Posts) != 1 || unread.Posts[0].ID != kept {
		t.Fatalf("unread shows %+v, want only %s", unread.Posts, kept)
	}
	// The deleted post is marked seen, so the cursor's contig passes it.
	files, err := filepath.Glob(filepath.Join(m.env["XDG_STATE_HOME"], "agora", "*", "cursors", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var c struct {
			Reader string
			Nodes  map[string]struct{ Contig int }
		}
		if err := json.Unmarshal(data, &c); err != nil {
			t.Fatal(err)
		}
		if c.Reader == "codex/reader" && c.Nodes["wsl"].Contig != 2 {
			t.Errorf("the reader's cursor: %s", data)
		}
	}
}
