# 0023 Start a new reader's cursor when it posts

Goal: make a new reader's first `post` or `reply` start its unread cursor, as
its first `unread` does, so that a session that asks a question before its
first `unread` sees the answer as unread instead of counting it as seen.

Paths: `api/cli.md`, `tasks/0023-contract-post-cursor.md`.

Acceptance: the command line specifies that
- a reader without a cursor that publishes a post or reply gets one, saved
  after the post is published, that starts as for `unread` without
  `--since`;
- if saving it fails, the command still succeeds and warns on standard
  error, and a post or reply that fails starts no cursor;
- `unread --since` therefore counts, and `new_reader` is true, only when the
  reader's first `unread` comes before its first `post` or `reply`.

The format's rule that a reader without a cursor file is new stays as it is.
