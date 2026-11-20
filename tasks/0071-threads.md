# 0071 Threads and their content

Goal: implement the parts of 0069 that concern threads and what posts
carry: open and resolved threads, pins and locks, `--state`, pages of lists
with `--skip`, attachments, moving posts between threads, and backups.
0070 implements the rest of 0069 at the same time.

Paths: `internal/post/`, `internal/store/`, `internal/server/`,
`internal/client/`, `internal/cli/`, `README.md`, `tasks/0071-threads.md`.

Acceptance:
- `internal/store` keeps each root's state, pin, and lock; changes them for
  the identities that api/cli.md allows, all or none; refuses replies,
  edits, deletions, and attachments in a locked thread to anyone but a
  person; lists pinned threads first in `list`; filters `list` and
  `search` by state; and skips the first `K` threads of either.
- `internal/store` keeps attachments, at most 10 MiB each, which only the
  post's author or a person adds, replaces, and removes, and which aren't
  edits; moves posts as api/cli.md's `move` says, for people only; and
  writes a consistent copy of the database for a backup. Deleting a post
  removes its attachments.
- `<thread>` carries `state`, `pinned`, and `locked`, and `<post>` carries
  `attachments`, empty as `[]`.
- `internal/server` answers `PATCH .../threads/ID`, `state` and `skip` in
  lists and searches, `PUT`, `GET`, and `DELETE` of
  `.../posts/ID/attachments/NAME`, `POST .../posts/ID/move`, and
  `GET /api/backup`; the board page filters by state, shows states, pins,
  and locks, and links to the next and previous pages; the thread page
  shows attachments and the person's buttons that change the thread;
  `/b/BOARD/a/ID/NAME` serves attachments with the headers of api/web.md;
  the forms for new threads, replies, and edits take files, the edit form
  detaches them and moves the post.
- `internal/cli` adds `agora thread`, `--state` and `--skip` for `list` and
  `search`, `-a` for `post`, `reply`, and `edit`, `edit --detach`,
  `agora download`, `agora move`, and `agora backup`, with their usage
  errors.
- Every new behavior has a test that fails on 0069's commit; tests are
  self-contained, as before.

Changed tests:
- `internal/server/pages_test.go`, `TestBoardListsThreadsAndFilters`: the
  board page's "Show more threads" link to `?n=51` became the "Next page"
  link to `?n=1&skip=1`, since api/web.md replaces the larger `n` with next
  and previous pages.
- `internal/server/pages_test.go`, `TestEditForms`: a reply's edit page
  lacks the edit form's title field, `id="f-title"`, rather than any
  `name="title"`, and has the move form's, since the edit page now holds
  the move form, whose `title` field api/web.md names.

Rebased onto 0070: a locked thread refuses agents' single tag changes, as
other edits, and keeps their reactions, which
`internal/store/locks_readers_test.go` tests.
