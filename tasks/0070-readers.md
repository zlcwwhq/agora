# 0070 What readers see

Goal: implement the parts of 0069 that concern readers: mentions and
`unread --to-me`, `unread --thread`, the edits since a reader saw a post,
versions and `agora history`, reactions, `edit --add-tag` and
`--remove-tag`, and the upgrade of agora v0.5's databases to schema 3.
0071 implements the rest of 0069 at the same time.

Paths: `internal/post/`, `internal/store/`, `internal/server/`,
`internal/client/`, `internal/cli/`, `README.md`, `tasks/0070-readers.md`.

Acceptance:
- `internal/post` finds mentions as api/cli.md's Mentions says, with the
  bounds and code spans of references, and tells whether one names a
  reader.
- `internal/store` opens a schema 2 database by running
  `migrations.Upgrades` in the transaction that opens it and recording the
  mentions of the bodies that it holds; boards, posts, tags, references,
  and cursors survive, and the result is the schema that `schema.sql`
  creates.
- `internal/store` records mentions on post, reply, and edit; keeps each
  edit's replaced version and numbers edits per board; returns `history`;
  keeps reactions, once per author and reaction; applies `add_tags` and
  `remove_tags`; and answers unread with `to_me`, `thread`, and `edited`,
  marking seen what api/cli.md says. Deleting a post removes its versions,
  mentions, reactions, and seen marks.
- `<post>` carries `versions`, `mentions`, and `reactions`, empty arrays as
  `[]`.
- `internal/server` answers `GET .../posts/ID/history`,
  `POST .../posts/ID/reactions`, `add_tags` and `remove_tags` in `PATCH`,
  and `to_me` and `thread` in unread; the thread page links mentions and
  edited posts' versions and shows reactions; `/b/BOARD/t/ID/history` shows
  versions with their removed and added lines.
- `internal/cli` adds `unread --to-me` and `--thread`, shows edited posts,
  and adds `agora history`, `agora react`, and `edit --add-tag` and
  `--remove-tag`, with their usage errors.
- Every new behavior has a test that fails on 0069's commit; tests are
  self-contained, as before.

Changed tests: none. The new behavior is tested in new files,
`readers_test.go` in `internal/post`, `internal/store`, `internal/server`,
and `internal/cli`, with `internal/store/testdata/schema-2.sql`, agora
v0.5.0's `migrations/schema.sql`, for the upgrade; every existing test
passes unchanged.
