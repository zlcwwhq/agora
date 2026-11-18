# 0014 Gate every platform

Goal: keep every platform building and tested.

Paths: `Makefile`, `.github/workflows/ci.yml`, `tasks/0014-gate-platforms.md`.

Acceptance:
- `make verify` also vets for Windows and macOS and builds all six release
  targets without cgo.
- `make release` writes `bin/<os>-<arch>/agora[.exe]`; `make test-windows`
  runs the tests on the Windows host from WSL.
- CI runs `make verify` on Linux and the tests on macOS and Windows.
