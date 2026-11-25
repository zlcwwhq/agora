package cli

import (
	"slices"
	"strings"
	"testing"
)

type unreadJSON struct {
	NewReader bool `json:"new_reader"`
	Posts     []struct{ ID int64 }
	Edited    []struct {
		ID       int64
		Versions int
	}
}

func TestReaderCommands(t *testing.T) {
	e := newEnv(t)
	e.serve(t)
	e.vars["CODEX_THREAD_ID"] = "019a2b3c-codex"
	e.ok(t, "boards", "create", "proj")
	e.vars["AGORA_BOARD"] = "proj"
	asClaude := []string{"--as", "claude-code/95d3e5c1-claude"}

	e.ok(t, "post", "--title", "Plan", "-t", "a", "-t", "b", "-m", "ask @claude-code, not `@nope`")
	e.ok(t, "post", "--title", "Other", "-m", "nothing for claude")

	// A new reader for which the first thread is.
	var u unreadJSON
	e.json(t, &u, append([]string{"unread", "--to-me", "--since", "1h"}, asClaude...)...)
	if !u.NewReader || len(u.Posts) != 1 || u.Posts[0].ID != 1 || u.Edited == nil {
		t.Fatalf("unread --to-me = %+v", u)
	}

	// Single tags, versions, and history.
	var edited struct {
		ID     int64
		Edited string
	}
	e.json(t, &edited, "edit", "1", "--add-tag", "C", "--remove-tag", "a", "--remove-tag", "zzz")
	if edited.ID != 1 || edited.Edited == "" {
		t.Errorf("edit = %+v", edited)
	}
	var show struct {
		Posts []struct {
			Tags     []string
			Versions int
			Mentions []string
		}
	}
	e.json(t, &show, "show", "1")
	if p := show.Posts[0]; !slices.Equal(p.Tags, []string{"b", "c"}) || p.Versions != 2 || !slices.Equal(p.Mentions, []string{"claude-code"}) {
		t.Errorf("after edit: %+v", p)
	}
	var history struct {
		ID, Thread int64
		Versions   []struct {
			Version int
			Author  string
			Tags    []string
			Body    string
		}
	}
	e.json(t, &history, "history", "#1")
	if history.ID != 1 || history.Thread != 1 || len(history.Versions) != 2 || !slices.Equal(history.Versions[0].Tags, []string{"a", "b"}) ||
		history.Versions[1].Version != 2 || history.Versions[1].Author != "codex/019a2b3c@testbox" {
		t.Errorf("history = %+v", history)
	}
	if out := e.ok(t, "history", "1"); !strings.Contains(out, "2 versions") || !strings.Contains(out, "version 2 · codex/019a2b3c@testbox") {
		t.Errorf("history:\n%s", out)
	}

	// Reactions.
	var reacted struct {
		ID, Thread int64
		Reactions  []struct {
			Reaction string
			Authors  []string
		}
	}
	e.json(t, &reacted, append([]string{"react", "1", "👀"}, asClaude...)...)
	if reacted.ID != 1 || len(reacted.Reactions) != 1 || reacted.Reactions[0].Reaction != "eyes" ||
		!slices.Equal(reacted.Reactions[0].Authors, []string{"claude-code/95d3e5c1@testbox"}) {
		t.Errorf("react = %+v", reacted)
	}
	if out := e.ok(t, "show", "1"); !strings.Contains(out, "reactions: 👀 ×1 (claude-code/95d3e5c1@testbox)") {
		t.Errorf("show:\n%s", out)
	}
	e.json(t, &reacted, append([]string{"react", "1", "👀", "--undo"}, asClaude...)...)
	if reacted.Reactions == nil || len(reacted.Reactions) != 0 {
		t.Errorf("react --undo = %+v", reacted)
	}

	// The edit is news to claude, which saw the post before it.
	out := e.ok(t, append([]string{"unread", "--thread", "1", "--peek"}, asClaude...)...)
	if !strings.Contains(out, "edited since you saw them") || !strings.Contains(out, "mentions you") || !strings.Contains(out, "agora history 1") {
		t.Errorf("unread --thread --peek:\n%s", out)
	}
	e.json(t, &u, append([]string{"unread", "--thread", "1"}, asClaude...)...)
	if len(u.Posts) != 0 || len(u.Edited) != 1 || u.Edited[0].ID != 1 || u.Edited[0].Versions != 2 {
		t.Errorf("unread --thread = %+v", u)
	}
	// The other thread is still unread.
	e.json(t, &u, append([]string{"unread"}, asClaude...)...)
	if len(u.Posts) != 1 || u.Posts[0].ID != 2 || len(u.Edited) != 0 {
		t.Errorf("unread = %+v", u)
	}

	if r := e.run(append([]string{"unread", "--thread", "99"}, asClaude...)...); r.code != 1 || !strings.Contains(r.err, "no post #99") {
		t.Errorf("unread --thread 99: %+v", r)
	}
	if r := e.run("history", "99", "--json"); r.code != 1 || r.out != "" || !strings.Contains(r.err, "no post #99") {
		t.Errorf("history 99: %+v", r)
	}
	if r := e.run("react", "99", "ack"); r.code != 1 || !strings.Contains(r.err, "no post #99") {
		t.Errorf("react 99: %+v", r)
	}
}

func TestReaderUsageErrors(t *testing.T) {
	e := newEnv(t)
	e.vars["AGORA_AUTHOR"] = "human/alice"
	e.vars["AGORA_BOARD"] = "b"
	e.vars["AGORA_SERVER"] = "127.0.0.1:9"
	for _, c := range []struct {
		args []string
		err  string
	}{
		{[]string{"edit", "1", "--add-tag", "x", "-t", "y"}, "exclude -t and --no-tags"},
		{[]string{"edit", "1", "--remove-tag", "x", "--no-tags"}, "exclude -t and --no-tags"},
		{[]string{"edit", "1", "--add-tag", "a b"}, `invalid tag "a b"`},
		{[]string{"edit", "1", "--remove-tag", "#x"}, `invalid tag "#x"`},
		{[]string{"history"}, "usage: agora history ID"},
		{[]string{"history", "x"}, `invalid post id "x"`},
		{[]string{"history", "1", "2"}, "usage: agora history ID"},
		{[]string{"react", "1"}, "usage: agora react ID REACTION [--undo]"},
		{[]string{"react", "x", "ack"}, `invalid post id "x"`},
		{[]string{"react", "1", "two words"}, `invalid reaction "two words"`},
		{[]string{"react", "1", ""}, `invalid reaction ""`},
		{[]string{"react", "1", "ack", "more"}, "usage: agora react ID REACTION [--undo]"},
		{[]string{"unread", "--thread", "x"}, `invalid post id "x"`},
		{[]string{"unread", "--thread", "0"}, `invalid post id "0"`},
	} {
		if r := e.run(c.args...); r.code != 2 || !strings.Contains(r.err, c.err) {
			t.Errorf("agora %s: exit %d, want 2 and %q: %s", strings.Join(c.args, " "), r.code, c.err, r.err)
		}
	}
}
