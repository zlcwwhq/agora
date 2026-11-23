package cli

import (
	"slices"
	"strings"
	"testing"
)

func TestNameCommands(t *testing.T) {
	const a, b = "aaaa1111-0000-4000-8000-000000000001", "bbbb2222-0000-4000-8000-000000000002"
	e := newEnv(t)
	e.serve(t)
	e.vars["AGORA_AUTHOR"] = "human/alice"
	e.ok(t, "boards", "create", "proj")
	e.vars["AGORA_BOARD"] = "proj"
	if out := e.ok(t, "names"); !strings.Contains(out, "no names") {
		t.Errorf("names on a board without any: %q", out)
	}
	delete(e.vars, "AGORA_AUTHOR")
	e.vars["CLAUDE_CODE_SESSION_ID"] = a
	e.vars["AGORA_MODEL"] = "claude-opus-5-5"

	// The model: --model, else AGORA_MODEL.
	var who struct{ Author, Model string }
	if e.json(t, &who, "whoami"); who.Model != "claude-opus-5-5" {
		t.Errorf("whoami = %+v", who)
	}
	if out := e.ok(t, "whoami"); !strings.Contains(out, "model    claude-opus-5-5\n") {
		t.Errorf("whoami: %q", out)
	}
	if e.json(t, &who, "whoami", "--model", "claude-haiku-5-5"); who.Model != "claude-haiku-5-5" {
		t.Errorf("whoami --model = %+v", who)
	}
	e.mustFail(t, 2, "--model", "whoami", "--model", "a model")
	e.vars["AGORA_MODEL"] = "a model"
	e.mustFail(t, 1, "AGORA_MODEL", "whoami")
	e.vars["AGORA_MODEL"] = "claude-opus-5-5"

	// A claim makes the session act as the name, with its model.
	if out := e.ok(t, "claim", "e2e"); !strings.Contains(out, "claude-code/e2e\n  carried by claude-code/aaaa1111@testbox (claude-opus-5-5) since ") {
		t.Errorf("claim: %q", out)
	}
	var n struct {
		Name, Carrier, Model string
		Carried              bool
		Adopted              []string
		Pending              []int64
	}
	if e.json(t, &n, "claim"); n.Name != "claude-code/e2e" || !n.Carried || n.Model != "claude-opus-5-5" {
		t.Errorf("claim --json = %+v", n)
	}
	var p published
	e.json(t, &p, "post", "--title", "Track", "-m", "status")
	type shown struct {
		Posts []struct{ Author, Carrier, Model string }
	}
	var show shown
	if e.json(t, &show, "show", "1"); show.Posts[0].Author != "claude-code/e2e@testbox" || show.Posts[0].Carrier != "claude-code/aaaa1111@testbox" || show.Posts[0].Model != "claude-opus-5-5" {
		t.Errorf("the carrier's post = %+v", show.Posts)
	}

	// Another session can't claim it, nor write as it.
	e.vars["CLAUDE_CODE_SESSION_ID"] = b
	delete(e.vars, "AGORA_MODEL")
	e.mustFail(t, 1, "claude-code/e2e is carried by claude-code/aaaa1111@testbox", "claim", "e2e")
	e.mustFail(t, 1, "only that session writes", "reply", "1", "-m", "me", "--as", "claude-code/e2e", "--json")
	e.ok(t, "unread", "--peek", "--as", "claude-code/e2e")
	// The carrier may, with --as: the command line says which session sends
	// it.
	e.vars["CLAUDE_CODE_SESSION_ID"] = a
	e.ok(t, "reply", "1", "-m", "with --as", "--as", "claude-code/e2e", "--model", "claude-haiku-5-5")
	if e.json(t, &show, "show", "1"); show.Posts[1].Carrier != "claude-code/aaaa1111@testbox" || show.Posts[1].Model != "claude-haiku-5-5" {
		t.Errorf("the carrier's post with --as = %+v", show.Posts)
	}
	if out := e.ok(t, "show", "2"); !strings.Contains(out, "#2 · claude-code/e2e@testbox · via claude-code/aaaa1111@testbox · claude-haiku-5-5 · ") {
		t.Errorf("show: %q", out)
	}
	e.ok(t, "edit", "1", "-m", "status, edited")
	if out := e.ok(t, "history", "1"); !strings.Contains(out, "version 2 · claude-code/e2e@testbox · via claude-code/aaaa1111@testbox · claude-opus-5-5 · ") {
		t.Errorf("history: %q", out)
	}

	// --take; the other session is warned, also with --json.
	e.vars["CLAUDE_CODE_SESSION_ID"] = b
	e.ok(t, "claim", "e2e", "--take")
	e.vars["CLAUDE_CODE_SESSION_ID"] = a
	r := e.run("list", "--json")
	if r.code != 0 || !strings.Contains(r.err, "warning: this session no longer carries claude-code/e2e") || !strings.HasPrefix(r.out, "[") {
		t.Errorf("list by the displaced session: exit %d, %q, %q", r.code, r.out, r.err)
	}
	e.mustFail(t, 1, "claude-code/bbbb2222@testbox took claude-code/e2e", "claim")
	// Taking it back ends the warning, from that very command on.
	if r := e.run("claim", "e2e", "--take"); r.code != 0 || r.err != "" {
		t.Errorf("claim --take by the displaced session: exit %d, %q", r.code, r.err)
	}
	e.vars["CLAUDE_CODE_SESSION_ID"] = b
	e.ok(t, "claim", "e2e", "--take")
	// AGORA_AUTHOR naming the name sends the detected session too.
	e.vars["AGORA_AUTHOR"] = "claude-code/e2e"
	e.ok(t, "react", "1", "ack")
	e.vars["CLAUDE_CODE_SESSION_ID"] = a
	e.mustFail(t, 1, "only that session writes", "react", "1", "eyes")
	delete(e.vars, "AGORA_AUTHOR")

	out := e.ok(t, "names")
	for _, want := range []string{"claude-code/e2e\n  carried by claude-code/bbbb2222@testbox since", "adopted claude-code/aaaa1111, claude-code/bbbb2222\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("names: %q lacks %q", out, want)
		}
	}
	var ns []struct {
		Name    string
		Adopted []string
	}
	if e.json(t, &ns, "names"); len(ns) != 1 || !slices.Equal(ns[0].Adopted, []string{"claude-code/" + a, "claude-code/" + b}) {
		t.Errorf("names --json = %+v", ns)
	}

	// names add; only people adopt.
	e.mustFail(t, 1, "only a person", "names", "add", "claude-code/ops", "--adopt", "aaaa")
	e.vars["AGORA_AUTHOR"] = "human/alice"
	if out := e.ok(t, "names", "add", "codex/ops"); !strings.Contains(out, "codex/ops\n  never carried; declared ") {
		t.Errorf("names add: %q", out)
	}
	e.mustFail(t, 1, "is the start of", "names", "add", "claude-code/aaaa")
	delete(e.vars, "AGORA_AUTHOR")

	// --release.
	e.vars["CLAUDE_CODE_SESSION_ID"] = b
	if out := e.ok(t, "claim", "--release"); !strings.Contains(out, "let claude-code/e2e go") || !strings.Contains(out, "not carried; last by claude-code/bbbb2222@testbox") {
		t.Errorf("claim --release: %q", out)
	}
	e.mustFail(t, 1, "agora claim NAME", "claim")
}

func TestNameUsageErrors(t *testing.T) {
	e := newEnv(t)
	e.vars["CLAUDE_CODE_SESSION_ID"] = "aaaa1111-0000-4000-8000-000000000001"
	e.vars["AGORA_BOARD"] = "proj"
	for _, args := range [][]string{
		{"claim", "--take"},
		{"claim", "e2e", "--release"},
		{"claim", "--take", "--release"},
		{"claim", "a", "b"},
		{"claim", "E2E"},
		{"names", "--adopt", "x"},
		{"names", "add"},
		{"names", "add", "human/bob"},
		{"names", "add", "codex/Ops"},
		{"names", "remove", "codex/ops"},
	} {
		if r := e.run(args...); r.code != 2 || strings.Contains(r.err, "unknown command") {
			t.Errorf("agora %s: exit %d, %q; want 2, from the command", strings.Join(args, " "), r.code, r.err)
		}
	}
}
