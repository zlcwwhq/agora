package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	earlier = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	later   = earlier.Add(time.Hour)
)

// transcript writes content to the file at path, making its directory, and
// gives it the modification time at.
func transcript(t *testing.T, path string, at time.Time, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, at, at); err != nil {
		t.Fatal(err)
	}
}

// appendLines adds lines to the transcript at path, which keeps the
// modification time at.
func appendLines(t *testing.T, path string, at time.Time, lines ...string) {
	t.Helper()
	old, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	transcript(t, path, at, string(old)+strings.Join(lines, "\n")+"\n")
}

// modelOf returns the model that whoami reports, and fails the test unless
// it succeeds without writing to standard error.
func (e *testEnv) modelOf(t *testing.T, args ...string) string {
	t.Helper()
	r := e.run(append([]string{"whoami", "--json"}, args...)...)
	if r.code != 0 || r.err != "" {
		t.Fatalf("whoami %s: exit %d: %q", strings.Join(args, " "), r.code, r.err)
	}
	var who struct{ Model string }
	if err := json.Unmarshal([]byte(r.out), &who); err != nil {
		t.Fatalf("whoami: %v in %q", err, r.out)
	}
	return who.Model
}

func TestDetectedModelClaudeCode(t *testing.T) {
	const s = "cccc3333-0000-4000-8000-000000000003"
	e := newEnv(t)
	e.vars["CLAUDE_CODE_SESSION_ID"] = s
	path := filepath.Join(e.home, ".claude", "projects", "-home-proj", s+".jsonl")
	transcript(t, path, earlier, strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"go"}}`,
		`{"type":"assistant","message":{"model":"claude-opus-5-5","content":[{"type":"tool_use","name":"Bash"}]}}`,
		`{"type":"assistant","isSidechain":true,"message":{"model":"claude-haiku-5-5"}}`,
		`{"type":"assistant","message":{"model":"<synthetic>","content":[{"type":"text","text":"No response requested."}]}}`,
		`{"type":"assistant","message":{"model":"a model"}}`,
		`{"type":"user","message":{"role":"user","content":"what the assistant said"}}`,
		`{"type":"assistant","message":{"model":"claude-sonn`,
	}, "\n"))

	// The last response's model, but for subagents', synthetic ones, and
	// invalid ones; a line still being written doesn't count.
	if m := e.modelOf(t); m != "claude-opus-5-5" {
		t.Errorf("detected model = %q", m)
	}
	// It follows the session to another model.
	appendLines(t, path, earlier, `et-5-5"}}`, `{"type":"assistant","message":{"model":"claude-sonnet-5-5"}}`)
	if m := e.modelOf(t); m != "claude-sonnet-5-5" {
		t.Errorf("after a switch = %q", m)
	}
	// --model and AGORA_MODEL come first.
	if m := e.modelOf(t, "--model", "claude-fable-5-2"); m != "claude-fable-5-2" {
		t.Errorf("with --model = %q", m)
	}
	e.vars["AGORA_MODEL"] = "claude-haiku-5-5"
	if m := e.modelOf(t); m != "claude-haiku-5-5" {
		t.Errorf("with AGORA_MODEL = %q", m)
	}
	delete(e.vars, "AGORA_MODEL")
	// Only for an identity that detection found: another identity, or even
	// the session named by --as or AGORA_AUTHOR, gets none.
	for _, as := range []string{"human/alice", "claude-code/" + s} {
		if m := e.modelOf(t, "--as", as); m != "" {
			t.Errorf("with --as %s = %q", as, m)
		}
	}
	e.vars["AGORA_AUTHOR"] = "claude-code/" + s
	if m := e.modelOf(t); m != "" {
		t.Errorf("with AGORA_AUTHOR = %q", m)
	}
	delete(e.vars, "AGORA_AUTHOR")

	// Of the projects that hold the session, as after it moves to a
	// worktree, the most recently modified.
	other := filepath.Join(e.home, ".claude", "projects", "-home-proj--claude-worktrees-x", s+".jsonl")
	transcript(t, other, later, `{"type":"assistant","message":{"model":"claude-fable-5-2"}}`+"\n")
	if m := e.modelOf(t); m != "claude-fable-5-2" {
		t.Errorf("newer project = %q", m)
	}
	if err := os.Chtimes(other, earlier.Add(-time.Hour), earlier.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if m := e.modelOf(t); m != "claude-sonnet-5-5" {
		t.Errorf("older project = %q", m)
	}

	// CLAUDE_CONFIG_DIR moves the directory, if it is absolute, whatever
	// its name holds.
	config := filepath.Join(e.home, "claude-config[1]")
	transcript(t, filepath.Join(config, "projects", "p", s+".jsonl"), earlier, `{"type":"assistant","message":{"model":"claude-opus-5-5"}}`+"\n")
	e.vars["CLAUDE_CONFIG_DIR"] = config
	if m := e.modelOf(t); m != "claude-opus-5-5" {
		t.Errorf("with CLAUDE_CONFIG_DIR = %q", m)
	}
	e.vars["CLAUDE_CONFIG_DIR"] = "claude-config[1]"
	if m := e.modelOf(t); m != "claude-sonnet-5-5" {
		t.Errorf("with a relative CLAUDE_CONFIG_DIR = %q", m)
	}

	// Nor does a relative home count, which would name a directory below
	// wherever agora runs, even when it leads to the home. Where the tests
	// run on another volume than the home, as on Windows, no relative path
	// leads there.
	delete(e.vars, "CLAUDE_CONFIG_DIR")
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if rel, err := filepath.Rel(wd, e.home); err == nil && !filepath.IsAbs(rel) {
		e.vars["HOME"] = rel
		if m := e.modelOf(t); m != "" {
			t.Errorf("with a relative home = %q", m)
		}
	}
}

