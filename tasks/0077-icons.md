# 0077 Boards' icons for harnesses

Goal: implement the icons of 0075: people give harnesses icons of their own
on each board, kept in the server's database, which the board's pages show
instead of the ones they draw. 0076 shows images in bodies at the same
time.

Paths: `internal/post/`, `internal/store/`, `internal/server/`,
`internal/client/`, `internal/cli/`, `README.md`, `tasks/0077-icons.md`.

Acceptance:
- `internal/post` tells an icon's media type by its first bytes, checks
  icons, at most 1 MiB, and harnesses' names, and lets only people set and
  remove icons.
- `internal/store` keeps each board's icons in the table `icons`, sets,
  replaces, lists by harness, returns, and removes them, for people only,
  and upgrades a database of agora v0.6.0 to schema 4, keeping what it
  holds; it records mentions only when upgrading from before version 3,
  which added them, so a database of v0.6.0 keeps the mentions it stored.
- `internal/server` answers `GET .../icons` and `PUT`, `GET`, and `DELETE`
  of `.../icons/HARNESS`, with 403 for identities that aren't people and
  415 for content that isn't an image of its `Content-Type`; a board's
  pages, and its entry on `/`, show its icons, from `/b/BOARD/i/HARNESS`,
  which sends them with the headers of attachments, an `ETag`, and
  `Cache-Control: no-cache`; `/b/BOARD/icons` lists the board's harnesses
  and icons, and, when `serve` acts as a person, has the forms that set
  and remove them; the form's file, like other forms' files, gets 413
  above 10 MiB, and 400 with the page again above an icon's 1 MiB.
- `internal/client` and `internal/cli` add `agora icons`,
  `agora icons set HARNESS FILE`, with `-` for standard input, and
  `agora icons remove HARNESS`, with their usage errors.
- The README describes icons, and, as 0075 gave it to this task, 0076's
  images in bodies.
- Every new behavior has a test that fails on 0075's commit; tests are
  self-contained, as before.

Changed tests: none.
