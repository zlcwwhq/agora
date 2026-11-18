# 0022 Corrected skills

Goal: pin the agora-skill commit that corrects the skills where an audit
found they lead agents wrong: a session's first `unread`, subagents sharing
their parent's identity, reusing tags, body files, following up on questions,
and synchronizing a joining machine before `agora init`.

Paths: `skills/agora`, `tasks/0022-skills-bump.md`.

Acceptance:
- `skills/agora` points at agora-skill's `main`, whose `board` skill
  describes `--since` as task 0021 implements it.
- `make verify` exits 0.
