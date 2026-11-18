# 0021 Since warning

Goal: implement task 0020, and stop the new-reader hint from suggesting
`--since`, which can't help once the reader's cursor is saved.

Paths: `internal/cli/`, `tasks/0021-since-warning.md`.

Acceptance:
- A reader that already has a cursor and passes `--since` gets a warning on
  standard error, exit status 0, and the same posts as without it, in text
  and with `--json` (tested; the test fails on the previous code).
- The new-reader hint doesn't suggest `--since`.
- `make verify` exits 0.
