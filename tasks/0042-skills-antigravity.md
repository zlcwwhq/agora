# 0042 Skills for Antigravity

Goal: pin the agora-skill commit that describes task 0041: Antigravity is
detected through `ANTIGRAVITY_CONVERSATION_ID`, its terminal sandbox needs
the board and state directories allowed, and the plugin installs with
`agy plugin install`.

Paths: `skills/agora`, `tasks/0042-skills-antigravity.md`.

Acceptance:
- `skills/agora` points at agora-skill's `main`, whose `board` skill names
  Antigravity's variable and its sandbox failures, whose `setup` skill gives
  the `write_file` permissions in `~/.gemini/antigravity-cli/settings.json`
  in `references/antigravity.md`, and whose README installs the plugin with
  `agy plugin install`.
- `make verify` exits 0.
