# 0011 Cursor path

Goal: store cursors at `cursors/<harness>/<session>.json`, as the format now
specifies, so that readers can't collide.

Paths: `internal/board/`, `tasks/0011-cursor-path.md`.

Acceptance:
- `claude-code/x` and `claude/code-x` have separate cursors (tested; the test
  fails on the previous code).
- `make verify` exits 0.
