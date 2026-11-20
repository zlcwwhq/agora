# 0064 Boards

Goal: implement the contracts of 0063: one server keeps several boards,
each numbering its posts from 1, and commands work on the board that
`--board`, `AGORA_BOARD`, or `[board] id` names.

Paths: `internal/post/`, `internal/store/`, `internal/server/`,
`internal/client/`, `internal/config/`, `internal/cli/`, `README.md`,
`tasks/0064-boards.md`.

Acceptance:
- `internal/store` keeps boards and posts as schema version 2 says: each
  board gives its posts the ids 1, 2, … and never gives one again, even
  after deleting its newest post; references, backlinks, tags, lists,
  searches, and unread cursors stay within a board; every board-scoped call
  on an unknown board fails as not found; creating a taken id fails as a
  conflict; a version 1 database is refused.
- `internal/server` answers `/api/boards` and `/api/boards/BOARD/...`, with
  400 for an invalid board id, 404 for an unknown one, and 409 for a taken
  one, and serves `/` as the list of boards, with a form that creates one
  for a person, and each board's pages under `/b/BOARD/`.
- `internal/config` reads `[board] id` and applies `AGORA_BOARD` and
  `--board` over it, refusing invalid ids; `init --board` writes it.
- `internal/cli` adds `--board`, `agora boards`, and `agora boards create`,
  shows the board in `whoami` and `init`, prints addresses under
  `/b/BOARD/`, and fails without a board, saying how to name one.
- Tests are self-contained, as before.

Changed tests: the behavior that 0063 changes moves existing tests, which
keep what they check otherwise.
- `internal/server/pages_test.go`: every page is now under `/b/BOARD/`, and
  `/` lists boards, so each test uses the new addresses, and
  `TestIndexListsAndFilters` becomes `TestBoardListsThreadsAndFilters`.
- `internal/server/api_test.go`: the endpoints move under
  `/api/boards/BOARD/`, and the helper creates the board that tests use.
- `internal/store/store_test.go`: every call names its board, which the
  helper creates, and the queries that inspect tables use `pk` and `n`.
- `internal/config/config_test.go`: `Load` and `Write` take the board.
- `internal/cli/cli_test.go`: commands need a board, so `TestEndToEnd`
  creates one, and `TestIdentityOrder` and `TestUnreachableServer` name
  one, so that they still reach the failure they test.
