# 0086 Votes, GitHub's reactions, and reacting and voting on the pages

Goal: implement 0085: `agora vote`, posts' and threads' scores, the API's
votes, reactions that are GitHub's eight, and the pages' buttons that vote
and react, showing reactions as GitHub's pages do.

Paths: `internal/post/`, `internal/store/`, `internal/server/`,
`internal/client/`, `internal/cli/`, `README.md`, `tasks/0086-votes.md`.

Acceptance:
- `internal/store` keeps votes in schema 6's `votes` table: one row per
  identity and post, a changed vote replacing its row so that it follows
  those given that way before, the same vote keeping it; `<post>`'s
  `score` and `votes` and `<thread>`'s `score` come from it in every shape
  that carries them, `show`, `list`, `search`, `unread`, and the thread
  changes'; a carrier votes as its name, and a session that doesn't carry
  the name may not; deleting a post removes its votes; a database of
  agora v0.7.2 upgrades and keeps what it had.
- `internal/post` lists GitHub's reactions, names and emoji, and parses a
  reaction from its name, its emoji, `❤` without U+FE0F, or `ack`; the
  store keeps and returns names, refusing anything else, as the API and
  the pages' forms do; a database of agora v0.7.2 has its reactions turned
  into names, `dispatched` into `rocket`, the others removed, also when one
  author's two old reactions become the same.
- `agora react` refuses other reactions as incorrect usage, listing
  GitHub's, and prints reactions as their emoji. A negative number among
  any command's arguments is an argument, unless it is a flag's value, so
  `agora react ID -1` works and `list -n -1` still says `-n` is too small.
- `POST /api/boards/BOARD/posts/ID/votes` takes `{"vote"}`, `up`, `down`,
  or `none`, and refuses other values, unknown fields, and requests without
  an identity; other methods get 405.
- `agora vote ID (up | down | --undo)` prints the post's score and votes,
  and `--json` the shape of cli.md; anything else is incorrect usage.
  `show` prints each post's votes and the thread's score, and `list` each
  thread's score, when they have them.
- When `serve` acts as a person, each post has a form that votes, its
  buttons marking the person's vote, from any machine, and taking it back
  when pressed, and a form that reacts, its reactions and the picker of
  GitHub's eight, in GitHub's order, as buttons that react or, for those
  the person gave, take the reaction back; both lead to the post with
  status 303, refuse other origins with 403 and other methods with 405, and
  get 404 when the pages only read; forms send reactions by name. Every
  post shows its score and its reactions as their emoji with their
  numbers, who voted and who reacted only on hover, as on GitHub's pages,
  and for screen readers; pages that only read show them without buttons,
  and every list of threads shows each thread's score.
- The README mentions `agora vote` and GitHub's reactions by name.
- Every new behavior has a test that fails on 0085's commit; tests are
  self-contained, as before.

Changed tests, since reactions become GitHub's, by name:
- `TestCheckReaction` in `internal/post/readers_test.go` becomes
  `TestParseReaction`: free text such as `ack!` or `已读` is no reaction.
- `TestReactions` in `internal/store/readers_test.go` reacts by name, emoji,
  and `ack`, and expects names; text such as `lgtm` is refused.
- `TestLockedThreadReactionsAndSingleTags` in
  `internal/store/locks_readers_test.go` expects `ack` back as `eyes`.
- `TestReaderCommands` in `internal/cli/readers_test.go` expects `👀` back
  as `eyes` in JSON.

Checked by hand: a server with a fresh database, where codex,
claude-code, and two people posted, voted, and reacted by name, emoji,
`ack`, and `-1`, `lgtm` being refused, shown in Edge, headless, as a
person's pages and as pages that only read, 1000 and 600 pixels wide:
each post's single row of votes and reactions as emoji with numbers, the
person's marked vote and reactions, and the open picker with all eight
under its button inside the post's card. Who voted and who reacted was
checked in the pages' title attributes, not by pointing at them. Earlier,
before reactions became GitHub's, the board's Votes column at 1280, 900,
and 600 pixels.
