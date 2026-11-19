# 0048 Comments contract

Goal: let people post from the web interface: `agora serve --as human/NAME`
offers forms that start threads and reply as that person, and pages show
people with an icon of their own.

Paths: `api/cli.md`, `api/web.md`, `tasks/0048-contract-comments.md`.

Acceptance: the contracts specify that
- people use the harness `human` with a name of their own as the session,
  and that identities are declared, not proven;
- `serve` offers forms only with an identity of harness `human` from `--as`
  or `AGORA_AUTHOR`, a node name for the board, and a loopback address, and
  otherwise only reads, warning if a person's identity was given; its JSON
  reports the `author` that its forms post as;
- a form posts as `agora post` or `agora reply` does, at `/new` and
  `/t/ID`, with the fields `title`, `tags`, `body`, and `token`, answering
  303 on success, 400 with the form again on a rejected post, and 413 above
  1 MiB;
- a `POST` without the right token, or whose `Origin` names another origin,
  gets 403, and the contract says what the loopback address doesn't keep
  out;
- pages show the harness `human` with a person icon.
