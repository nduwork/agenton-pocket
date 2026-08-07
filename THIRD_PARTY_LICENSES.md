# Third-Party Licenses

agenton (the daemon + web binary) is GPL-3.0. It statically links the Go
modules below. Their licenses are reproduced at the linked sources. Notably, the
Tailscale client (`tailscale.com`) is BSD-3-Clause and GPL-compatible; agenton
uses only its local-API client, not the embedded `tsnet` node.

Regenerate with: `go-licenses csv ./cmd/agenton`.

The iOS app (`ios/`) additionally links one Swift package, listed under
[iOS / Swift dependencies](#ios--swift-dependencies) below.

| Module | License | Source |
|---|---|---|
| `filippo.io/edwards25519` | BSD-3-Clause | https://github.com/FiloSottile/edwards25519/blob/v1.2.0/LICENSE |
| `github.com/aymanbagabas/go-osc52/v2` | MIT | https://github.com/aymanbagabas/go-osc52/blob/v2.0.1/LICENSE |
| `github.com/BurntSushi/toml` | MIT | https://github.com/BurntSushi/toml/blob/v1.6.0/COPYING |
| `github.com/charmbracelet/bubbletea` | MIT | https://github.com/charmbracelet/bubbletea/blob/v1.3.10/LICENSE |
| `github.com/charmbracelet/colorprofile` | MIT | https://github.com/charmbracelet/colorprofile/blob/v0.4.3/LICENSE |
| `github.com/charmbracelet/lipgloss` | MIT | https://github.com/charmbracelet/lipgloss/blob/v1.1.0/LICENSE |
| `github.com/charmbracelet/ultraviolet` | MIT | https://github.com/charmbracelet/ultraviolet/blob/2399af76d5b1/LICENSE |
| `github.com/charmbracelet/x/ansi` | MIT | https://github.com/charmbracelet/x/blob/ansi/v0.11.7/ansi/LICENSE |
| `github.com/charmbracelet/x/cellbuf` | MIT | https://github.com/charmbracelet/x/blob/cellbuf/v0.0.15/cellbuf/LICENSE |
| `github.com/charmbracelet/x/exp/ordered` | MIT | https://github.com/charmbracelet/x/blob/exp/ordered/v0.1.0/exp/ordered/LICENSE |
| `github.com/charmbracelet/x/term` | MIT | https://github.com/charmbracelet/x/blob/term/v0.2.2/term/LICENSE |
| `github.com/charmbracelet/x/termios` | MIT | https://github.com/charmbracelet/x/blob/termios/v0.1.1/termios/LICENSE |
| `github.com/charmbracelet/x/vt` | MIT | https://github.com/charmbracelet/x/blob/b57e5e6d29bb/vt/LICENSE |
| `github.com/charmbracelet/x/windows` | MIT | https://github.com/charmbracelet/x/blob/windows/v0.2.2/windows/LICENSE |
| `github.com/clipperhouse/displaywidth` | MIT | https://github.com/clipperhouse/displaywidth/blob/v0.11.0/LICENSE |
| `github.com/clipperhouse/uax29/v2/graphemes` | MIT | https://github.com/clipperhouse/uax29/blob/v2.7.0/LICENSE |
| `github.com/coder/websocket` | ISC | https://github.com/coder/websocket/blob/v1.8.15/LICENSE.txt |
| `github.com/creack/pty` | MIT | https://github.com/creack/pty/blob/v1.1.24/LICENSE |
| `github.com/fxamacker/cbor/v2` | MIT | https://github.com/fxamacker/cbor/blob/v2.9.0/LICENSE |
| `github.com/go-json-experiment/json` | BSD-3-Clause | https://github.com/go-json-experiment/json/blob/d219187c3433/LICENSE |
| `github.com/google/uuid` | BSD-3-Clause | https://github.com/google/uuid/blob/v1.6.0/LICENSE |
| `github.com/hdevalence/ed25519consensus` | BSD-3-Clause | https://github.com/hdevalence/ed25519consensus/blob/v0.2.0/LICENSE |
| `github.com/lucasb-eyer/go-colorful` | MIT | https://github.com/lucasb-eyer/go-colorful/blob/v1.4.0/LICENSE |
| `github.com/mattn/go-isatty` | MIT | https://github.com/mattn/go-isatty/blob/v0.0.20/LICENSE |
| `github.com/mattn/go-runewidth` | MIT | https://github.com/mattn/go-runewidth/blob/v0.0.24/LICENSE |
| `github.com/mdp/qrterminal/v3` | MIT | https://github.com/mdp/qrterminal/blob/v3.2.1/LICENSE |
| `github.com/mitchellh/go-ps` | MIT | https://github.com/mitchellh/go-ps/blob/v1.0.0/LICENSE.md |
| `github.com/muesli/ansi` | MIT | https://github.com/muesli/ansi/blob/276c6243b2f6/LICENSE |
| `github.com/muesli/cancelreader` | MIT | https://github.com/muesli/cancelreader/blob/v0.2.2/LICENSE |
| `github.com/muesli/termenv` | MIT | https://github.com/muesli/termenv/blob/v0.16.0/LICENSE |
| `github.com/nduwork/agenton-pocket` | GPL-3.0 | https://github.com/nduwork/agenton-pocket/blob/HEAD/LICENSE |
| `github.com/rivo/uniseg` | MIT | https://github.com/rivo/uniseg/blob/v0.4.7/LICENSE.txt |
| `github.com/taigrr/bubbleterm/emulator` | 0BSD | https://github.com/taigrr/bubbleterm/blob/v0.3.3/LICENSE |
| `github.com/x448/float16` | MIT | https://github.com/x448/float16/blob/v0.8.4/LICENSE |
| `github.com/xo/terminfo` | MIT | https://github.com/xo/terminfo/blob/abceb7e1c41e/LICENSE |
| `go4.org/mem` | Apache-2.0 | https://github.com/go4org/mem/blob/ae6ca9944745/LICENSE |
| `go4.org/netipx` | BSD-3-Clause | https://github.com/go4org/netipx/blob/fdeea329fbba/LICENSE |
| `golang.org/x/crypto` | BSD-3-Clause | https://cs.opensource.google/go/x/crypto/+/v0.52.0:LICENSE |
| `golang.org/x/exp` | BSD-3-Clause | https://cs.opensource.google/go/x/exp/+/b7579e27:LICENSE |
| `golang.org/x/net` | BSD-3-Clause | https://cs.opensource.google/go/x/net/+/v0.55.0:LICENSE |
| `golang.org/x/sync/errgroup` | BSD-3-Clause | https://cs.opensource.google/go/x/sync/+/v0.22.0:LICENSE |
| `golang.org/x/sys` | BSD-3-Clause | https://cs.opensource.google/go/x/sys/+/v0.47.0:LICENSE |
| `golang.org/x/term` | BSD-3-Clause | https://cs.opensource.google/go/x/term/+/v0.45.0:LICENSE |
| `golang.org/x/text` | BSD-3-Clause | https://cs.opensource.google/go/x/text/+/v0.40.0:LICENSE |
| `rsc.io/qr` | BSD-3-Clause | https://github.com/rsc/qr/blob/v0.2.0/LICENSE |
| `tailscale.com` | BSD-3-Clause | https://github.com/tailscale/tailscale/blob/v1.100.0/LICENSE |

## Vendored web assets

The browser client embeds vendored copies of the xterm.js project (MIT). The
jsDelivr-minified files omit the upstream copyright header, so the full
license is reproduced at `internal/web/static/vendor/LICENSE` and ships
inside the binary via `go:embed`.

| Package | License | Source |
|---|---|---|
| `@xterm/xterm@5.5.0` (xterm.js, xterm.css) | MIT | https://github.com/xtermjs/xterm.js/blob/master/LICENSE |
| `@xterm/addon-fit@0.10.0` (addon-fit.js) | MIT | https://github.com/xtermjs/xterm.js/blob/master/LICENSE |

## iOS / Swift dependencies

The SwiftUI app links one third-party Swift package (resolved by SwiftPM):

| Package | License | Source |
|---|---|---|
| `SwiftTerm` | MIT | https://github.com/migueldeicaza/SwiftTerm/blob/main/LICENSE |
