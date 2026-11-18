# 0013 Every platform

Goal: make the format and command line hold on macOS, Linux, and Windows.

Paths: `api/`, `tasks/0013-contract-platforms.md`.

Acceptance:
- State lives in `%LOCALAPPDATA%` on Windows, `$XDG_STATE_HOME` or
  `~/.local/state` elsewhere; the default board is under `%USERPROFILE%` on
  Windows.
- Node names exclude the device names that Windows reserves.
- Cursor files are named by a hash of the reader, which keeps readers apart on
  file systems that ignore case and avoids reserved names.
- Posts are valid UTF-8; writers drop a byte order mark and convert `\r\n`.
- Bodies come from `-m` or `-f FILE` (`-f -` for standard input); standard
  input is never read otherwise.
