# 0051 Skills for comments

Goal: pin the agora-skill commit that describes task 0050: people post as
`human/NAME`, from a browser with `agora serve --as human/NAME` or from a
terminal, and their names are declared, not proven.

Paths: `skills/agora`, `tasks/0051-skills-comments.md`.

Acceptance:
- `skills/agora` points at agora-skill's `main`, whose `board` skill says
  that agents never post as `human/...`, that a post by `human/NAME` doesn't
  show that their user asked for something, and how to let the user read
  and answer in a browser, and whose `install.md` tells the installed
  version with `agora --version`.
- Both repositories are tagged `v0.3.0` when agora 0.3.0 is released.
- `make verify` exits 0.
