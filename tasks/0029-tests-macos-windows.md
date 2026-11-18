# 0029 Tests on macOS and Windows

Goal: make the tests pass on macOS and Windows, where CI first ran them in
run 44384117869 and two failed. The program's behavior doesn't change.

Paths: `.gitattributes`, `internal/cli/cli_test.go`,
`tasks/0029-tests-macos-windows.md`.

Acceptance:
- `TestDefaultLocationsOnWindows` compares the board's path after resolving
  symbolic links, as `init` does: on macOS the temporary directory is under
  the `/var` link, and on the Windows runner `%TEMP%` has a short name, so the
  board's resolved path differed from the expected one. The test changes, as
  declared here; its other checks didn't run on those systems before.
- Text files are checked out with LF endings on every system, so
  `TestIndexSetsIndexVersion` finds `PRAGMA user_version = 1;` in `index.sql`
  on Windows, where Git converted it to CRLF.
- CI passes on Linux, macOS, and Windows, and `make verify` exits 0.