func TestDetectedModelCodex(t *testing.T) {
	const thread = "019a2b3c-0000-7000-8000-000000000004"
	e := newEnv(t)
	e.vars["CODEX_THREAD_ID"] = thread
	sessions := filepath.Join(e.home, ".codex", "sessions")
	first := filepath.Join(sessions, "2026", "11", "22", "rollout-2026-11-22T10-00-00-"+thread+".jsonl")
	transcript(t, first, earlier, strings.Join([]string{
		`{"timestamp":"2026-11-22T09:00:00.000Z","type":"session_meta","payload":{"id":"` + thread + `"}}`,
		`{"timestamp":"2026-11-22T09:00:01.000Z","type":"turn_context","payload":{"cwd":"/p","model":"gpt-5.5-codex","effort":"high"}}`,
		`{"timestamp":"2026-11-22T09:00:02.000Z","type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"model\":\"x\"}"}}`,
	}, "\n")+"\n")
	// Another thread's rollout, even a newer one, doesn't count.
	transcript(t, filepath.Join(sessions, "2026", "11", "22", "rollout-2026-11-22T11-00-00-019a2b3c-0000-7000-8000-000000000005.jsonl"), later,
		`{"timestamp":"2026-11-22T10:00:00.000Z","type":"turn_context","payload":{"model":"gpt-5.5-mini"}}`+"\n")

	if m := e.modelOf(t); m != "gpt-5.5-codex" {
		t.Errorf("detected model = %q", m)
	}
	// The newest of the thread's rollouts, such as one that a revert
	// wrote on a later day.
	transcript(t, filepath.Join(sessions, "2026", "11", "23", "rollout-2026-11-23T08-00-00-"+thread+"_019a2b3c-0000-7000-8000-000000000006.jsonl"), later,
		`{"timestamp":"2026-11-23T07:00:00.000Z","type":"turn_context","payload":{"model":"gpt-5.5"}}`+"\n")
	if m := e.modelOf(t); m != "gpt-5.5" {
		t.Errorf("after a revert = %q", m)
	}

	// CODEX_HOME moves the directory, if it is absolute.
	home := filepath.Join(e.home, "codex-home")
	transcript(t, filepath.Join(home, "sessions", "2026", "11", "23", "rollout-2026-11-23T09-00-00-"+thread+".jsonl"), earlier,
		`{"timestamp":"2026-11-23T08:00:00.000Z","type":"turn_context","payload":{"model":"o5"}}`+"\n")
	e.vars["CODEX_HOME"] = home
	if m := e.modelOf(t); m != "o5" {
		t.Errorf("with CODEX_HOME = %q", m)
	}
	e.vars["CODEX_HOME"] = "codex-home"
	if m := e.modelOf(t); m != "gpt-5.5" {
		t.Errorf("with a relative CODEX_HOME = %q", m)
	}
}

