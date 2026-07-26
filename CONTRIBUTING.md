# Contributing to agenton

Thanks for your interest. A quick note on licensing before you send a PR.

## Contributor License grant

agenton is GPL-3.0, but the copyright holder (Niu Du) also ships a proprietary
build on the iOS App Store. That dual arrangement only works while all
copyright is held by one author.

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

Both the Go core (daemon / TUI / web) **and** the iOS app (`ios/`) are open
source in this repo and welcome contributions. The Contributor License grant
above is exactly what lets accepted iOS contributions ship in the official,
signed App Store build.

**Merging a PR is not a release.** Contributions land on `main` after review;
the maintainer cuts versioned releases and publishes the *official* iOS build
from a private signing pipeline, on their own schedule. Anyone can build the app
to the **iOS Simulator** with no Apple account or signing material — see
[`ios/README.md`](ios/README.md).

## Practical notes

- Keep changes focused and include a test where it makes sense.
- Go: `go test -race ./...` must pass (CI runs `go vet` + race tests).
- iOS: `ios/Tools/build-sim.sh` must still compile (CI has no macOS runner, so
  run this locally after Swift changes).
