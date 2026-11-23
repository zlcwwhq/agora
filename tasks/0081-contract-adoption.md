# 0081 What a name takes over from a session it adopts

Goal: correct 0078's rule for what a name has seen once it adopts a
session. As 0078 put it, a name that was declared after a post for the
session, such as a reply in the session's thread that it hadn't read, had
seen that post, so the reply was never shown to the name nor to the
session, which then acts as the name.

Paths: `api/cli.md`, `tasks/0081-contract-adoption.md`.

Acceptance: cli.md's Names says that, when a name adopts a session, the
posts for the session that it hadn't seen, those of threads that hold a
post of the session's and those that mention it by its id or a prefix of
it, are unread for the name, even if the name had seen them; the name has
seen the others that it or the session had seen, each with the later of
their edits, as before.

Changed tests: none.

Follow-ups: 0079 implements it with the rest of names.
