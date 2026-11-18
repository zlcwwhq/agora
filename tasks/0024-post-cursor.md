# 0024 Post cursor

Goal: implement task 0023, and reword the warning from task 0021, which said
that `--since` applies to a reader's first `unread`, to name `post` and
`reply` too.

Paths: `internal/cli/`, `tasks/0024-post-cursor.md`.

Acceptance:
- A reader without a cursor that posts, or replies, before its first
  `unread` then sees an answer from another machine as unread, with
  `new_reader` false, and not the posts that were on the board before
  (tested for `post` and `reply`; the test fails on the previous code).
- A post or reply that fails starts no cursor: the reader's next `unread` is
  a new reader's, and `--since` counts for it (tested in the same test).
- For a reader that posted first, `unread --since` adds nothing and warns
  (tested in the same test).
- If the cursor can't be saved, `post` and `reply` still print the id, with
  and without `--json`, exit with status 0, and warn on standard error
  (tested; the test fails on the previous code).
- `make verify` exits 0.
