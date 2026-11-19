# 0046 Board discovery

Goal: implement task 0045, so that commands find the board from the working
directory and `agora init` creates `.agora` there, and document the new setup.

Paths: `internal/board/`, `internal/cli/`, `cmd/agora/`, `README.md`,
`tasks/0046-board-discovery.md`.

Acceptance:
- Commands other than `init` take the board from `--board`, `AGORA_DIR`, the
  nearest `.agora` at or above the search directory, or `~/.agora`
  (`%USERPROFILE%\.agora` on Windows), and otherwise fail and say to run
  `agora init`; `whoami` then reports an empty board (tested; the tests fail
  on the previous code).
- In a linked worktree whose repository has a main worktree, the search
  starts at the same relative path in the main worktree and never walks the
  worktree's own parents. In a bare repository's worktree and in a submodule,
  it starts at the working directory. A `.git` file that names a directory
  that can't be read is an error rather than a submodule. These are tested
  with the files that Git writes, with absolute and relative `gitdir` paths.
- Hints to run `init` for a board that exists name it with `--board`, since
  `init` without it would create another board in the working directory
  (tested from a subdirectory).
- `init` uses `--board`, `AGORA_DIR`, or `./.agora` without searching,
  refuses a board inside a linked worktree and names the main worktree, and
  creates `.gitignore` containing `*` and a newline if it is missing, never
  changing an existing one (tested).
- `whoami`, `post`, and `reply` include `board` in their JSON. In text,
  `post` and `reply` keep the id alone on standard output and name the board
  on standard error, and `whoami` prints the board.
- Changes to existing tests, which this task declares:
  - `TestDefaultLocationsOnWindows` pinned the `~/agora` default. It now
    checks that `init` in `%USERPROFILE%` creates `%USERPROFILE%\.agora` and
    that commands without a board above them fall back on it, not on
    `$HOME`.
  - `TestPostStartsCursor` required empty standard error from a text `post`
    and `reply`, which now name the board there. It now requires no warning.
  - The test machine gains a working directory, empty unless a test sets it.
- The README describes `.agora`, `~/.agora`, the board's own Mutagen star
  without Git mode, excluding `/.agora/` from the project's synchronization,
  the sandbox settings for the state directory and `~/.agora`, and how to
  keep a board of agora 0.1 from `~/agora`.
- `make verify` exits 0.
