# agenton — iOS client

A native SwiftUI port of the web client (`internal/web`). Same scope — list
sessions, attach to one, drive it from a button pad + text bar, rebind the two
custom keys — but with iOS-native wins:

- **SwiftTerm** renders the terminal (a `UIScrollView`), so scrollback panning
  and momentum are native instead of an xterm.js viewport in a web view.
- **Button pad** uses SF Symbols, tints the primary keys — Enter (green) /
  Esc (red) — and fires a haptic on every press. Hold a custom key to rebind it.
- Connects with the built-in `URLSessionWebSocketTask` — no third-party deps
  beyond SwiftTerm.

It speaks the exact same wire protocol as the TUI and browser, so it needs no
daemon changes: `[1 type][4 session_id BE][4 len BE][payload]`, control frames
carry the JSON envelope from `internal/protocol`.

## Build

The Xcode project is **generated** from `project.yml` (the source of truth) with
[XcodeGen](https://github.com/yonaskolb/XcodeGen) and is not committed — so the
first step is always to generate it:

```sh
brew install xcodegen
cd ios && xcodegen generate
```

To check that it compiles — no signing, no team, no device (this runs
`xcodegen generate` for you):

```sh
ios/Tools/build-sim.sh        # unsigned iOS Simulator build; args pass to xcodebuild
```

That's the whole iOS check; CI doesn't build the app (no macOS runner), so run
it after any Swift change.

To open it in Xcode (15+, iOS 17 deployment target; last verified with Xcode 26
against SwiftTerm 1.15.0):

```sh
open ios/Agenton.xcodeproj      # after `xcodegen generate`
```

To run on your own iPhone: select the **Agenton** target → Signing &
Capabilities → pick **your** Team, and set your own bundle id. The bundle id in
`project.yml` is a `com.example.*` placeholder; the Simulator ignores it, but a
device build needs a real id under your team. Xcode resolves the SwiftTerm
package on first open.

## Flight test

1. On your Mac (or wherever the agent runs), start the daemon + web bridge:
   ```sh
   agenton up        # daemon + web + TUI
   # or just the bridge:  agenton web
   ```
2. Make it reachable from the phone — a Tailscale hostname is easiest:
   `mac.your-tailnet.ts.net`.
3. In the app, tap ⚙︎ and enter that host + port `9787`. Or tap **Scan QR** and
   scan the block `agenton qr` (or `agenton up`) prints. **Test connection**
   should report `✓ reached agenton`. The bridge is plain `ws://` over the
   tailnet — there is no TLS option, by design.
4. Back out — sessions load. Tap one to attach, or launch a new command.

## Wire self-check

`Tools/selfcheck.swift` asserts the frame codec and envelope JSON match the
daemon byte-for-byte. It's plain Foundation, so it runs without Xcode on a
healthy Swift toolchain:

```sh
swiftc Agenton/Models/Frame.swift Agenton/Models/Protocol.swift \
       Tools/selfcheck.swift -o /tmp/selfcheck && /tmp/selfcheck
```

## Limitations

- Quoted args in the new-session command (whitespace split only, same as web v1.1).
- The daemon bridge is plain `ws://` with no TLS option — the tailnet is the
  security boundary, so App Transport Security is opened for cleartext (arbitrary
  loads + a targeted `ts.net` exception in `project.yml`). This is by design, not
  a prototype shortcut.
