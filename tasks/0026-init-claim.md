# 0026 Init claim

Goal: implement task 0025, recording the node before creating its directory
so that an interrupted `init` can be run again.

Paths: `internal/board/`, `internal/cli/`, `README.md`,
`tasks/0026-init-claim.md`.

Acceptance:
- On a machine without the name, `init` refuses a name whose directory exists
  but holds no posts, with an error that mentions `--resume`, writes no
  `node` file, and accepts the name with `--resume` (tested; the test fails on
  the previous code).
- An `init` that fails before recording the name leaves no `posts/NAME/`,
  and running it again succeeds, as it does after one that stopped between
  recording the name and creating the directories (tested; the test fails on
  the previous code, which created the directory first).
- The README and the help of `--resume` state the rule.
- `make verify` exits 0.
