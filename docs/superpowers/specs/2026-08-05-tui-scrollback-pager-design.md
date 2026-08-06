# Desktop TUI scrollback pager — design

**Date:** 2026-08-05
**Status:** approved (Approach B)
**Branch:** `tui-scrollback-pager`

## Problem

The desktop TUI does not feel like a native terminal when scrolling. Today it
enables `tea.WithMouseCellMotion()`, which makes the host terminal report the
wheel as mouse events instead of scrolling its scrollback. The TUI forwards
those events to the session emulator, which **drops them unless the running
agent enabled mouse reporting**. Result:

- **claude/codex** (mouse tracking on) → the agent scrolls its own conversation. Works.
- **shell / any non-mouse program** → the wheel does nothing at all. There is no
  scrollback view, and the native terminal scrollback is suppressed by
  alt-screen + mouse capture.

Goal: the wheel should feel identical to a native terminal — scroll the agent
when it wants mouse events, otherwise scroll back through session history — with
**zero change to the remote (web/iOS) clients**.

## Why remote is unaffected

The desktop TUI (`internal/tui`) and the remote clients (`internal/web/static`,
`ios/`) are independent render + input paths. They share only the daemon, whose
output, replay, and wire protocol this change does not touch. The one piece of
shared logic reused here — DEC private-mode scanning — is lifted into a small
shared internal package without changing the daemon's behavior.

## Approach B: own the emulator wrapper over `charmbracelet/x/vt`

The TUI currently drives `github.com/taigrr/bubbleterm/emulator`, which wraps a
private `*vt.Emulator` and exposes only `GetScreen`/`SendMouse` — not scrollback,
alt-screen, or mode state. `charmbracelet/x/vt.Emulator` exposes all of it
publicly (`Scrollback`, `ScrollbackLen`, `ScrollbackCellAt`, `CellAt`,
`IsAltScreen`, `CursorPosition`, `Render`, `SendMouse`, `Resize`) and implements
`io.Reader`/`io.Writer`. `x/vt` is already a direct dependency of the TUI (used
for mouse-button constants), so we replace the bubbleterm wrapper with a thin
agenton-owned one and drop the bubbleterm dependency from the TUI.

### New file: `internal/tui/vtemu.go` (~120 lines)

A pipe-only emulator wrapper reproducing exactly what the TUI used from
bubbleterm, plus the scrollback/mode accessors:

- `newVTEmu(cols, rows int, r io.Reader, w io.Writer) *vtEmu`
  - `vt.NewEmulator(cols, rows)`, `SetScrollbackSize(10000)` (parity with daemon `emuScrollback`).
  - `readLoop`: `r.Read` → under `mu`: `vt.Write(chunk)`, scan chunk for private
    modes, mark damaged/notify. (Same structure as bubbleterm `ptyReadLoop`.)
  - `responseLoop`: `vt.Read` → `w.Write` — **must** drain vt's synchronous
    response pipe (DA/DSR/XTVERSION) or the read loop blocks. Behavior identical
    to today.
- Ported unchanged from bubbleterm: `splitIntoRows`, `padRow`, the `damaged`
  flag + buffered `notifyC`, `NotifyChanged()`, `Done()`, `Close()` (idempotent,
  closes the stop channel + writer; does **not** call `vt.Close()` — matches the
  bubbleterm note about Read/Write races).
- `GetScreen() frame` → `{Rows []string, Damage []lineDamage}` from `vt.Render()`
  + `splitIntoRows`; returns cached rows with empty damage when unchanged (the
  TUI keys re-render off `len(Damage) > 0`).
- `Cursor() (pos, visible)` → `vt.CursorPosition()` + real `?25` visibility from
  the mode scanner (bubbleterm hard-coded `true`).
- `Resize(cols, rows)` → `vt.Resize` + mark damaged.
- `SendMouseWheel(button, x, y)` → `vt.SendMouse(vt.MouseWheel{...})`.
- New accessors for the pager: `IsAltScreen()`, `ScrollbackLen()`,
  `ScrollbackCellAt(x, y)`, `CellAt(x, y)`, `Width()`, `Height()`,
  `MouseTrackingOn()` (modes 1000/1002/1003 any set).

### Shared mode scanner: `internal/vtmode`

