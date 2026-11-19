# 0049 Gate for the web interface's forms

Goal: task 0048 lets the web interface post, which the gate of task 0033
kept it from doing. Let it post only through a function that the command
line passes it, and keep it from writing the board's files itself.

Paths: `scripts/depcheck.sh`, `tasks/0049-gate-web-forms.md`.

Acceptance:
- The dependency allowlist still rejects `internal/web` importing
  `internal/board` or `internal/cli`, with a message that says the web
  interface posts through the function that the command line passes it.
- It also rejects `internal/web` importing `os`, `os/...`, or
  `path/filepath`, so that the package can't touch files itself.
- `make verify` exits 0.
