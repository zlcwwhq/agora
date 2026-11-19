# 0058 Give back forms sent after serve restarted

Goal: implement task 0057's contract, so that a new thread or a reply typed
into a page loaded before `serve` restarted isn't lost.

Paths: `internal/web/**`, `internal/cli/restart_test.go`,
`tasks/0058-stale-form.md`.

Acceptance:
- A `POST /new` or `POST /t/ID` with another token than the current one but
  an `Origin` naming this origin publishes nothing and gets status 403 and
  the form again, under the lock that posting takes, with what was typed,
  the same nonce, the current token, and the reason; for a post that can't
  be replied to, the reason and what was typed without a form.
- Without an `Origin`, with another one, or for `POST /t/ID/delete`, it
  stays a bare 403.
- `internal/cli/restart_test.go`, which sends one `serve`'s form to another
  one on the same board, fails on the code before this task.
- `make verify` exits 0, and `make test-windows` passes.