Move `updatePrivateModes` (currently unexported in `internal/daemon/session.go`)
into `internal/vtmode`. The daemon imports it at its one call site with no
behavior change; the TUI's `readLoop` uses it to keep a `map[int]bool` for
`MouseTrackingOn()` and cursor `?25`. `modeRestoreBytes` stays in the daemon
(daemon-only). Pure refactor, covered by existing daemon tests.

## Pager behavior (`internal/tui/session.go`)

State added to `sessionModel`: `scrollTop int` — absolute scrollback index of the
top visible row; `0` means "live" (bottom).

`handleWheel`:
1. `m.parked || m.emu == nil` → ignore (unchanged).
2. `m.emu.MouseTrackingOn()` → forward to the agent via `SendMouseWheel` exactly
   as today (claude/codex scroll their own view). Do **not** engage the pager.
3. Otherwise → adjust `scrollTop` by the wheel ticks, clamped to
   `[0, ScrollbackLen()]`. Wheel up increases it (older), wheel down decreases.

`handleKey`: any key press resets `scrollTop = 0` (snap to live) before its
normal handling — matches tmux copy-mode exit and a native terminal (typing
jumps you to the prompt).

New output while scrolled keeps `scrollTop` anchored to the same absolute
scrollback line (does not yank to the bottom). Because `scrollTop` is an absolute
index and new lines only push into scrollback below the view, the anchored
content stays put; clamp on each render in case scrollback overflows its cap.

`renderTerminal`:
- `scrollTop == 0` → unchanged (bubbleterm-parity live frame + spliced cursor).
- `scrollTop > 0` → compose a `termRows`-tall window: rows pulled from
  `ScrollbackCellAt` for indices `[scrollTop, …)` and, once past the scrollback
  boundary, from `CellAt` (current screen). Render cells → ANSI with a
  `writeCells`-style helper (the daemon's `replay.go` proves this rendering).
  Hide the spliced cursor. Hint bar shows `SCROLLBACK ↑N` so the state is obvious.

## Text selection / copy

Selecting and copying text is **delegated to the host terminal**, not
implemented in agenton. The desktop TUI captures the mouse (`WithMouseCellMotion`)
and renders in the alt-screen, so plain click-drag goes to agenton; but every
mainstream terminal offers a modifier-bypass that does native selection of the
on-screen glyphs regardless: **Option+drag** (iTerm2/Ghostty), **Shift+drag**
(most Linux terminals + macOS Terminal). Because the terminal selects whatever
glyphs are visible, this works for live output *and* for scrollback once the
pager brings it on-screen — so the pager is what makes history copyable. This is
confirmed viable in-repo: URLs are already click-through today, which means the
output is real selectable text and the terminal's modifier-bypass is active.

An app-side copy-mode (drag-select + OSC 52) and a mouse-free passthrough
redesign were both considered and rejected for v1: they re-implement what the
terminal already does. Deliverable here is a short README note, not code.

## Out of scope (v1)

- A full-screen alt-screen app that does **not** enable mouse (e.g. `less`
  without mouse) has no scrollback and gains no scroll. The targets are shell
  scrollback and claude/codex; note it, don't build for it.
- No horizontal scroll, no search, no selection/copy.

## Testing

- `internal/vtmode`: unit tests move with `updatePrivateModes` (existing cases).
- `vtemu.go`: table test feeding known ANSI (text + `?1002h`/`?1002l`/`?25l` +
  scrollback overflow) and asserting `MouseTrackingOn`, `Cursor` visibility,
  `ScrollbackLen`, and rendered rows.
- Pager: unit-test the `scrollTop` clamp/anchor math and the forward-vs-scroll
  branch of `handleWheel` (reuse the existing `wheel_test.go` harness) with a
  fake emulator interface so no real PTY is needed.
- Whole suite: `go test -race ./...`. Manual: run in a branch build, scroll at a
  shell and inside claude, confirm both feel native; confirm web/iOS unchanged.

## Risk

The live-render + damage loop currently works; reproducing it is the main risk.
Mitigation: port bubbleterm's loop structure verbatim (only swapping in the
public `x/vt` accessors), keep the change on this branch, run `go test -race`,
and dogfood before merging to main.
