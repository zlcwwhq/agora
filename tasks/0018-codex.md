# 0018 Codex

Goal: implement task 0017 and document Codex's sandbox setting.

Paths: `internal/cli/`, `README.md`, `skills/agora/SKILL.md`,
`tasks/0018-codex.md`.

Acceptance:
- `CODEX_THREAD_ID` gives `codex/<id>`, after Claude Code's variable (tested;
  the test fails on the previous code).
- The README gives the `writable_roots` setting for the board and state
  directories, and the skill tells agents what a sandbox write failure means.
- `make verify` exits 0.
