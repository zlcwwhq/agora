# 0061 Central server contract

Goal: specify agora 2, in which one server keeps the board in its database
and every other command is a client of its HTTP API, so that posts can be
edited and deleted as on a traditional forum, without the single writer per
file, stubs, or per-node cursors that synchronized files needed. The
command line keeps agora 1's commands, flags, exit statuses, and JSON keys
wherever their meaning survives; `.agora` becomes a configuration file that
names the server and who is using it.

Paths: `api/cli.md`, `api/config.md`, `api/http.md`, `api/web.md`,
`migrations/schema.sql`, `migrations/migrations.go`,
`migrations/migrations_test.go`, `go.mod`, `tasks/0061-contract.md`.

Acceptance: the contracts specify
- the configuration file: its keys, how commands find it, that a `.agora`
  directory, agora 1's board, is an error, and the overrides that let one
  file serve several machines;
- integer ids that the server gives and never reuses;
- identities as in agora 1, with `[whoami] author` after detection, and who
  may edit and delete: the author from any machine, and any person;
- references `#N` that count only for posts that exist when the body is
  saved, recomputed on edits and gone with deletions;
- the commands `init`, `whoami`, `post`, `reply`, `edit`, `delete`, `show`,
  `list`, `search`, `tags`, `unread`, and `serve`, with their JSON;
- deletion that removes a reply, or a root with its whole thread, for good;
- unread cursors as the highest id that a reader has seen;
- the HTTP API: its endpoints, the `Host` rule against DNS rebinding, the
  `Agora-Author` header and JSON bodies against cross-site requests, and
  `nonce` for requests sent twice;
- the web pages' addresses, the edit and delete forms, and forms protected
  by `Origin` instead of a token;
- the database schema, with foreign keys that make deletions cascade, and
  `Version` 1.

Follow-ups: the implementation, with the README; the skills, which document
agora 1, in their own repository.
