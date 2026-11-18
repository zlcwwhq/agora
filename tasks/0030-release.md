# 0030 Releases

Goal: publish a GitHub release for each version tag, starting with v0.1.0,
with agora for macOS on arm64 and for Linux and Windows on amd64.

Paths: `.github/workflows/release.yml`, `.github/workflows/ci.yml`,
`tasks/0030-release.md`.

Acceptance:
- Pushing a tag `vX.Y.Z` (or `vX.Y.Z-PRERELEASE`, released as a
  prerelease) runs ci on its commit and, once it passes, publishes the
  release `vX.Y.Z` with `agora_darwin_arm64_vX.Y.Z.tar.gz`,
  `agora_linux_amd64_vX.Y.Z.tar.gz`, `agora_windows_amd64_vX.Y.Z.zip`, and
  `SHA256SUMS`, named like the zlcwwhq/mutagen fork's releases. Each archive
  holds only `agora` or `agora.exe`, built by `make release`. Another tag
  starting with `v` fails without publishing.
- Running the workflow by hand builds and packages the same files for
  `dev-<commit>` and keeps them as the run's artifact, without publishing.
- ci can be called by the release workflow and runs on pushed branches, not
  on tags, which the release workflow covers.
- `make verify` exits 0.
