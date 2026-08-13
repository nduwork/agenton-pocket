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

## Dev loop — `./dev.sh`

Rebuilds from source and restarts the pieces. The three steps are
**independent**, and `stop` is never implied — a running daemon and its sessions
survive unless you explicitly ask to kill them.

    ./dev.sh                    stop + build + start (the full loop)
    ./dev.sh build              rebuild ./agenton — nothing restarted
    ./dev.sh stop start         cycle the server without rebuilding

Pick by what you changed:

| Changed | Run |
|---|---|
| `internal/daemon/**`, `internal/web/*.go`, `cmd/agenton/**` | `./dev.sh stop build start` |
| `internal/web/static/**` (embedded — a browser reload serves the old asset) | `./dev.sh stop build start` |

Options: `-y` skips the "these live sessions will be killed" prompt, `--tailnet`
publishes over the tailnet (default is `--lan`, localhost only).

## Verification

Run these before opening a PR:

- `go test -race ./...` — full suite. CI runs the same on Linux (`ci.yml`).
- `go test -tags live -run LiveClaude ./internal/daemon/` — optional real-agent
  attach check (needs a logged-in `claude` CLI; costs tokens).

Restart the daemon after any `internal/daemon/**` change — a long-lived
`agenton daemon` keeps serving old behavior, so phone/web tests against it
silently pass on stale code. `./dev.sh stop build start` does the cycle and
lists the sessions it will kill first.

## Conventions

- Use Conventional Commit messages — release-please builds the changelog and
  version bumps from them. Don't hand-edit `CHANGELOG.md`.
- **PR titles must carry a Conventional Commit type** —
  `type(optional-scope): summary`, where type is one of `feat` `fix` `docs`
  `style` `refactor` `perf` `test` `build` `ci` `chore` `revert` (append `!`
  for a breaking change). PRs are squash-merged, so **the PR title becomes the
  commit on main that release-please versions from** — a title without a type
  ships silently: no changelog entry, no version bump, no release build (PR
  #23 missed its release exactly this way). CI enforces this (`pr-title.yml`);
  pick the type by the dominant change: user-facing behavior → `feat`/`fix`,
  docs-only → `docs`, CI/tooling → `ci`/`chore`.
- Update docs in the same change that alters behavior. If you add/rename/remove
  a CLI command or flag, or change what a mode does, update `README.md` (the
  Quickstart, Modes, and `## Commands` list) and `agenton help`/`up -h` usage
  text to match. A behavior change with stale docs is an incomplete change.
- Contributions are accepted under the CLA in `.github/CONTRIBUTING.md`. The
  daemon + web source is GPL-3.0. The iOS app is closed-source and maintained in
  a separate repo; App Store / signed builds are the copyright holder's separate
  proprietary path, not part of this repo's flow.