func TestDetectedModelAntigravity(t *testing.T) {
	const conv = "ffff6666-0000-4000-8000-000000000009"
	e := newEnv(t)
	e.vars["ANTIGRAVITY_CONVERSATION_ID"] = conv
	logs := func(dir, conv string) string {
		return filepath.Join(dir, "brain", conv, ".system_generated", "logs", "transcript.jsonl")
	}
	gemini := filepath.Join(e.home, ".gemini")
	// Without ANTIGRAVITY_APP_DATA_DIR, the first of the CLI's, the desktop
	// app's, and the IDE's directories that holds the conversation.
	transcript(t, logs(filepath.Join(gemini, "antigravity-cli"), "aaaaaaaa-0000-4000-8000-000000000000"), later,
		`{"type":"PLANNER_RESPONSE","model":"gemini-3.8-flash-high"}`+"\n")
	transcript(t, logs(filepath.Join(gemini, "antigravity-ide"), conv), earlier, strings.Join([]string{
		`{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","model":"gemini-4-carbon-medium"}`,
		`{"step_index":2,"source":"USER","type":"USER_INPUT"}`,
		`{"step_index":3,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","model":"claude-opus-5-5-medium"}`,
		`{"step_index":4,"type":"CHECKPOINT"}`,
		`{"step_index":5,"type":"PLANNER_RESPONSE","model":""}`,
	}, "\n")+"\n")
	if m := e.modelOf(t); m != "claude-opus-5-5-medium" {
		t.Errorf("detected model = %q", m)
	}

	// ANTIGRAVITY_APP_DATA_DIR names the directory, if it is absolute. A
	// line may be longer than any chunk that detection reads.
	app := filepath.Join(e.home, "app-data")
	long := `{"type":"PLANNER_RESPONSE","tool_calls":"` + strings.Repeat("x", 300<<10) + `","model":"gemini-4-carbon-high"}`
	transcript(t, logs(app, conv), earlier, long+"\n"+`{"type":"USER_INPUT"}`+"\n")
	e.vars["ANTIGRAVITY_APP_DATA_DIR"] = app
	if m := e.modelOf(t); m != "gemini-4-carbon-high" {
		t.Errorf("with ANTIGRAVITY_APP_DATA_DIR = %q", m)
	}
	e.vars["ANTIGRAVITY_APP_DATA_DIR"] = "app-data"
	if m := e.modelOf(t); m != "claude-opus-5-5-medium" {
		t.Errorf("with a relative ANTIGRAVITY_APP_DATA_DIR = %q", m)
	}
}

