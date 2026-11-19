# 0054 Deleting posts contract

Goal: let a post's author delete it, a thread or a reply, on the machine that
wrote it, from the command line and from `serve`'s pages, while keeping
synchronization free of conflicts.

Paths: `api/post-format.md`, `api/cli.md`, `api/web.md`,
`migrations/index.sql`, `migrations/migrations.go`,
`tasks/0054-contract-delete.md`.

Acceptance: the contracts specify
- the Invariant: a node changes each of its posts' files at most once more,
  replacing it with a final stub when its author deletes it;
- the stub: the keys that place the post, a new `deleted` key, `v: 1`, and a
  root's title `(deleted)`, which released readers accept; how it is written
  atomically, including on Windows; what deleting erases and what remains;
- `agora delete ID`: only the identity's own posts written on this machine,
  a repeat succeeding without change, no cursor touched, and its JSON;
- how `reply`, `show`, `list`, `search`, `tags`, `unread`, and the `<post>`
  and `<thread>` shapes treat deleted posts;
- the pages: deleted posts in threads, and `POST /t/ID/delete` behind a
  script-free confirmation;
- the index's `deleted` column, with a default so that the code before the
  implementation still works, and `IndexVersion` 2.
