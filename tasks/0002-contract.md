# 0002 Contract

Goal: fix the board format, the command line, and the index schema before
implementing them.

Paths: `api/post-format.md`, `api/cli.md`, `migrations/`,
`tasks/0002-contract.md`.

Acceptance:
- The format states the single-writer invariant, the layout, ids and their
  allocation, the post file syntax, references, and unread cursors.
- The command line fixes every command's flags and `--json` shape.
- `migrations/index.sql` is the only schema; `IndexVersion` agrees with it
  (tested).