func TestDetectedModelQuietly(t *testing.T) {
	const conv = "dddd4444-0000-4000-8000-000000000007"
	e := newEnv(t)
	app := filepath.Join(e.home, "app-data")
	e.vars["ANTIGRAVITY_APP_DATA_DIR"] = app
	e.vars["ANTIGRAVITY_CONVERSATION_ID"] = conv
	path := filepath.Join(app, "brain", conv, ".system_generated", "logs", "transcript.jsonl")
	model := `{"type":"PLANNER_RESPONSE","model":"gemini-4-neon-xhigh"}` + "\n"
	transcript(t, path, earlier, model)
	if m := e.modelOf(t); m != "gemini-4-neon-xhigh" {
		t.Fatalf("detected model = %q", m)
	}

	// Only the last 16 MiB are read.
	filler := strings.Repeat(`{"type":"USER_INPUT","content":"`+strings.Repeat("y", 1000)+`"}`+"\n", 17<<10)
	transcript(t, path, earlier, model+filler)
	if m := e.modelOf(t); m != "" {
		t.Errorf("past the tail = %q", m)
	}
	transcript(t, path, earlier, filler[:15<<20]+"\n"+model)
	if m := e.modelOf(t); m != "gemini-4-neon-xhigh" {
		t.Errorf("after 15 MiB = %q", m)
	}

	// A transcript that isn't there or can't be read gives none, and
	// nothing on standard error, which modelOf checks.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if m := e.modelOf(t); m != "" {
		t.Errorf("without a transcript = %q", m)
	}
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	if m := e.modelOf(t); m != "" {
		t.Errorf("with a directory for a transcript = %q", m)
	}
	// Nor do . and .. name a transcript.
	transcript(t, filepath.Join(app, ".system_generated", "logs", "transcript.jsonl"), earlier, model)
	transcript(t, filepath.Join(app, "brain", ".system_generated", "logs", "transcript.jsonl"), earlier, model)
	for _, s := range []string{".", ".."} {
		e.vars["ANTIGRAVITY_CONVERSATION_ID"] = s
		if m := e.modelOf(t); m != "" {
			t.Errorf("session %s = %q", s, m)
		}
	}
}

func TestDetectedModelOnPosts(t *testing.T) {
	const s = "eeee5555-0000-4000-8000-000000000008"
	e := newEnv(t)
	e.serve(t)
	e.vars["AGORA_AUTHOR"] = "human/alice"
	e.ok(t, "boards", "create", "proj")
	e.vars["AGORA_BOARD"] = "proj"
	delete(e.vars, "AGORA_AUTHOR")
	e.vars["CLAUDE_CODE_SESSION_ID"] = s
	path := filepath.Join(e.home, ".claude", "projects", "-home-proj", s+".jsonl")
	transcript(t, path, earlier, `{"type":"assistant","message":{"model":"claude-opus-5-5"}}`+"\n")

	var show struct {
		Posts []struct{ Author, Model string }
	}
	models := func() []string {
		t.Helper()
		e.json(t, &show, "show", "1")
		var m []string
		for _, p := range show.Posts {
			m = append(m, p.Model)
		}
		return m
	}
	// A detected session's posts carry its model; a person's, from the
	// same shell, don't.
	e.ok(t, "post", "--title", "Track", "-m", "status")
	e.ok(t, "reply", "1", "-m", "from a person", "--as", "human/alice")
	// A claim records the detected model, and a carrier's posts then carry
	// the model that it switched to, or, with --as, the claim's.
	var n struct{ Model string }
	e.ok(t, "claim", "e2e")
	if e.json(t, &n, "claim"); n.Model != "claude-opus-5-5" {
		t.Errorf("claim's model = %q", n.Model)
	}
	appendLines(t, path, earlier, `{"type":"assistant","message":{"model":"claude-sonnet-5-5"}}`)
	e.ok(t, "reply", "1", "-m", "switched")
	e.ok(t, "reply", "1", "-m", "as the name", "--as", "claude-code/e2e")
	if got, want := strings.Join(models(), ","), "claude-opus-5-5,,claude-sonnet-5-5,claude-opus-5-5"; got != want {
		t.Errorf("models = %s, want %s", got, want)
	}
}

func TestModelFlagHelp(t *testing.T) {
	e := newEnv(t)
	r := e.run("whoami", "-h")
	if want := "default $AGORA_MODEL, else the model in a detected session's transcript"; !strings.Contains(r.err, want) {
		t.Errorf("whoami -h: exit %d, want %q in %q", r.code, want, r.err)
	}
}
