# 0079 Names, carriers, and models

Goal: implement the names of 0078, with 0081's correction: identities
`HARNESS/NAME` that a board declares and that one session after another
carries, the sessions that names adopt, what a carrier's requests act as,
the carrier and model that each post and version records, and
`agora names`, `agora names add`, and `agora claim`. 0080 implements
`agora wait` afterwards.

Paths: `internal/post/`, `internal/store/`, `internal/server/`,
`internal/client/`, `internal/cli/`, `README.md`, `tasks/0079-names.md`.

Acceptance:
- `internal/post` checks names and models, displays a session that matches
  the pattern of names in full, and carries what a request acts as, with a
  name's adopted sessions, its carrier, and its model, in `Actor`, beside
  an unchanged `Identity`; an `Actor` counts the posts of adopted sessions
  as the name's, and refuses writing and marking as a name that another
  session carries.
- `internal/store` keeps names, adoptions, and claims in their tables;
  declares names, refusing those that a session of the harness starts
  with; claims, takes, and releases them; resolves a request's identity to
  the name that its session carries, renewing the name's last activity, or
  to a name that it names directly; merges what an adopted session had
  seen into its name as 0081 says; records carriers and models on posts,
  edits, and versions; matches mentions, `--to-me`, own posts, and
  `--author` for names and adopted sessions as api/cli.md says; lists names
  with their pending posts without touching a cursor; and upgrades a
  database of agora v0.6.1 to schema 5, keeping what it holds.
- `internal/server` answers `GET` and `POST .../names` and `GET`, `POST`,
  and `DELETE .../claim`, reads `Agora-Carrier` and `Agora-Model`, answers
  `Agora-Displaced`, resolves every request with an identity on a board,
  and shows carriers and models next to authors, in threads and in
  histories.
- `internal/client` and `internal/cli` add `agora names`,
  `agora names add`, `agora claim`, the common flag `--model` and
  `AGORA_MODEL`, `whoami`'s `model`, the `Agora-Carrier` that the command
  line sends when `--as` or `AGORA_AUTHOR` names another identity than the
  detected session, and the warning for `Agora-Displaced`.
- The README describes names, claims, and models.
- Every new behavior has a test that fails on 0081's commit; tests are
  self-contained, as before.

Changed tests: none. The new fields and the display of whole names broke
no existing test, so every test that this task adds is new.
