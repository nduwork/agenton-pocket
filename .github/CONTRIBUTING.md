# Contributing to agenton

Thanks for your interest. A quick note on licensing before you send a PR.

## How to propose a change

Changes are proposed by **fork and pull request**. Fork the repo, push your
work to a branch on your fork, and open a PR against `main`. Direct push access
to this repo isn't granted — everyone contributes through their own fork, and
the maintainer reviews and merges.

## Contributor License grant

agenton is GPL-3.0, but the copyright holder (Niu Du) also ships a proprietary
build on the iOS App Store. That dual arrangement only works while all
copyright is held by one author. Bundled dependency licenses (e.g. the
Tailscale client, BSD-3-Clause) are listed in
[THIRD_PARTY_LICENSES.md](../THIRD_PARTY_LICENSES.md).

**By submitting a contribution (a pull request, patch, or any code) you agree
that:**

1. You are the original author of the contribution and have the right to submit
   it.
2. You license your contribution to the project under **GPL-3.0**, and
3. You additionally grant Niu Du a perpetual, worldwide, irrevocable,
   royalty-free right to **relicense your contribution under other terms**,
   including proprietary terms, so it can be included in the paid App Store
   build.

If you cannot agree to point 3, please open an issue to discuss before
contributing.

## What's open, and how releases work

The **Go core in this repo** — daemon / TUI / web — is open source under GPL-3.0
and welcomes contributions. The **iOS app is closed-source and maintained in a
separate repo**; its source is not part of this repo and is not accepted here.
The Contributor License grant above is what lets accepted Go-core contributions
also ship in the official, signed App Store build.

**Merging a PR is not a release.** Contributions land on `main` after review;
the maintainer cuts versioned releases and publishes the *official* iOS build
from a private signing pipeline, on their own schedule.

To try the iOS app on a physical device, request access to the beta test group
by emailing <ndu@nduwork.com>.

## Practical notes

- Commit messages and PR titles follow
  [Conventional Commits](https://www.conventionalcommits.org) (`feat:`, `fix:`,
  `docs:`, …). The release bot ([release-please](https://github.com/googleapis/release-please))
  reads them to compute the next version and update the changelog.
- **Releases are daemon-only, and the commit type is the switch.** A release
  (new version tag + prebuilt binaries) is cut *only* by a daemon behavior
  change: `feat:`, `fix:`, `perf:`, or a breaking `!`. Anything that does not
  change the daemon — docs, README, `install.sh`, CI, tests, or refactors — must
  use a non-releasing type (`docs:`, `chore:`, `ci:`, `test:`, `refactor:`) so
  release-please does not bump the version or rebuild the artifact.
- Keep changes focused and include a test where it makes sense.
- Go: `go test -race ./...` must pass (CI runs `go vet` + race tests). CI skips
  the Go steps for docs-only changes (`docs/`, `assets/`, `*.md`, `LICENSE`) —
  the `test` check still reports green so it never blocks a docs PR.
