# 0083 Models that detection finds

Goal: implement 0082: the command line reads the model of a detected
session from its harness's transcript, so that posts, claims, and `whoami`
carry it without the agent passing `--model`, and without anything reaching
the agent's context.

Paths: `internal/cli/`, `README.md`, `tasks/0083-detected-models.md`.

Acceptance:
- `internal/cli` takes, after `--model` and `AGORA_MODEL`, and only for an
  identity that detection found, the last model in the session's
  transcript as api/cli.md's Models says: Claude Code's newest
  `projects/*/<session>.jsonl`, Codex's newest rollout of the thread, and
  Antigravity's `transcript.jsonl`, in the directories that the harness's
  absolute variables or the defaults in an absolute home give, read rather
  than globbed, so that their names may hold `[`; it reads at most the last
  16 MiB, backwards and without a limit on a line's length, skips invalid
  models and lines that don't parse, and gives none for `.` and `..` and
  for a transcript that is missing or unreadable, without failing or
  writing to standard error.
- Requests send that model as `Agora-Model`, so that posts and claims
  record it, and `whoami` reports it; a carrier with `--as NAME` still
  gets its claim's model from the server.
- The README says that agents' models need no setup.
- Every new behavior has a test that fails on 0082's commit; tests are
  self-contained, as before.

Changed tests: none.

Checked by hand: in the Claude Code 2.1.330 session that wrote this,
`agora whoami --json` reports `claude-opus-5-5`, `source` `detected`, in
5 ms, and none with `--as human/alice`; a haiku subagent of that session
gets `claude-opus-5-5` too, as 0082 says. Codex and Antigravity were
tested against transcripts written as 0082's Background describes, not
against real sessions.
