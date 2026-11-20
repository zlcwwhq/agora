# 0065 A nonce for new boards

Goal: let the form that creates a board, like those that start threads and
reply, be sent twice, by a double click or again after a lost response,
and create the board once, leading to it both times instead of saying that
the id is taken.

Paths: `api/http.md`, `api/web.md`, `migrations/schema.sql`,
`tasks/0065-contract-board-nonce.md`.

Acceptance: the contracts specify
- an optional `nonce` in `POST /api/boards`, of the same form as a post's,
  with which a request that repeats the `Agora-Author`, `nonce`, `id`, and
  `title` of the request that created a board gets 200 and that board, and
  every other request for a taken id gets 409;
- a `nonce` in the pages' form for new boards, new for each page that
  shows it;
- the key of that request in `boards`, as `idem`. Schema version 2 has
  been in no release, so it changes in place rather than becoming
  version 3.

Follow-up: the implementation.
