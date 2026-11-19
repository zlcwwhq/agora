package cli

import (
	"html"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestServeGivesBackFormsFromBeforeARestart(t *testing.T) {
	m := newMachine(t, "wsl")
	root := m.post("--title", "Agents start here", "-m", "A question for people.")
	// A form loaded from one serve and sent to another one on the same
	// board is a form loaded before serve restarted.
	before, stopBefore := m.startServe("--as", "human/alice")
	after, stopAfter := m.startServe("--as", "human/alice")
	old, now := strings.TrimSuffix(before.URL, "/"), strings.TrimSuffix(after.URL, "/")
	oldPage := page(t, old+"/new")
	oldToken, oldNonce := token(t, oldPage), nonce(t, oldPage)
	if current := token(t, page(t, now+"/new")); current == oldToken {
		t.Fatalf("both servers have the token %s", current)
	}

	// A new thread comes back with what was typed and the current token.
	typed := url.Values{"token": {oldToken}, "nonce": {oldNonce}, "title": {"Typed before the restart"},
		"tags": {"people, design"}, "body": {"A long text <that> mustn't be lost.\n"}}
	files := postFiles(t, m)
	resp, body := submit(t, now+"/new", typed, now, "")
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(body, "agora serve has restarted") {
		t.Fatalf("POST /new with the old token: status %d\n%s", resp.StatusCode, body)
	}
	for _, want := range []string{`value="Typed before the restart"`, `value="people, design"`,
		">" + html.EscapeString("A long text <that> mustn't be lost.\n") + "</textarea>"} {
		if !strings.Contains(body, want) {
			t.Errorf("the form given back lacks %s:\n%s", want, body)
		}
	}
	if postFiles(t, m) != files {
		t.Fatal("a form with the old token posted")
	}
	given := token(t, body)
	if given == oldToken || nonce(t, body) != oldNonce {
		t.Errorf("the form given back has token %s and nonce %s; want the current token and nonce %s", given, nonce(t, body), oldNonce)
	}
	typed.Set("token", given)
	if resp, _ := submit(t, now+"/new", typed, now, ""); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("sending the form given back: status %d", resp.StatusCode)
	}

	// So does a reply, in its thread.
	oldThread := page(t, old+"/t/"+root)
	reply := url.Values{"token": {token(t, oldThread)}, "nonce": {nonce(t, oldThread)}, "body": {"An answer typed before."}}
	resp, body = submit(t, now+"/t/"+root, reply, now, "")
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(body, ">An answer typed before.</textarea>") ||
		!strings.Contains(body, `action="/t/`+root+`#reply"`) {
		t.Errorf("POST /t/%s with the old token: status %d\n%s", root, resp.StatusCode, body)
	}
	// A reply to a post that isn't here keeps its text without a form.
	resp, body = submit(t, now+"/t/wsl-99", reply, now, "")
	if resp.StatusCode != http.StatusForbidden || !strings.Contains(body, ">An answer typed before.</textarea>") {
		t.Errorf("POST /t/wsl-99 with the old token: status %d\n%s", resp.StatusCode, body)
	}

	// Without an Origin that names these pages, nothing shows that the form
	// came from them, so nothing comes back; nor for deleting.
	typed.Set("token", oldToken)
	for _, c := range []struct{ name, path, origin string }{
		{"no Origin", "/new", ""},
		{"another Origin", "/new", "http://evil.example"},
		{"deleting", "/t/" + root + "/delete", now},
	} {
		resp, body := submit(t, now+c.path, typed, c.origin, "")
		if resp.StatusCode != http.StatusForbidden || strings.Contains(body, "Typed before the restart") || strings.Contains(body, `name="token"`) {
			t.Errorf("%s: POST %s with the old token: status %d\n%s", c.name, c.path, resp.StatusCode, body)
		}
	}
	if postFiles(t, m) != files+1 {
		t.Errorf("posted %d posts, want only the form sent again", postFiles(t, m)-files)
	}
	for _, stop := range []func() (int, string){stopBefore, stopAfter} {
		if code, stderr := stop(); code != 0 {
			t.Errorf("serve stopped with status %d\n%s", code, stderr)
		}
	}
}
