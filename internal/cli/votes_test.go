package cli

import (
	"slices"
	"strings"
	"testing"
)

func TestVoteCommand(t *testing.T) {
	e := newEnv(t)
	e.serve(t)
	e.vars["CODEX_THREAD_ID"] = "019a2b3c-codex"
	e.ok(t, "boards", "create", "proj")
	e.vars["AGORA_BOARD"] = "proj"
	asClaude := []string{"--as", "claude-code/95d3e5c1-claude"}
	asAlice := []string{"--as", "human/alice"}
	e.ok(t, "post", "--title", "Plan", "-m", "a plan")
	e.ok(t, append([]string{"reply", "1", "-m", "agreed"}, asClaude...)...)

	type voted struct {
		ID, Thread int64
		Score      int
		Votes      struct{ Up, Down []string }
	}
	var v voted
	e.json(t, &v, append([]string{"vote", "1", "up"}, asClaude...)...)
	if v.ID != 1 || v.Thread != 1 || v.Score != 1 || !slices.Equal(v.Votes.Up, []string{"claude-code/95d3e5c1@testbox"}) ||
		v.Votes.Down == nil || len(v.Votes.Down) != 0 {
		t.Errorf("vote up = %+v", v)
	}
	out := e.ok(t, append([]string{"vote", "#1", "down"}, asAlice...)...)
	if !strings.Contains(out, "voted #1 down") ||
		!strings.Contains(out, "votes: score 0 · up ×1 (claude-code/95d3e5c1@testbox) · down ×1 (human/alice@testbox)") {
		t.Errorf("vote down:\n%s", out)
	}
	// Its author may vote on its own post, and on the reply.
	e.ok(t, "vote", "1", "up")
	e.ok(t, append([]string{"vote", "2", "down"}, asAlice...)...)
	out = e.ok(t, append([]string{"vote", "1", "--undo"}, asAlice...)...)
	if !strings.Contains(out, "took back your vote on #1") || !strings.Contains(out, "votes: score 2 · up ×2") {
		t.Errorf("vote --undo:\n%s", out)
	}
	e.json(t, &v, append([]string{"vote", "1", "--undo"}, asAlice...)...)
	if v.Score != 2 {
		t.Errorf("vote --undo without a vote = %+v", v)
	}

	var show struct {
		Thread struct{ Score int }
		Posts  []struct {
			Score int
			Votes struct{ Up, Down []string }
		}
	}
	e.json(t, &show, "show", "1")
	if show.Thread.Score != 2 || show.Posts[0].Score != 2 || show.Posts[1].Score != -1 ||
		!slices.Equal(show.Posts[1].Votes.Down, []string{"human/alice@testbox"}) {
		t.Errorf("show = %+v", show)
	}
	out = e.ok(t, "show", "1")
	if !strings.Contains(out, "score 2") || !strings.Contains(out, "votes: score -1 · down ×1 (human/alice@testbox)") {
		t.Errorf("show:\n%s", out)
	}
	if out := e.ok(t, "list"); !strings.Contains(out, "score 2") {
		t.Errorf("list:\n%s", out)
	}

	if r := e.run("vote", "99", "up"); r.code != 1 || !strings.Contains(r.err, "no post #99") {
		t.Errorf("vote 99: %+v", r)
	}
}

func TestVoteUsageErrors(t *testing.T) {
	e := newEnv(t)
	e.vars["AGORA_AUTHOR"] = "human/alice"
	e.vars["AGORA_BOARD"] = "b"
	e.vars["AGORA_SERVER"] = "127.0.0.1:9"
	const usage = "usage: agora vote ID (up | down | --undo)"
	for _, c := range []struct {
		args []string
		err  string
	}{
		{[]string{"vote"}, usage},
		{[]string{"vote", "1"}, usage},
		{[]string{"vote", "1", "sideways"}, usage},
		{[]string{"vote", "1", "none"}, usage},
		{[]string{"vote", "1", "Up"}, usage},
		{[]string{"vote", "1", "up", "--undo"}, usage},
		{[]string{"vote", "1", "up", "down"}, usage},
		{[]string{"vote", "1", "2", "--undo"}, usage},
		{[]string{"vote", "x", "up"}, `invalid post id "x"`},
		{[]string{"vote", "x", "--undo"}, `invalid post id "x"`},
	} {
		if r := e.run(c.args...); r.code != 2 || !strings.Contains(r.err, c.err) {
			t.Errorf("agora %s: exit %d, want 2 and %q: %s", strings.Join(c.args, " "), r.code, c.err, r.err)
		}
	}
}

func TestReactWithGitHubsReactions(t *testing.T) {
	e := newEnv(t)
	e.serve(t)
	e.vars["AGORA_AUTHOR"] = "human/alice"
	e.ok(t, "boards", "create", "proj")
	e.vars["AGORA_BOARD"] = "proj"
	e.ok(t, "post", "--title", "Plan", "-m", "a plan")

	type reacted struct {
		Reactions []struct {
			Reaction string
			Authors  []string
		}
	}
	names := func(r reacted) []string {
		var out []string
		for _, x := range r.Reactions {
			out = append(out, x.Reaction)
		}
		return out
	}
	var r reacted
	// By name, -1 included, which isn't a flag, by emoji, and ack.
	e.json(t, &r, "react", "1", "-1")
	e.json(t, &r, "react", "--json", "1", "🚀")
	e.json(t, &r, "react", "1", "ack")
	if want := []string{"-1", "rocket", "eyes"}; !slices.Equal(names(r), want) {
		t.Errorf("reactions = %v, want %v", names(r), want)
	}
	if out := e.ok(t, "react", "1", "hooray"); !strings.Contains(out, "reacted with 🎉 hooray on #1") ||
		!strings.Contains(out, "reactions: 👎 ×1 (human/alice@testbox) · 🚀 ×1") {
		t.Errorf("react hooray:\n%s", out)
	}
	// As agora-skill says to write it, with --, too.
	e.json(t, &r, "react", "1", "--", "-1")
	if want := []string{"-1", "rocket", "eyes", "hooray"}; !slices.Equal(names(r), want) {
		t.Errorf("react 1 -- -1 = %v, want %v", names(r), want)
	}
	e.json(t, &r, "react", "--undo", "1", "-1")
	e.json(t, &r, "react", "1", "👀", "--undo")
	if want := []string{"rocket", "hooray"}; !slices.Equal(names(r), want) {
		t.Errorf("after --undo = %v, want %v", names(r), want)
	}
	if out := e.ok(t, "show", "1"); !strings.Contains(out, "reactions: 🚀 ×1 (human/alice@testbox) · 🎉 ×1 (human/alice@testbox)") {
		t.Errorf("show:\n%s", out)
	}

	for _, bad := range []string{"lgtm", "thumbsup", ":+1:", "✅", "Eyes"} {
		if res := e.run("react", "1", bad); res.code != 2 || !strings.Contains(res.err, "invalid reaction") ||
			!strings.Contains(res.err, "+1 👍, -1 👎, laugh 😄, hooray 🎉, confused 😕, heart ❤️, rocket 🚀, eyes 👀") {
			t.Errorf("react 1 %s: %+v", bad, res)
		}
	}
	// A negative number that is a flag's value stays its value.
	if res := e.run("list", "-n", "-1"); res.code != 2 || !strings.Contains(res.err, "-n must be at least 1") {
		t.Errorf("list -n -1: %+v", res)
	}
	if res := e.run("react", "1", "-2"); res.code != 2 || !strings.Contains(res.err, `invalid reaction "-2"`) {
		t.Errorf("react 1 -2: %+v", res)
	}
}
