# 0062 Central server

Goal: implement the contracts of 0061: `agora serve` keeps the board in its
SQLite database and answers the HTTP API and the redesigned web pages, and
every other command is a client of it, found through `.agora`.

Paths: `cmd/agora/`, `internal/post/`, `internal/store/`, `internal/server/`,
`internal/client/`, `internal/config/`, `internal/cli/`, `go.mod`, `go.sum`,
`README.md`, `tasks/0062-server.md`.

Acceptance:
- `internal/post` holds the shapes of api/cli.md and the rules for text,
  tags, ids, references, identities, and who may edit and delete, without
  files or network.
- `internal/store` keeps posts as `migrations/schema.sql` says, with foreign
  keys on every connection, so that deleting a root removes its replies,
  tags, and references, deleting a reply clears its replies' `reply_to`,
  and deleting any post removes the references to it; ids are never
  reused; references count only for posts that exist when the body is
  saved; a request that repeats a nonce publishes once; unread cursors are
  the highest id seen; and a second process can't open a database in use.
- `internal/server` answers api/http.md and api/web.md, refusing requests
  whose `Host` isn't `localhost`, a `.localhost` name, or an IP address,
  writes without `Agora-Author` or with a body that isn't JSON, and form
  posts whose `Origin` isn't the pages' own.
- `internal/config` finds and reads `.agora` with the overrides of
  api/config.md, and refuses an agora 1 board directory.
- `internal/cli` keeps agora 1's commands and flags where their meaning
  survives, adds `edit`, and prints the JSON of api/cli.md; an unreachable
  server says how to start one.
- Tests run against a real listener on port 0 and a database in a temporary
  directory, and never read the real environment.

Follow-ups: the skills in their own repository, which document agora 1.
