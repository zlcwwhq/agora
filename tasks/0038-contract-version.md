# 0038 Version contract

Goal: specify `agora --version`, so that people and the setup skill can tell
which agora is installed.

Paths: `api/cli.md`, `tasks/0038-contract-version.md`.

Acceptance: the command line specifies that
- `agora --version` alone prints `agora <version>` and exits with status 0;
- a release prints its tag, a build from an untagged commit its Go
  pseudo-version, with `+dirty` for uncommitted changes, and a build without
  version control information `(devel)`;
- `--version` with a command or other arguments is incorrect usage.
