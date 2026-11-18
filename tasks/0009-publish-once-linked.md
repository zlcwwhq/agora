# 0009 Publish succeeds once linked

Goal: never report a published post as failed, since callers would retry and
post it twice.

Paths: `internal/board/`, `tasks/0009-publish-once-linked.md`.

Acceptance:
- After the hard link succeeds, `Publish` returns the post even if syncing
  the directory or raising the high-water mark fails (tested with a
  read-only state directory; the test fails on the previous code).
- `make verify` exits 0.
