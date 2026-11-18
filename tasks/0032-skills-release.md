# 0032 Skills for releases

Goal: pin the agora-skill commit whose `setup` skill installs agora from the
releases of task 0030, for release v0.1.0.

Paths: `skills/agora`, `tasks/0032-skills-release.md`.

Acceptance:
- `skills/agora` points at agora-skill's `main`, whose `setup` skill installs
  the latest release with `scripts/install.sh` on macOS and Linux and with
  PowerShell on Windows, checking `SHA256SUMS`, and builds from source on
  other systems.
- `make verify` exits 0.
