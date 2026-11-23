package cli

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"
)

type unreadShape struct {
	NewReader bool `json:"new_reader"`
	Posts     []struct{ ID int64 }
	Edited    []struct{ ID int64 }
}

func unreadOf(t *testing.T, r result) []int64 {
	t.Helper()
	var u unreadShape
	if err := json.Unmarshal([]byte(r.out), &u); err != nil || u.Posts == nil || u.Edited == nil {
		t.Fatalf("not the shape of unread: %q, %v", r.out, err)
	}
	var out []int64
	for _, p := range append(u.Posts, u.Edited...) {
		out = append(out, p.ID)
	}
	return out
}

func TestWaitCommand(t *testing.T) {
	e := newEnv(t)
	e.serve(t)
	e.vars["AGORA_AUTHOR"] = "human/alice"
	e.ok(t, "boards", "create", "proj")
	e.vars["AGORA_BOARD"] = "proj"
	delete(e.vars, "AGORA_AUTHOR")
	e.vars["CLAUDE_CODE_SESSION_ID"] = "aaaa1111-0000-4000-8000-000000000001"
	e.ok(t, "claim", "e2e")

	// When the time is up, it exits with status 3, printing what unread
	// prints without posts, also with --json.
	start := time.Now()
	r := e.run("wait", "--to-me", "--timeout", "1s", "--json")
	if r.code != 3 || !strings.Contains(r.err, "nothing to show within 1s") || len(unreadOf(t, r)) != 0 || time.Since(start) < time.Second {
		t.Errorf("wait that times out: exit %d, %q, %q", r.code, r.out, r.err)
	}
	if r := e.run("wait", "--timeout", "1s"); r.code != 3 || r.out != "no unread posts\n" {
		t.Errorf("wait that times out, as text: exit %d, %q, %q", r.code, r.out, r.err)
	}

	// It returns once a post for the reader arrives, and marks it seen.
	got := make(chan result, 1)
	go func() { got <- e.run("wait", "--to-me", "--json", "--timeout", "30s") }()
	waiting := false
	for range 500 {
		var ns []struct{ Waiting bool }
		if e.json(t, &ns, "names"); len(ns) == 1 && ns[0].Waiting {
			waiting = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("names never showed the name waiting")
	}
	e.ok(t, "post", "--title", "Noise", "-m", "not for the name", "--as", "human/alice")
	e.ok(t, "post", "--title", "Job", "-m", "@claude-code/e2e run the suite", "--as", "human/alice")
	select {
	case r := <-got:
		if r.code != 0 || !slices.Equal(unreadOf(t, r), []int64{2}) {
			t.Errorf("wait: exit %d, %q, %q", r.code, r.out, r.err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("wait didn't return")
	}
	if got := unreadOf(t, e.run("unread", "--to-me", "--peek", "--json")); len(got) != 0 {
		t.Errorf("unread after wait = %v", got)
	}

	// --peek leaves what it shows unread, so the wait that follows returns
	// at once, and marks it.
	e.ok(t, "post", "--title", "Again", "-m", "@claude-code/e2e again", "--as", "human/alice")
	for _, args := range [][]string{{"--peek"}, {"--timeout", "0"}} {
		r := e.run(append([]string{"wait", "--to-me", "--json"}, args...)...)
		if r.code != 0 || !slices.Equal(unreadOf(t, r), []int64{3}) {
			t.Errorf("wait %s: exit %d, %q, %q", strings.Join(args, " "), r.code, r.out, r.err)
		}
	}
	if got := unreadOf(t, e.run("unread", "--to-me", "--peek", "--json")); len(got) != 0 {
		t.Errorf("unread after the wait that marks = %v", got)
	}
	// Without --to-me, it waits for any post, here the noise.
	if r := e.run("wait", "--json", "--timeout", "5s"); r.code != 0 || !slices.Equal(unreadOf(t, r), []int64{1}) {
		t.Errorf("wait for any post: exit %d, %q, %q", r.code, r.out, r.err)
	}

	e.mustFail(t, 2, "--timeout", "wait", "--timeout", "5")
	e.mustFail(t, 2, "--timeout", "wait", "--timeout", "-1s")
	e.mustFail(t, 1, "doesn't exist", "wait", "--thread", "99", "--timeout", "1s")
}
