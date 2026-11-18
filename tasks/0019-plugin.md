# 0019 Plugin

Goal: distribute the skills as a plugin for Claude Code and Codex, in the
layout of mutagen-remote-skill.

Paths: `skills/agora` (now a submodule), `.gitmodules`, `README.md`,
`tasks/0019-plugin.md`.

Acceptance:
- The skills live in the agora-skill repository, which is the plugin
  marketplace (`claude plugin validate .` passes), with `board` and `setup`
  skills.
- `skills/agora` is a submodule of it at a relative URL, which resolves both
  from a local clone and after both repositories are published side by side;
  a fresh `git clone --recurse-submodules` checks it out.
- The README gives the install commands and applies the board's star with
  `mutagen project apply`, as Mutagen's star documentation says.
