# 0006 Opening a busy index

Goal: open a new index from several processes at once without failing.

Paths: `internal/index/index.go`, `tasks/0006-index-busy.md`.

Acceptance:
- Opening retries with jitter while the database is busy or locked, for up to
  15 seconds.
- TestConcurrentOpenCreatesTheSchemaOnce, which failed intermittently,
  passes repeatedly.
