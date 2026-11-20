# 0072 Temporary directories that Windows lets go

Goal: stop the Windows runs of the tests from failing, now in most runs,
with `TempDir RemoveAll cleanup: ... The directory is not empty` for
directories that are empty a moment later. Go's `t.TempDir` retries only
"Access is denied" and sharing violations, not `ERROR_DIR_NOT_EMPTY`.

Cause: outside the test process, and not identified. Three ways of holding
a file from user mode (a handle that shares deletion, a file mapping, and a
mapped view) all let Windows delete it at once, and 300 store cycles in a
row never failed. In 8 Windows runs of every test with `testdir`, 3
removals needed a second try, 2 to 3 ms later: two found the directory
empty, and one found `BOARD.DB-JOURNAL.tmp`, a name that neither agora nor
SQLite writes, made from SQLite's journal by another program.

Paths: `internal/testdir/`, `internal/cli/cli_test.go`,
`internal/config/config_test.go`, `internal/server/api_test.go`,
`internal/server/api_threads_test.go`, `internal/server/pages_test.go`,
`internal/store/backup_test.go`, `internal/store/readers_test.go`,
`internal/store/store_test.go`, `tasks/0072-windows-tempdirs.md`.

Acceptance:
- `testdir.New(t)` gives a new directory that is removed after the test and
  its subtests, retrying for up to 10 s while Windows reports a held file
  (access denied, sharing violation, or a directory that isn't empty), and
  logging the tries and what the first one left when there are several.
- Its retry loop is tested with injected errors on every system: it retries
  while the error is a held file's, stops on success, returns the last
  error at the timeout, and never retries other errors; on Windows, the
  errors that count as held are tested.
- No test calls `t.TempDir` any more.
- `make test-windows` passes in repeated runs.

Changed tests: every `t.TempDir()` in the files above becomes
`testdir.New(t)`, and nothing else in them changes. They test the same
behavior as before, so, unlike the tests of `internal/testdir`, they pass on
the merge base too: what this task fixes is their cleanup on Windows, which
the merge base fails in most Windows runs.

Follow-up: a check in the gate that tests don't call `t.TempDir`, with
`testdir.New` as its fix, so that new tests don't bring the failure back.
