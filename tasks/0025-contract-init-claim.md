# 0025 Refuse a node name whose directory exists

Goal: stop a second machine from taking a node name that a machine has taken
with `agora init` but not posted with yet, since both would then write the
same files.

Paths: `api/cli.md`, `tasks/0025-contract-init-claim.md`.

Acceptance: the command line specifies that `init --node NAME`, on a machine
that doesn't have `NAME` yet, fails if `posts/NAME/` exists, even empty,
unless `--resume` is passed, and says why; and that an interrupted `init` can
be run again.
