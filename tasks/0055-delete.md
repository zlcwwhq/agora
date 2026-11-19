# 0055 Deleting posts

Goal: implement task 0054's contract: `agora delete ID`, deleted posts in
every command and page, and the pages' delete form.

Paths: `internal/post/**`, `internal/board/**`, `internal/index/**`,
`internal/cli/**`, `internal/web/**`, `README.md`,
`tasks/0055-delete.md`.

Acceptance:
- `post.Post.Stub` and `Render` write the stub of `api/post-format.md`,
  which readers that ignore `deleted` read as a root titled `(deleted)`
  without text; `Parse` drops a deleted post's title, body, and tags.
- `board.Delete` replaces only this machine's posts of the given author,
  with a scratch file renamed over the post, retried on Windows while the
  file is held, and leaves a stub as it is.
- The index stores `deleted`; `list` leaves out threads whose every post is
  deleted, and threads count and date only posts that aren't deleted.
- `agora delete` refuses other authors' posts, other machines' posts, and
  posts that aren't here; succeeds again on a deleted post without changing
  it; and updates the index at once. `reply` refuses a deleted post;
  `unread` leaves deleted posts out but marks them seen; `show` and the
  JSON shapes carry `deleted`.
- `serve`'s pages show deleted posts without text and without reply links,
  offer the delete form, behind a `<details>` confirmation, only on the
  person's posts written on this machine, and answer `POST /t/ID/delete` as
  `api/web.md` says: 303, 403, 404, 400, and 405 for other methods. A refused
  reply to a deleted post shows the reason and what was typed. A thread's
  command-line hint replies to a post that isn't deleted, and a thread
  without one shows no activity, here and in `show`.
- The new tests fail on the code before this task.
- `make verify` exits 0, and `make test-windows` passes.
