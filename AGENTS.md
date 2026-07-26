# Agenton Pocket — repo guide for agents

Practical guide for working in this repo. See `.github/CONTRIBUTING.md` for the
CLA and contribution policy, and `README.md` for what the project does.

## Layout

- `cmd/agenton/**` — CLI entry points and flags.
- `internal/daemon/**` — the long-lived daemon that owns PTYs and sessions.
- `internal/web/**` — the web bridge; the frontend under `internal/web/static`
  is compiled into the binary via `//go:embed static`.
- `internal/protocol`, `internal/transport` — the wire frame + envelope shared
  by every client.
- `ios/**` — the SwiftUI client, generated from `ios/project.yml` with XcodeGen.

## Dev loop — `./dev.sh`

Rebuilds from source, restarts the pieces, and installs a fresh app on a
simulator. The four steps are **independent**, and `stop` is never implied — a
running daemon and its sessions survive unless you explicitly ask to kill them.

    ./dev.sh                    stop + build + start + ios (the full loop)
    ./dev.sh ios                app only — server left running
    ./dev.sh build              rebuild ./agenton — nothing restarted
    ./dev.sh stop start         cycle the server without rebuilding

Pick by what you changed:

| Changed | Run |
|---|---|
| `internal/daemon/**`, `internal/web/*.go`, `cmd/agenton/**` | `./dev.sh stop build start` |
| `internal/web/static/**` (embedded — a browser reload serves the old asset) | `./dev.sh stop build start` |
| `ios/**` (Swift) | `./dev.sh ios` |
| `ios/project.yml` | `./dev.sh ios --xcodegen` (regenerates the `.xcodeproj` first) |

Options: `-y` skips the "these live sessions will be killed" prompt, `--sim NAME`
targets another simulator (default `iPhone 17`), `--tailnet` publishes over the
tailnet (default is `--lan`, the only deterministic simulator target). The
`.xcodeproj` is generated from `project.yml` and gitignored — `--xcodegen` is
opt-in, never automatic.

## Verification

Run these before opening a PR:

- `go test -race ./...` — full suite. CI runs the same on Linux (`ci.yml`).
- `ios/Tools/build-sim.sh` — unsigned iOS Simulator build (needs Xcode; no
  signing, device, or network). Run after **any** Swift edit — CI has no macOS
  runner, so this is the only iOS compile check. Edited `ios/project.yml`?
  `cd ios && xcodegen generate` first.
- `ios/Tools/selfcheck.swift` — asserts the Swift frame codec + envelope JSON
  match the daemon byte-for-byte; plain Foundation, runs without Xcode.
- `go test -tags live -run LiveClaude ./internal/daemon/` — optional real-agent
  attach check (needs a logged-in `claude` CLI; costs tokens).

Restart the daemon after any `internal/daemon/**` change — a long-lived
`agenton daemon` keeps serving old behavior, so phone/web tests against it
silently pass on stale code. `./dev.sh stop build start` does the cycle and
lists the sessions it will kill first.

## Conventions

- Use Conventional Commit messages — release-please builds the changelog and
  version bumps from them.
- Contributions are accepted under the CLA in `.github/CONTRIBUTING.md`. The
  daemon + web source is GPL-3.0; anyone can build and run the app in the iOS
  Simulator with no Apple account. App Store / signed builds are the copyright
  holder's separate proprietary path, not part of this repo's flow.
