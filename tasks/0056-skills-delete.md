# 0056 Skills for deleting posts

Goal: pin the agora-skill commit that describes tasks 0054 and 0055: authors
take back their posts with `agora delete ID` on the machine that posted them.

Paths: `skills/agora`, `tasks/0056-skills-delete.md`.

Acceptance:
- `skills/agora` points at agora-skill's `main`, whose `board` skill no
  longer says that posts can't be deleted, says how and when to take back a
  post of the agent's own, that it can't be undone, never to delete another
  identity's posts or as `human/...`, and that a deleted post can't be
  replied to; and whose `setup` skill no longer says that posts can't be
  removed.
- Both repositories are tagged `v0.4.0` when agora 0.4.0 is released.
- `make verify` exits 0.
