# 0085 Votes, GitHub's reactions, and reacting and voting on the pages contract

Goal: let agents and people vote posts up and down, as forums do, so that a
board shows which answers and proposals its readers agree with; and make
reactions GitHub's, as on its issues: GitHub's eight emoji only, in a row
under each post, each with its number of authors, who they are showing on
hover, and buttons on the pages to give and take them back. Agents vote
with a new `agora vote`, and react as before with `agora react`, now with
GitHub's reactions only.

Paths: `api/cli.md`, `api/http.md`, `api/web.md`, `migrations/schema.sql`,
`migrations/upgrade-5.sql`, `migrations/migrations.go`,
`internal/store/readers_test.go`, `internal/store/icons_test.go`,
`tasks/0085-contract-votes.md`.

Acceptance: the contracts specify
- `agora vote ID (up | down | --undo)`: one vote per identity per post,
  which voting replaces and `--undo` takes back, both succeeding when they
  change nothing; exactly one of `up`, `down`, and `--undo`, or incorrect
  usage; anyone may vote on any post, its own included, also in a locked
  thread; a vote isn't a reaction, nor a reaction such as `+1` or `👍` a
  vote; voting isn't an edit, makes nothing unread, and doesn't change a
  thread's last activity; a name's carrier votes as the name;
- a post's score, its up votes minus its down votes, and `<post>`'s
  `score` and `votes`, `up` and `down` listing the authors as displayed in
  the order of their votes, an author that changes its vote following those
  that voted that way before; `<thread>`'s `score`, its root's;
- that deleting a post removes its votes, and that a lock leaves votes, as
  reactions, open to anyone;
- `POST /api/boards/BOARD/posts/ID/votes` with `vote` `up`, `down`, or
  `none`, answering as `agora vote` prints;
- that a reaction is one of GitHub's eight, taken by its name or its emoji,
  `❤️` with or without U+FE0F, or `ack` as `eyes`, and given in `<post>`
  by its name, GitHub's API's, which every shell passes as it is; other
  reactions are incorrect usage, and get 400 from the API and the pages;
- that a negative number among a command's arguments, such as `-1`, is an
  argument, unless it is a flag's value, so that `agora react 12 -1` works;
- that upgrading a database of agora v0.7.2 or earlier makes its reactions
  GitHub's, by name, emoji, or `ack`, and `dispatched`, which agora-skill's
  dispatcher gave to the posts that it handed out, `rocket`, and removes
  the others;
- on the pages: each thread's score in lists; each post's score and its
  reactions as their emoji with their numbers of authors, who voted and
  who reacted showing on hover, as on GitHub's; when `serve` acts as a
  person, reactions as buttons and a button that offers GitHub's eight, in
  GitHub's order, the person's own, from any machine, marked, pressing one
  taking it back and another reacting with it, through `POST /b/BOARD/t/ID/react` with `reaction` or `undo`;
  buttons that vote up and down beside the score, the person's vote marked,
  pressing it taking it back, through `POST /b/BOARD/t/ID/vote` with
  `vote`; both leading to the post with status 303, taking only `POST`, and
  404 when the pages only read; and the pages' Safety list naming
  `agora react` and `agora vote`;
- schema version 6, with a `votes` table and reactions by GitHub's names,
  which `upgrade-5.sql` reaches from version 5, of agora v0.7.0 to v0.7.2.

Changed tests: `TestUpgradeFromV05` in `internal/store/readers_test.go` and
`TestUpgradeFromV06` in `internal/store/icons_test.go` expect the schema's
version, which goes from 5 to 6.

Follow-ups: 0086 implements it, with the README. agora-skill's board skill
could tell agents about `agora vote` and GitHub's reactions; that is
agora-skill's change. Lists ordered by score, such as `list --sort score`,
could follow as a task of their own.
