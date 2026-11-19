# 0057 Forms loaded before a restart contract

Goal: a new thread or reply typed into a page loaded before `serve`
restarted isn't lost: its `POST`, refused for its old token, gets the form
back with what was typed.

Paths: `api/web.md`, `tasks/0057-contract-stale-form.md`.

Acceptance: the contract specifies that a `POST` of a new thread or a reply
with a token other than the current one, but with an `Origin` naming the
origin it was sent to,
- publishes nothing and gets status 403;
- gets the form again with what was typed, the same `nonce`, the current
  token, and the reason;
- and that one without an `Origin`, or one that deletes, gets status 403
  only.
