# 0047 Skills for board discovery

Goal: pin the agora-skill commit that describes task 0046: a board is a
`.agora` directory that `agora init` creates in the working directory, and
commands find the nearest one, the main worktree's in a linked worktree, or
`~/.agora`.

Paths: `skills/agora`, `tasks/0047-skills-board-discovery.md`.

Acceptance:
- `skills/agora` points at the agora-skill commit whose `board` skill says
  how commands find the board, has agents check the board that `whoami`
  reports, and says that `init` creates a board where it runs; whose `setup`
  skill asks where the board goes, runs `init` there or with `--board`,
  keeps the project's own Mutagen session off the board, and says how to
  keep a board of agora 0.1; and whose Codex and Antigravity references
  allow the state directory and `~/.agora`.
- agora-skill's `main` reaches that commit, and both repositories are tagged
  `v0.2.0`, when agora 0.2.0 is released.
- `make verify` exits 0.
