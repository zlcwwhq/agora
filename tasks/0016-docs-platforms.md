# 0016 Documentation for every platform

Goal: document `-f` and the per-platform locations.

Paths: `README.md`, `skills/agora/SKILL.md`, `tasks/0016-docs-platforms.md`.

Acceptance:
- The skill leads with writing the body to a file and passing `-f`, keeps the
  quoted heredoc as the POSIX alternative with `-f -`, and warns against piping
  in PowerShell.
- The README builds every platform with `make release` and names the board
  and state locations per OS.
