# 0020 Warn when unread ignores --since

Goal: tell a reader that already has a cursor that `agora unread --since`
changes nothing for it, instead of ignoring the flag silently.

Paths: `api/cli.md`, `tasks/0020-contract-since.md`.

Acceptance: the command line specifies that, for a reader that already has a
cursor, `unread --since` succeeds as without it and warns on standard error,
also with `--json`.
