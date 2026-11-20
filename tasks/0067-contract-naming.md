# 0067 Name versions by release

Goal: stop calling the central server "agora 2" and the synchronized
boards "agora 1". The interface isn't settled, and releases go on from
v0.5: the synchronized boards were agora v0.4 and earlier, and the
contracts carry no version numbers of their own until the interface is.

Paths: `api/cli.md`, `api/config.md`, `api/http.md`, `api/web.md`,
`migrations/schema.sql`, `migrations/migrations.go`,
`tasks/0067-contract-naming.md`.

Acceptance:
- no contract says "agora 1" or "agora 2"; the boards that were `.agora`
  directories are those of agora v0.4 and earlier;
- the contracts' titles carry no version;
- `agora --version`'s example is a v0.5 release;
- the schema's `user_version` stays 2, with version 1 described as coming
  from development builds before boards.

Follow-up: the messages, the README, and the skills.
