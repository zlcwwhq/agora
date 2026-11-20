# 0068 Name versions by release

Goal: implement 0067: messages, comments, and the README call the boards
that were `.agora` directories those of agora v0.4 and earlier, and no
longer speak of agora 1 and agora 2.

Paths: `internal/config/`, `internal/store/`, `internal/cli/cli_test.go`,
`internal/server/pages_test.go`, `README.md`, `tasks/0068-naming.md`.

Acceptance:
- a `.agora` directory is refused as a board of agora v0.4 or earlier,
  which this agora can't read;
- a schema version 1 database is refused as a development build's;
- `git grep` finds "agora 1" and "agora 2" only in the task files of
  earlier tasks, which record what was done at the time.

Changed tests: `TestOldBoardDirectory` in `internal/cli` and the old board
case of `internal/config` look for the new message, and the version that
the command line and page tests give is `v0.5.0-test` instead of
`v2.0.0-test`; what they check is otherwise unchanged.
