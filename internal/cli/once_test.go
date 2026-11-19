package cli

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
)

var nonceField = regexp.MustCompile(`name="nonce" value="([0-9a-f]{32})"`)

// nonce returns the nonce of a page's form.
func nonce(t *testing.T, body string) string {
	t.Helper()
	m := nonceField.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("the page's form has no nonce:\n%s", body)
	}
	return m[1]
}

func TestServeFormsPostOnce(t *testing.T) {
	m := newMachine(t, "wsl")
	root := m.post("--title", "Agents start here", "-m", "A question for people.")
	s, stop := m.startServe("--as", "human/alice")
	base := strings.TrimSuffix(s.URL, "/")

	// sendTwice sends the same form twice, as a double click does, and
	// returns where both responses lead.
	sendTwice := func(path string, fields url.Values) string {
		t.Helper()
		before := postFiles(t, m)
		var locs []string
		for range 2 {
			resp, _ := submit(t, base+path, fields, base, "")
			if resp.StatusCode != http.StatusSeeOther {
				t.Fatalf("POST %s: status %d", path, resp.StatusCode)
			}
			locs = append(locs, resp.Header.Get("Location"))
		}
		if got := postFiles(t, m) - before; got != 1 {
			t.Errorf("POST %s twice with the same nonce published %d posts, want 1", path, got)
		}
		if locs[0] != locs[1] {
			t.Errorf("POST %s again led to %s, not to %s", path, locs[1], locs[0])
		}
		return locs[0]
	}

	// Each page that shows a form gives it a nonce of its own.
	first, second := page(t, base+"/new"), page(t, base+"/new")
	if nonce(t, first) == nonce(t, second) {
		t.Fatalf("two pages gave the form the same nonce %s", nonce(t, first))
	}
	tok := token(t, first)
	sendTwice("/new", url.Values{"token": {tok}, "nonce": {nonce(t, first)}, "title": {"Sent twice"}})
	threadPage := page(t, base+"/t/"+root)
	sendTwice("/t/"+root, url.Values{"token": {tok}, "nonce": {nonce(t, threadPage)}, "body": {"A reply sent twice."}})

	// A refused post's form keeps its nonce, which then posts once.
	n := nonce(t, second)
	resp, body := submit(t, base+"/new", url.Values{"token": {tok}, "nonce": {n}, "body": {"No title yet."}}, base, "")
	if resp.StatusCode != http.StatusBadRequest || nonce(t, body) != n {
		t.Fatalf("a refused post: status %d, its form's nonce %s, want %s", resp.StatusCode, nonce(t, body), n)
	}
	sendTwice("/new", url.Values{"token": {tok}, "nonce": {n}, "title": {"Titled now"}})

	// A form changed after going back to it publishes, though its page and
	// so its nonce are the same.
	back := page(t, base+"/new")
	again := url.Values{"token": {tok}, "nonce": {nonce(t, back)}, "title": {"Before going back"}}
	firstLoc := sendTwice("/new", again)
	again.Set("title", "After going back")
	if loc := sendTwice("/new", again); loc == firstLoc {
		t.Errorf("a changed form led to the first post, %s", loc)
	}

	// Two sent at once post once too.
	before := postFiles(t, m)
	sameTime := url.Values{"token": {tok}, "nonce": {nonce(t, page(t, base+"/new"))}, "title": {"Sent at once"}}
	var wg sync.WaitGroup
	results := make([]string, 4)
	for i := range results {
		wg.Go(func() {
			req, err := http.NewRequest(http.MethodPost, base+"/new", strings.NewReader(sameTime.Encode()))
			if err != nil {
				results[i] = err.Error()
				return
			}
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			resp, err := browser.Do(req)
			if err != nil {
				results[i] = err.Error()
				return
			}
			_ = resp.Body.Close()
			results[i] = fmt.Sprintf("%d %s", resp.StatusCode, resp.Header.Get("Location"))
		})
	}
	wg.Wait()
	for _, r := range results {
		if r != results[0] || !strings.HasPrefix(r, "303 /t/") {
			t.Errorf("forms sent at once got %q", results)
			break
		}
	}
	if got := postFiles(t, m) - before; got != 1 {
		t.Errorf("%d forms sent at once published %d posts, want 1", len(results), got)
	}

	// A form without a nonce publishes every time.
	before = postFiles(t, m)
	for range 2 {
		if resp, _ := submit(t, base+"/new", url.Values{"token": {tok}, "title": {"No nonce"}}, base, ""); resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("POST /new without a nonce: status %d", resp.StatusCode)
		}
	}
	if got := postFiles(t, m) - before; got != 2 {
		t.Errorf("POST /new twice without a nonce published %d posts, want 2", got)
	}
	if code, stderr := stop(); code != 0 {
		t.Errorf("serve stopped with status %d\n%s", code, stderr)
	}
}
