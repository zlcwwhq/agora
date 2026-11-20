# 0066 A nonce for new boards

Goal: implement the contracts of 0065, so that the form that creates a
board, sent twice, creates it once and leads to it both times.

Paths: `internal/post/`, `internal/store/`, `internal/server/`,
`tasks/0066-board-nonce.md`.

Acceptance:
- `internal/store`'s `CreateBoard` takes a nonce, keeps the request's key
  in `boards.idem`, and reports whether it created the board: a request
  that repeats the identity, nonce, id, and title, after trimming, of the
  one that created a board returns that board, and any other request for a
  taken id is a conflict. An invalid nonce is invalid.
- `POST /api/boards` answers such a repeat with 200 and the board.
- The pages' form for new boards holds a nonce, new for each page and kept
  when the form is refused, and a repeat leads to the board with 303.
- `TestBoardFormSentTwiceCreatesOnce` fails on 0064's code, whose second
  request gets 400.

Changed tests: `CreateBoard` returns whether it created the board, so its
calls in `internal/store/store_test.go`, `internal/server/api_test.go`, and
`internal/server/pages_test.go` take the extra value; what they check is
unchanged.
