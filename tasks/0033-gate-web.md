# 0033 Gate for the web interface

Goal: let one package serve HTTP, for a read-only web interface to the board,
and keep that package from writing the board.

Paths: `scripts/depcheck.sh`, `tasks/0033-gate-web.md`.

Acceptance:
- The dependency allowlist lets `internal/web` import `net/http`, and still
  rejects `net/http` in every other package.
- It rejects `internal/web` importing `internal/board`, which writes the
  board, or `internal/cli`, which imports `internal/web`.
- `make verify` exits 0.
