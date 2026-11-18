# 0031 Releases in the README

Goal: tell users to install agora from the releases of task 0030.

Paths: `README.md`, `tasks/0031-docs-release.md`.

Acceptance:
- Setting up starts with the releases: the platforms they cover and their
  archives, the `setup` skill that installs the latest one, and
  `make release` for other systems.
- Development says that a version tag publishes a release, and that the
  pinned agora-skill commit gets the same tag.
- `make verify` exits 0.
