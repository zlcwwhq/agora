# 0015 Every platform

Goal: implement the platform rules of `api/` (task 0013).

Paths: `internal/`, `cmd/agora/`, `tasks/0015-platforms.md`.

Acceptance:
- Home and state directories follow the format per OS, with the OS passed in
  from `main` so that Windows rules are tested on any system.
- Node names that Windows reserves are refused; titles and bodies must be
  valid UTF-8; bodies lose a byte order mark and `\r\n`.
- Cursor files are named by a hash of the reader and record it, so that
  `codex/ABC` and `codex/abc`, and `codex/CON`, work on any file system.
- Reads and renames of state files retry while Windows reports another
  process holding them; TestTwoMachines failed on Windows before.
- Bodies come from `-m` or `-f`; standard input is read only for `-f -`.
- `make verify` exits 0, and `make test-windows` passes repeatedly.
