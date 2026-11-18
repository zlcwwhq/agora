# 0039 Version

Goal: implement `agora --version` from task 0038.

Paths: `internal/cli/`, `cmd/agora/`, `tasks/0039-version.md`.

Acceptance:
- `agora --version` prints `agora <version>` and exits with status 0, where
  the version is the one that the go command stamps into the binary, read in
  `cmd/agora` and passed in `cli.Env`; `(devel)` if it is unknown (tested; the
  test fails on the previous code).
- `--version` with arguments, or after a command, exits with status 2
  (tested).
- A build of a tagged commit, also in the shallow checkout that the release
  workflow makes, prints the tag; a build of another commit prints its
  pseudo-version (checked by hand).
- `make verify` exits 0.
