# Tutorial: agenton on your phone

The web client ships inside the `agenton` binary — nothing to install on the
phone. You expose the built-in web server to your private tailnet and open it
in the phone's browser.

## 1. One-time setup

On the computer that runs your agents:

    ./install.sh                          # or: go build -o agenton ./cmd/agenton

Install the **Tailscale app** on that computer and log in — the Mac becomes a
node on your tailnet. The default `agenton up` serves the phone bridge over the
tailnet through that app; it works on the **free** Tailscale plan with no extra
setup.

On the phone: install the Tailscale app (App Store / Play Store), log in with
the **same account**, and switch the VPN toggle on.

## 2. Start it (every day)

    agenton up -no-tui

With the Tailscale app running, this binds the Mac's tailnet IP and prints a
scannable QR — scan it in the iOS app (⚙︎ → Scan QR) and you're connected. No
login link, no admin-console step. Run `agenton qr` any time to reprint it.

agenton serves **plain HTTP** over the tailnet. That is the design: only devices
on your tailnet can reach the port, and Tailscale already encrypts the wire.
agenton registers no tailnet node of its own and terminates no TLS.

### No app? Use the browser

The same server hosts a **web client** — no iOS app needed. Open the URL agenton
prints (`agenton: web ready (http://…)`) in any browser on a device that's on
your tailnet. For the phone:

    agenton qr --web        # a QR of the http(s):// URL

Scan that one with the **phone's built-in Camera** (not the app) — it's a normal
web URL, so the camera offers to open it directly in the browser. `agenton qr`
(no flag) still prints the `agenton://` deep link for the native app.

**Make it feel like an app:** in Safari/Chrome, Share → **Add to Home
Screen**. You get a bookmark-style full-screen launcher icon. (Full PWA/offline
behavior would need HTTPS, which agenton does not serve — use the iOS app if you
want a real app.)

## Modes

`agenton up` has two modes:

| Command | Transport |
|---|---|
| `agenton up` (default) | the machine's tailnet IP via the system Tailscale app |
| `agenton up --lan` | localhost only, no tailnet publish |

Both serve plain HTTP. agenton embeds no Tailscale node, so there is no login
link, no `TS_AUTHKEY`, and nothing to approve in the admin console — it reads the
tailnet address from the Tailscale app already running on the machine.

## 3. Use it

- **Session list** — tap a session to attach, `✕` to kill. Start a new one
  with the command field (`claude`, `codex`, `ollama run …`) and the 📁
  folder picker for its working directory.
- **Session view** — live terminal, a button pad (Accept / Reject / Mode /
  Stop, arrows, Rewind, two rebindable customs — hold to rebind), and a text
  bar. `Aa` cycles the terminal font size.
- **Term / Controller modes** — one session has one terminal size, so the
  phone and the desk can't both render it correctly. The `Pad`/`Term` button
  in the top bar switches roles:
  - **Terminal**: the phone owns the size; the terminal renders correctly
    for your screen.
  - **Controller**: the desk owns the size; the phone hides the terminal and
    becomes a full-screen button pad + text bar — drive the agent without
    squinting at a desktop-width frame.

  When you start typing at the desk, the phone parks itself into Controller
  automatically; tap `Term` to take the session back (the terminal resizes to
  the phone instantly).

## Stop / troubleshoot

    agenton up -no-tui --lan   # run LAN-only, no tailnet publish

To stop publishing, stop agenton. Only devices logged into *your* tailnet can
reach the URL — nothing is exposed to the internet. State and logs live in
`~/.local/state/agenton/`; `tailnet.json` there is just the published
host/port, rewritten on every start.

Upgrading from 0.7.x or earlier? agenton no longer creates its own tailnet node.
Delete the leftover state dir and remove the stale nodes it registered:

    rm -rf ~/.local/state/agenton/tsnet

then delete any `agenton`, `agenton-1`, `agenton-2`, … machines in the
[Tailscale admin console](https://login.tailscale.com/admin/machines).
