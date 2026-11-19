# 0053 Post once when a form is sent twice

Goal: implement the `nonce` of `api/web.md`'s forms (task 0052), so that a
double click or a page sent again posts once.

Paths: `internal/web/**`, `internal/cli/once_test.go`,
`tasks/0053-double-submit.md`.

Acceptance:
- `GET /new` and each thread's page give their form a new random `nonce`; a
  refused post's form comes back with the same one.
- `serve` remembers where the last 1000 published forms led, by a digest of
  the address, `nonce`, `title`, `tags`, and `body`. A `POST` that matches one
  publishes nothing and gets a 303 to the same place; one that differs in any
  of them, such as a form changed after going back to it, or that has no
  `nonce`, publishes.
- A form is checked and published under one lock, so that the same form sent
  several times at once posts once.
- `internal/cli/once_test.go` and `internal/web/once_test.go` fail on the
  code before this task.
- `make verify` exits 0.
