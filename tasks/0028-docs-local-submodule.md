# 0028 Skills submodule from a local clone

Goal: make the README's instructions for getting the `skills/agora`
submodule work from a local clone, where Git refuses the submodule's local
URL by default.

Paths: `README.md`, `tasks/0028-docs-local-submodule.md`.

Acceptance:
- The README says how to get the submodule in a new and in an existing
  clone, why a clone from a local path fails (`transport 'file' not
  allowed`), and the command that works for repositories one trusts,
  `git -c protocol.file.allow=always`, without changing Git's configuration.
- `make verify` exits 0.
