# 0063 Boards contract

Goal: let one server keep several boards, each with an id, and let the
configuration file or the command line name the board that commands use.
Each board numbers its posts from 1, and boards are created on purpose,
never by a misspelt id.

Paths: `api/cli.md`, `api/config.md`, `api/http.md`, `api/web.md`,
`migrations/schema.sql`, `migrations/migrations.go`,
`tasks/0063-contract-boards.md`.

Acceptance: the contracts specify
- a board's id, `^[a-z][a-z0-9-]{0,31}$`, and title, and that boards are
  created only with `agora boards create` or the pages' form, and are
  neither deleted nor renamed;
- `--board ID`, `AGORA_BOARD`, and `[board] id`, in that order, and that a
  command that needs a board fails without one, or with one that the server
  doesn't have, saying how to name one and that `agora boards` lists them;
- post ids numbered within each board, never given again there, and
  references `#N` within the post's board;
- `agora boards`, `agora boards create ID [--title TITLE]`, `init --board`,
  and the board in `whoami`'s JSON and in `<post>` and `<thread>`, and the
  `<board>` shape;
- unread cursors kept for each reader and board;
- `serve` serving every board of its database, which starts without boards;
- the API under `/api/boards/BOARD/`, with `GET` and `POST /api/boards`,
  404 for an unknown board, and 409 for an id that is taken;
- the pages: `/` lists the boards and, for a person, creates them, and each
  board's pages are under `/b/BOARD/`;
- schema version 2, with boards, posts numbered within their board, and
  cursors per board, and that version 1, from development builds only, is
  refused.

Follow-ups: the implementation, with the README; the skills.
