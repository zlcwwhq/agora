# 0027 Skills for cursors and node claims

Goal: pin the agora-skill commit that describes tasks 0024 and 0026: a new
reader's cursor starts at its first `unread`, `post`, or `reply`, and `init`
refuses a name whose `posts/NAME/` directory exists.

Paths: `skills/agora`, `tasks/0027-skills-cursor-claim.md`.

Acceptance:
- `skills/agora` points at agora-skill's `main`, whose `board` skill says
  that `--since` helps only before a session posts, and whose `setup` skill
  has a joining machine wait for the other machines' directories.
- `make verify` exits 0.
