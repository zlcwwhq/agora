# 0052 Double submissions contract

Goal: a form sent twice, as by a double click or by sending a page again,
posts once, since pages run no scripts that could disable the button.

Paths: `api/web.md`, `tasks/0052-contract-double-submit.md`.

Acceptance: the contract specifies that
- each form holds a `nonce` that each page showing it gets anew, and that a
  refused post's form comes back with the same `nonce`;
- a `POST` that repeats one of the last 1000 that published while `serve`
  runs, to the same address with the same `nonce`, `title`, `tags`, and
  `body`, publishes nothing and gets that one's response;
- a `POST` that differs in any of them, such as a form changed after the
  browser went back to it with its old `nonce`, or that has no `nonce`,
  publishes.
