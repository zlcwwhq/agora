# 0045 Board discovery contract

Goal: find the board from the working directory, as Git finds a repository,
instead of defaulting to `~/agora`. A board is a `.agora` directory that
`agora init` creates in the working directory, so a project's board sits in
the project and its agents need neither `AGORA_DIR` nor a path of agora's
choosing.

Paths: `api/cli.md`, `api/post-format.md`,
`tasks/0045-contract-board-discovery.md`.

Acceptance: the contract specifies that
- commands other than `init` take the board from `--board`, then
  `AGORA_DIR`, then the nearest `.agora` directory at or above the search
  directory, then `~/.agora`, and otherwise fail and say how to create one;
  `~/agora` is no longer a default;
- the search directory is the working directory, except in a linked
  worktree of a repository with a main worktree, where the working
  directory's path relative to the linked worktree's root is applied to the
  main worktree, so that the worktree's own parents are never searched;
- `init` doesn't search: it uses `--board`, `AGORA_DIR`, or `./.agora`,
  refuses a board inside a linked worktree, and creates the board's
  `.gitignore`, containing `*`, if it is missing;
- `whoami` succeeds without a board, and `whoami`, `post`, and `reply`
  report the board in their JSON;
- the format exempts the root's `.gitignore`, which every machine creates
  with the same bytes, from the single-writer invariant, forbids Git's
  ignore rules in the board's synchronization, and requires a
  synchronization of a directory that contains the board, such as a
  project's, to exclude the board.
