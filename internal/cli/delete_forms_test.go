package cli

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeDelete(t *testing.T) {
	m := newMachine(t, "wsl")
	root := m.post("--as", "human/alice", "--title", "Alice asks", "-m", "A question.")
	agents := m.reply(root, "-m", "An agent's answer.")
	mine := m.reply(root, "--as", "human/alice", "-m", "Oops, wrong thread.")
	s, stop := m.startServe("--as", "human/alice")
	base := strings.TrimSuffix(s.URL, "/")

	// The person's posts, and only theirs, have a form that deletes them,
	// behind a second button.
	threadPage := page(t, base+"/t/"+root)
	for _, id := range []string{root, mine} {
		if !strings.Contains(threadPage, `action="/t/`+id+`/delete"`) {
			t.Errorf("/t/%s has no form that deletes %s", root, id)
		}
	}
	if strings.Contains(threadPage, `action="/t/`+agents+`/delete"`) || !strings.Contains(threadPage, "<summary>Delete</summary>") {
		t.Errorf("/t/%s offers to delete the agent's post, or has no confirmation", root)
	}
	tok := token(t, threadPage)

	file := filepath.Join(m.root, "posts", "wsl", strings.TrimPrefix(mine, "wsl-")+".md")
	original, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name   string
		path   string
		fields url.Values
		status int
	}{
		{"no token", "/t/" + mine + "/delete", url.Values{}, http.StatusForbidden},
		{"another author's post", "/t/" + agents + "/delete", url.Values{"token": {tok}}, http.StatusForbidden},
		{"a post that isn't here", "/t/wsl-99/delete", url.Values{"token": {tok}}, http.StatusNotFound},
		{"not an id", "/t/nope/delete", url.Values{"token": {tok}}, http.StatusBadRequest},
	} {
		if resp, body := submit(t, base+c.path, c.fields, base, ""); resp.StatusCode != c.status {
			t.Errorf("%s: POST %s: status %d, want %d\n%s", c.name, c.path, resp.StatusCode, c.status, body)
		}
	}
	if resp, body := get(t, http.MethodGet, base+"/t/"+mine+"/delete"); resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") != "POST" {
		t.Errorf("GET /t/%s/delete: status %d, Allow %q\n%s", mine, resp.StatusCode, resp.Header.Get("Allow"), body)
	}
	if after, _ := os.ReadFile(file); string(after) != string(original) {
		t.Fatal("a refused delete changed the post")
	}

	// Deleting leads back to the post's place, which shows it deleted.
	for range 2 {
		resp, _ := submit(t, base+"/t/"+mine+"/delete", url.Values{"token": {tok}}, base, "")
		if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || loc != "/t/"+mine+"#"+mine {
			t.Fatalf("POST /t/%s/delete: status %d, Location %q", mine, resp.StatusCode, loc)
		}
	}
	var shown shownJSON
	m.json(&shown, "show", mine)
	if shown.Posts[2].ID != mine || shown.Posts[2].Deleted == "" || shown.Posts[2].Body != "" {
		t.Fatalf("after the form deleted %s: %+v", mine, shown.Posts[2])
	}
	deletedPage := page(t, base+"/t/"+mine)
	if strings.Contains(deletedPage, "Oops, wrong thread.") || !strings.Contains(deletedPage, "Deleted by its author") ||
		strings.Contains(deletedPage, `href="/t/`+mine+`#reply"`) || strings.Contains(deletedPage, `action="/t/`+mine+`/delete"`) {
		t.Errorf("/t/%s after deleting it:\n%s", mine, deletedPage)
	}
	// A deleted post can't be replied to.
	if strings.Contains(deletedPage, `id="reply"`) {
		t.Errorf("/t/%s offers to reply to the deleted post", mine)
	}
	resp, body := submit(t, base+"/t/"+mine, url.Values{"token": {tok}, "body": {"To a deleted post."}}, base, "")
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, "Can&#39;t reply to "+mine+", which is deleted") ||
		!strings.Contains(body, ">To a deleted post.</textarea>") {
		t.Errorf("replying to a deleted post: status %d, without the reason or what was typed\n%s", resp.StatusCode, body)
	}

	// A deleted root keeps its thread, which the list shows as deleted.
	if resp, _ := submit(t, base+"/t/"+root+"/delete", url.Values{"token": {tok}}, base, ""); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /t/%s/delete: status %d", root, resp.StatusCode)
	}
	if list := page(t, base+"/"); !strings.Contains(list, "Deleted by its author") || strings.Contains(list, "Alice asks") {
		t.Errorf("/ after deleting the root:\n%s", list)
	}
	// The command line's hint names a post that can still be replied to.
	if rootPage := page(t, base+"/t/"+root); !strings.Contains(rootPage, "agora reply "+agents+" ") || strings.Contains(rootPage, "agora reply "+root+" ") {
		t.Errorf("/t/%s with its root deleted hints at replying to another post than %s:\n%s", root, agents, rootPage)
	}

	// A thread whose every post is deleted shows no activity and no hint.
	alone := m.post("--as", "human/alice", "--title", "Posted by mistake", "-m", "Oops.")
	tok = token(t, page(t, base+"/t/"+alone))
	if resp, _ := submit(t, base+"/t/"+alone+"/delete", url.Values{"token": {tok}}, base, ""); resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /t/%s/delete: status %d", alone, resp.StatusCode)
	}
	if gone := page(t, base+"/t/"+alone); strings.Contains(gone, "<span>active") || strings.Contains(gone, "agora reply") ||
		strings.Contains(gone, "Posted by mistake") {
		t.Errorf("/t/%s with every post deleted:\n%s", alone, gone)
	}
	if code, stderr := stop(); code != 0 {
		t.Errorf("serve stopped with status %d\n%s", code, stderr)
	}
}
