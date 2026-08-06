# Desktop TUI Scrollback Pager Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the desktop TUI's mouse wheel feel native — forward to the agent when it wants mouse events, otherwise scroll back through session history — with zero change to the web/iOS clients.

**Architecture:** Replace the `taigrr/bubbleterm` emulator in `internal/tui` with a thin agenton wrapper over `charmbracelet/x/vt.Emulator` (which exposes scrollback, alt-screen, and cell access). Track DEC private modes off the byte stream (shared with the daemon) to know when the agent owns the mouse. Add a `scrollTop` pager to `sessionModel` that renders a window from the emulator's scrollback when the agent isn't in mouse mode.

**Tech Stack:** Go, `github.com/charmbracelet/x/vt`, `github.com/charmbracelet/ultraviolet` (`uv`), `github.com/charmbracelet/bubbletea` (v1).

## Global Constraints

- Scope is `internal/tui` + one shared helper package `internal/vtmode`. Do **not** change the daemon's output/replay/protocol behavior, `internal/web/**`, or `ios/**`.
- Scrollback cap: `10000` lines (parity with daemon `emuScrollback`).
- Mouse-tracking modes that mean "agent owns the wheel": DEC private `1000`, `1002`, `1003`.
- All access to the shared `*vt.Emulator` and the modes map happens under the emulator's mutex (the read loop writes concurrently).
- Conventional Commit messages. End commit bodies with `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.
- Verify with `go test -race ./...`.

---

### Task 1: Shared DEC private-mode scanner (`internal/vtmode`)

Extract the daemon's `updatePrivateModes` into a shared package so the TUI can reuse it verbatim. Pure refactor — the daemon's behavior is unchanged.

**Files:**
- Create: `internal/vtmode/vtmode.go`
- Create: `internal/vtmode/vtmode_test.go`
- Modify: `internal/daemon/session.go` (remove `updatePrivateModes`, call the shared one at its single call site ~line 264)

**Interfaces:**
- Produces: `func vtmode.UpdatePrivateModes(b []byte, modes map[int]bool)` — scans `b` for `ESC [ ? params h/l`, setting `modes[n]=true` on `h` and `delete(modes, n)` on `l`. Same semantics as today.

- [ ] **Step 1: Write the failing test**

`internal/vtmode/vtmode_test.go`:
```go
package vtmode

import "testing"

func TestUpdatePrivateModes(t *testing.T) {
	modes := map[int]bool{}
	UpdatePrivateModes([]byte("\x1b[?1002h\x1b[?1006h"), modes)
	if !modes[1002] || !modes[1006] {
		t.Fatalf("set: got %v, want 1002+1006 on", modes)
	}
	UpdatePrivateModes([]byte("\x1b[?1002l"), modes)
	if modes[1002] {
		t.Fatalf("reset: 1002 still on: %v", modes)
	}
	// multi-field and non-mode sequences
	modes = map[int]bool{}
	UpdatePrivateModes([]byte("\x1b[?1000;1006h ignore \x1b[31m"), modes)
	if !modes[1000] || !modes[1006] {
		t.Fatalf("multi: got %v", modes)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/vtmode/`
Expected: FAIL — build error, `UpdatePrivateModes` undefined.

- [ ] **Step 3: Create the package**

`internal/vtmode/vtmode.go` — move the body of `updatePrivateModes` verbatim, exported and with a doc comment:
```go
// Package vtmode scans a terminal byte stream for DEC private-mode set/reset
// sequences. Shared by the daemon (to replay modes on attach) and the desktop
// TUI (to know when the agent owns the mouse).
package vtmode

import (
	"strconv"
	"strings"
)

// UpdatePrivateModes scans b for DEC private-mode set/reset sequences
// (ESC [ ? params h/l) and applies them to modes: set on 'h', delete on 'l'. A
// sequence split across chunks won't match; that's rare (escapes are tiny vs.
// chunk size) and self-corrects on the next mode change.
func UpdatePrivateModes(b []byte, modes map[int]bool) {
	for i := 0; i < len(b); i++ {
		if b[i] != 0x1b || i+1 >= len(b) || b[i+1] != '[' {
			continue
		}
		j := i + 2
		if j >= len(b) || b[j] != '?' {
			continue
		}
		j++
		start := j
		for j < len(b) && (b[j] >= '0' && b[j] <= '9' || b[j] == ';') {
			j++
		}
		if j >= len(b) || (b[j] != 'h' && b[j] != 'l') {
			continue
		}
		set := b[j] == 'h'
		for _, field := range strings.Split(string(b[start:j]), ";") {
			if field == "" {
				continue
			}
			if n, err := strconv.Atoi(field); err == nil {
				if set {
					modes[n] = true
				} else {
					delete(modes, n)
				}
			}
		}
		i = j
	}
}
```

- [ ] **Step 4: Update the daemon call site**

In `internal/daemon/session.go`: delete the `updatePrivateModes` function (the block at ~548–585) and change its single caller (~line 264) from `updatePrivateModes(b, s.modes)` to `vtmode.UpdatePrivateModes(b, s.modes)`. Add `"github.com/nduwork/agenton-pocket/internal/vtmode"` to the imports. If `internal/daemon` has a test named for `updatePrivateModes`, point it at `vtmode.UpdatePrivateModes` (or delete it — Task 1's test covers the logic).

- [ ] **Step 5: Run tests**

Run: `go test ./internal/vtmode/ ./internal/daemon/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/vtmode internal/daemon/session.go
git commit -m "refactor: extract UpdatePrivateModes into internal/vtmode

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: `vtEmu` — the x/vt wrapper (`internal/tui/vtemu.go`)

A pipe-only emulator that replaces `bubbleterm/emulator` in the TUI. Reproduces the read/response/damage loops and screen rendering, and exposes scrollback + mouse-mode.

**Files:**
- Create: `internal/tui/vtemu.go`
- Create: `internal/tui/vtemu_test.go`

**Interfaces:**
- Consumes: `vtmode.UpdatePrivateModes` (Task 1).
- Produces:
  - `func newVTEmu(cols, rows int, r io.Reader, w io.Writer) *vtEmu`
  - `type curPos struct{ X, Y int }`
  - `type lineDamage struct{ Row int }`
  - `type frame struct { Rows []string; Damage []lineDamage }`
  - `(*vtEmu) GetScreen() frame`
  - `(*vtEmu) Cursor() (curPos, bool)` — visible is always `true` (parity with old behavior; the pager hides the cursor itself while scrolled)
  - `(*vtEmu) Resize(cols, rows int) error`
  - `(*vtEmu) SendMouseWheel(button, x, y int) error`
  - `(*vtEmu) NotifyChanged() <-chan struct{}`
  - `(*vtEmu) Done() <-chan struct{}`
  - `(*vtEmu) Close() error`
  - `(*vtEmu) MouseTrackingOn() bool`
  - `(*vtEmu) IsAltScreen() bool`
  - `(*vtEmu) ScrollbackLen() int`
  - `(*vtEmu) ScrollbackCellAt(x, y int) *uv.Cell`
  - `(*vtEmu) CellAt(x, y int) *uv.Cell`
  - `(*vtEmu) Width() int`, `(*vtEmu) Height() int`

- [ ] **Step 1: Write the failing test**

`internal/tui/vtemu_test.go`:
```go
package tui

import (
	"io"
	"strings"
	"testing"
	"time"
)

// waitUntil polls f up to 500ms so we don't race the async read loop.
func waitUntil(t *testing.T, f func() bool) bool {
	t.Helper()
	for i := 0; i < 50; i++ {
		if f() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func TestVTEmuRendersTextAndTracksMouseMode(t *testing.T) {
	// Text, then enable mouse tracking (?1002h).
	e := newVTEmu(20, 3, strings.NewReader("hello\x1b[?1002h"), io.Discard)
	defer e.Close()

	if !waitUntil(t, func() bool { return strings.Contains(e.GetScreen().Rows[0], "hello") }) {
		t.Fatalf("screen never showed text: %q", e.GetScreen().Rows)
	}
	if !waitUntil(t, e.MouseTrackingOn) {
		t.Fatal("MouseTrackingOn stayed false after ?1002h")
	}
}

func TestVTEmuMouseModeOffByDefault(t *testing.T) {
	e := newVTEmu(20, 3, strings.NewReader("plain text"), io.Discard)
	defer e.Close()
	waitUntil(t, func() bool { return strings.Contains(e.GetScreen().Rows[0], "plain") })
	if e.MouseTrackingOn() {
		t.Fatal("MouseTrackingOn true with no tracking sequence")
	}
}

func TestVTEmuScrollbackGrows(t *testing.T) {
	// 3-row screen; write 10 newline-terminated lines so ~7 fall into scrollback.
	var sb strings.Builder
	for i := 0; i < 10; i++ {
		sb.WriteString("line\r\n")
	}
	e := newVTEmu(20, 3, strings.NewReader(sb.String()), io.Discard)
	defer e.Close()
	if !waitUntil(t, func() bool { return e.ScrollbackLen() > 0 }) {
		t.Fatalf("scrollback stayed empty, len=%d", e.ScrollbackLen())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestVTEmu`
Expected: FAIL — `newVTEmu` undefined.

- [ ] **Step 3: Implement `vtemu.go`**

```go
package tui

import (
	"io"
	"strings"
	"sync"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"

	"github.com/nduwork/agenton-pocket/internal/vtmode"
)

// emuScrollback matches the daemon's cap so a desktop attach can page back
// through the same amount of history the daemon retains.
const emuScrollback = 10000

type curPos struct{ X, Y int }
type lineDamage struct{ Row int }
type frame struct {
	Rows   []string
	Damage []lineDamage
}

// vtEmu is a pipe-only terminal emulator: it reads a session's PTY output from
// r into a headless x/vt emulator, and drains that emulator's query responses
// (DA/DSR/XTVERSION) back to w — the daemon, i.e. the child's stdin. It replaces
// taigrr/bubbleterm/emulator in the TUI so scrollback, alt-screen, and DEC-mode
// state are reachable for the scrollback pager. All vt access is under mu; the
// read loop writes concurrently with the render/accessor calls.
type vtEmu struct {
	vt     *vt.Emulator
	writer io.Writer

	mu         sync.Mutex
	modes      map[int]bool
	damaged    bool
	lastRows   []string
	lastRender string
	width      int
	height     int

	notifyC   chan struct{}
	stopChan  chan struct{}
	closeOnce sync.Once
}

func newVTEmu(cols, rows int, r io.Reader, w io.Writer) *vtEmu {
	e := &vtEmu{
		vt:       vt.NewEmulator(cols, rows),
		writer:   w,
		modes:    map[int]bool{},
		damaged:  true,
		width:    cols,
		height:   rows,
		notifyC:  make(chan struct{}, 1),
		stopChan: make(chan struct{}),
	}
	e.vt.SetScrollbackSize(emuScrollback)
	go e.responseLoop()
	go e.readLoop(r)
	return e
}

// readLoop feeds child output into the emulator and scans it for DEC private
// modes. Same structure as bubbleterm's ptyReadLoop, plus the mode scan.
func (e *vtEmu) readLoop(r io.Reader) {
	buf := make([]byte, 4096)
	for {
		select {
		case <-e.stopChan:
			return
		default:
		}
		n, err := r.Read(buf)
		if n > 0 {
			e.mu.Lock()
			e.vt.Write(buf[:n])
			vtmode.UpdatePrivateModes(buf[:n], e.modes)
			e.markDamagedLocked()
			e.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

// responseLoop drains the vt emulator's generated responses to the child. The
// vt writes these to a synchronous pipe, so NOT draining them blocks the read
// loop under mu and freezes the terminal — identical to bubbleterm's behavior.
func (e *vtEmu) responseLoop() {
	buf := make([]byte, 4096)
	for {
		select {
		case <-e.stopChan:
			return
		default:
		}
		n, err := e.vt.Read(buf)
		if n > 0 && e.writer != nil {
			if _, werr := e.writer.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (e *vtEmu) markDamagedLocked() {
	e.damaged = true
	select {
	case e.notifyC <- struct{}{}:
	default:
	}
}

func (e *vtEmu) NotifyChanged() <-chan struct{} { return e.notifyC }
func (e *vtEmu) Done() <-chan struct{}          { return e.stopChan }

// Close stops the loops. Like bubbleterm, it does NOT call vt.Close(): the vt
// emulator does not synchronize Close with Read/Write, and closing the pipe
// endpoints (reader.stop / client.Close in sessionModel.Close) ends the I/O.
func (e *vtEmu) Close() error {
	e.closeOnce.Do(func() { close(e.stopChan) })
	return nil
}

// GetScreen renders the current screen; Damage is non-empty when the frame
// changed since the last call (the TUI keys re-render off len(Damage) > 0).
func (e *vtEmu) GetScreen() frame {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.damaged {
		return frame{Rows: e.lastRows}
	}
	rendered := e.vt.Render()
	e.damaged = false
	var damage []lineDamage
	if rendered != e.lastRender {
		damage = make([]lineDamage, e.height)
		for y := 0; y < e.height; y++ {
			damage[y] = lineDamage{Row: y}
		}
		e.lastRender = rendered
	}
	rows := splitIntoRows(rendered, e.height, e.width)
	e.lastRows = rows
	return frame{Rows: rows, Damage: damage}
}

func (e *vtEmu) Cursor() (curPos, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	p := e.vt.CursorPosition()
	return curPos{X: p.X, Y: p.Y}, true
}

func (e *vtEmu) Resize(cols, rows int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.vt.Resize(cols, rows)
	e.width, e.height = cols, rows
	e.markDamagedLocked()
	return nil
}

func (e *vtEmu) SendMouseWheel(button, x, y int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.vt.SendMouse(vt.MouseWheel{Button: vt.MouseButton(button), X: x, Y: y})
	return nil
}

func (e *vtEmu) MouseTrackingOn() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.modes[1000] || e.modes[1002] || e.modes[1003]
}

func (e *vtEmu) IsAltScreen() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.vt.IsAltScreen()
}

func (e *vtEmu) ScrollbackLen() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.vt.ScrollbackLen()
}

func (e *vtEmu) ScrollbackCellAt(x, y int) *uv.Cell {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.vt.ScrollbackCellAt(x, y)
}

func (e *vtEmu) CellAt(x, y int) *uv.Cell {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.vt.CellAt(x, y)
}

func (e *vtEmu) Width() int  { e.mu.Lock(); defer e.mu.Unlock(); return e.vt.Width() }
func (e *vtEmu) Height() int { e.mu.Lock(); defer e.mu.Unlock(); return e.vt.Height() }

// splitIntoRows / padRow port bubbleterm's screen slicing: exactly e.height
// rows, each padded to e.width with an SGR reset before the padding so styles
// don't bleed across the join.
func splitIntoRows(rendered string, height, width int) []string {
	rows := make([]string, height)
	lines := strings.Split(rendered, "\n")
	empty := strings.Repeat(" ", width)
	for i := 0; i < height; i++ {
		if i < len(lines) && lines[i] != "" {
			rows[i] = padRow(lines[i], width)
		} else {
			rows[i] = empty
		}
	}
	return rows
}

func padRow(row string, width int) string {
	const reset = "\033[0m"
	if vis := ansi.StringWidth(row); vis < width {
		return row + reset + strings.Repeat(" ", width-vis)
	}
	return row
}
```

- [ ] **Step 4: Run tests**

Run: `go test -race ./internal/tui/ -run TestVTEmu`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/vtemu.go internal/tui/vtemu_test.go
git commit -m "feat(tui): add vtEmu wrapper over x/vt with scrollback + mode access

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: Swap the TUI from bubbleterm to `vtEmu` (no behavior change)

Point `sessionModel` at `vtEmu`. The live view must look and behave exactly as before; the wheel still forwards unconditionally (the pager arrives in Task 4). Drop the bubbleterm import from the TUI.

**Files:**
- Modify: `internal/tui/session.go` (field type, `newSessionModel` ~95, `waitForDamage` ~175, imports)
- Modify: `internal/tui/wheel_test.go` (construct `vtEmu` instead of `emulator.New`)

**Interfaces:**
- Consumes: `newVTEmu`, `frame`, `curPos` (Task 2).

- [ ] **Step 1: Change the field and constructor**

In `session.go`:
- Field (line 27): `emu *emulator.Emulator` → `emu *vtEmu`.
- `newSessionModel` (line 95): `emu, err := emulator.NewFromPipes(80, 24, m.reader, newForwardingWriter(c, id))` → replace with:
  ```go
  m.emu = newVTEmu(80, 24, m.reader, newForwardingWriter(c, id))
  ```
  Remove the surrounding `if err == nil` (newVTEmu can't fail); keep the `m.outCh/activeCh/reader` assignments above it. The block becomes:
  ```go
  if err == nil {
      m.outCh = out
      m.activeCh = active
      m.reader = newChanReader(out)
      m.emu = newVTEmu(80, 24, m.reader, newForwardingWriter(c, id))
  }
  ```
- `waitForDamage` (line 175): signature `func waitForDamage(emu *emulator.Emulator) tea.Cmd` → `func waitForDamage(emu *vtEmu) tea.Cmd`. Body is unchanged: it calls `emu.NotifyChanged()`, `emu.Done()`, and `emu.GetScreen()` whose `.Damage` is now `[]lineDamage` — `len(f.Damage) > 0` still compiles.
- Remove the import `"github.com/taigrr/bubbleterm/emulator"` (line 11). Keep `"github.com/charmbracelet/x/vt"`.

- [ ] **Step 2: Update the wheel test harness**

In `wheel_test.go`, replace the bubbleterm construction. Change imports: drop `"github.com/taigrr/bubbleterm/emulator"`, add `"io"` and `"strings"`. In `TestHandleWheelEnqueuesOnlyForVerticalWheel`, replace:
```go
	emu, err := emulator.New(80, 24)
	if err != nil {
		t.Fatalf("emulator.New: %v", err)
	}
	defer emu.Close()
```
with:
```go
	emu := newVTEmu(80, 24, strings.NewReader(""), io.Discard)
	defer emu.Close()
```
Leave the rest of that test as-is (mouse tracking is off, so with Task 3's unconditional forward it still enqueues once for wheel up/down — Task 4 revisits it).

- [ ] **Step 3: Build and run the full suite**

Run: `go build ./... && go test -race ./internal/tui/`
Expected: PASS. If any other file in `internal/tui` referenced the `emulator` package (grep first: `grep -rn taigrr/bubbleterm internal/tui`), update it the same way.

- [ ] **Step 4: Confirm bubbleterm is gone from the TUI, tidy modules**

Run: `grep -rn "taigrr/bubbleterm" internal/tui` (expect no output), then `go mod tidy`.
Expected: `go.mod` drops `taigrr/bubbleterm` if nothing else imports it (the daemon may still — check `grep -rn taigrr/bubbleterm internal/`); either way `go build ./...` stays green.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/session.go internal/tui/wheel_test.go go.mod go.sum
git commit -m "refactor(tui): drive sessions with vtEmu instead of bubbleterm

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: Scrollback pager

Add `scrollTop` state and the forward-vs-scroll wheel logic, snap-to-live on keypress, and windowed rendering from scrollback with a hint marker.

**Files:**
- Modify: `internal/tui/session.go` (`sessionModel` struct, `handleWheel`, `handleKey`, `renderTerminal`, `hintBar`; add `renderScrolled`, `styledRow`, `clampScrollTop`)
- Modify: `internal/tui/wheel_test.go` (forward-vs-scroll assertions)
- Create: `internal/tui/pager_test.go`

**Interfaces:**
- Consumes: `vtEmu.MouseTrackingOn`, `ScrollbackLen`, `ScrollbackCellAt`, `CellAt`, `Width`, `Height` (Task 2).
- Produces: `func clampScrollTop(cur, delta, max int) int`.

- [ ] **Step 1: Write failing tests for the scroll math + branch**

`internal/tui/pager_test.go`:
```go
package tui

import (
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestClampScrollTop(t *testing.T) {
	cases := []struct{ cur, delta, max, want int }{
		{0, 3, 100, 3},     // scroll up into history
		{0, -3, 100, 0},    // can't go below live
		{98, 5, 100, 100},  // clamp at oldest
		{100, -1, 100, 99}, // scroll back down
	}
	for _, c := range cases {
		if got := clampScrollTop(c.cur, c.delta, c.max); got != c.want {
			t.Errorf("clampScrollTop(%d,%d,%d)=%d want %d", c.cur, c.delta, c.max, got, c.want)
		}
	}
}

func TestHandleWheelScrollsWhenAgentHasNoMouse(t *testing.T) {
	// 3-row screen with 10 lines => scrollback exists, mouse tracking off.
	e := newVTEmu(20, 3, strings.NewReader(strings.Repeat("x\r\n", 10)), io.Discard)
	defer e.Close()
	waitUntil(t, func() bool { return e.ScrollbackLen() > 0 })

	m := &sessionModel{emu: e, cols: 20, termRows: 3}
	m.inputCh = make(chan func(), 8)

	m.handleWheel(tea.MouseMsg{X: 1, Y: 1, Button: tea.MouseButtonWheelUp})
	if m.scrollTop == 0 {
		t.Fatal("wheel up did not scroll into scrollback")
	}
	if n := len(m.inputCh); n != 0 {
		t.Fatalf("scroll must not forward to agent: enqueued %d", n)
	}
}

func TestHandleWheelForwardsWhenAgentHasMouse(t *testing.T) {
	e := newVTEmu(20, 3, strings.NewReader("\x1b[?1002h"), io.Discard)
	defer e.Close()
	waitUntil(t, e.MouseTrackingOn)

	m := &sessionModel{emu: e, cols: 20, termRows: 3}
	m.inputCh = make(chan func(), 8)

	m.handleWheel(tea.MouseMsg{X: 1, Y: 1, Button: tea.MouseButtonWheelUp})
	if m.scrollTop != 0 {
		t.Fatal("must not engage pager when agent owns the mouse")
	}
	if n := len(m.inputCh); n != 1 {
		t.Fatalf("must forward to agent: enqueued %d, want 1", n)
	}
}

func TestKeyPressSnapsToLive(t *testing.T) {
	e := newVTEmu(20, 3, strings.NewReader(strings.Repeat("x\r\n", 10)), io.Discard)
	defer e.Close()
	waitUntil(t, func() bool { return e.ScrollbackLen() > 0 })

	m := &sessionModel{emu: e, cols: 20, termRows: 3, scrollTop: 5}
	m.inputCh = make(chan func(), 8)
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if m.scrollTop != 0 {
		t.Fatalf("keypress must snap to live, scrollTop=%d", m.scrollTop)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tui/ -run 'TestClampScrollTop|TestHandleWheel|TestKeyPressSnaps'`
Expected: FAIL — `clampScrollTop` undefined; `scrollTop` field missing.

- [ ] **Step 3: Add state, math, and the wheel branch**

In `session.go`:
- Add to `sessionModel` (near `parked`): `scrollTop int // absolute scrollback offset of the top visible row; 0 = live`.
- Add the pure helper:
```go
// clampScrollTop applies a wheel delta (positive = scroll up into history) to
// the current offset, bounded to [0, max]. 0 is the live bottom; max is the
// oldest scrollback line.
func clampScrollTop(cur, delta, max int) int {
	n := cur + delta
	if n < 0 {
		n = 0
	}
	if n > max {
		n = max
	}
	return n
}
```
- Rewrite `handleWheel` (keep the parked/nil guard and `wheelButton`/`wheelCoords`):
```go
func (m *sessionModel) handleWheel(msg tea.MouseMsg) (*sessionModel, tea.Cmd) {
	if m.emu == nil || m.parked {
		return m, nil
	}
	button, ok := wheelButton(msg.Button)
	if !ok {
		return m, nil // wheel-only: clicks/motion are out of scope
	}
	// Agent owns the wheel (claude/codex enabled mouse tracking): forward it, so
	// the agent scrolls its own conversation — exactly like a raw terminal.
	if m.emu.MouseTrackingOn() {
		x, y := wheelCoords(msg, hintHeight, m.cols, m.termRows)
		emu := m.emu
		m.enqueue(func() { _ = emu.SendMouseWheel(button, x, y) })
		return m, nil
	}
	// Otherwise page our own scrollback (shell, or any non-mouse program).
	delta := 3 // rows per wheel notch
	if button == int(vt.MouseWheelDown) {
		delta = -delta
	}
	m.scrollTop = clampScrollTop(m.scrollTop, delta, m.emu.ScrollbackLen())
	return m, func() tea.Msg { return renderTickMsg{} }
}
```
  (`renderTickMsg` already exists as the re-render trigger — confirm with `grep -n renderTickMsg internal/tui/*.go`; it's returned by `waitForDamage`.)
- In `handleKey`, snap to live before the existing logic. Right after the `ctrl+t` early-return, add:
```go
	m.scrollTop = 0 // any key returns to the live prompt (tmux copy-mode exit)
```

- [ ] **Step 4: Add windowed rendering**

In `session.go`, branch `renderTerminal` and add the helpers:
```go
// renderTerminal shows the live screen at scrollTop==0, otherwise a windowed
// view into scrollback.
func (m *sessionModel) renderTerminal() string {
	if m.emu == nil {
		return "\n  " + errStyle.Render("could not attach a terminal to this session")
	}
	if m.scrollTop > 0 {
		return m.renderScrolled()
	}
	rows := m.emu.GetScreen().Rows
	if !m.parked && m.cursorOn {
		if pos, visible := m.emu.Cursor(); visible && pos.Y >= 0 && pos.Y < len(rows) {
			rows = append([]string(nil), rows...)
			rows[pos.Y] = reverseCellAt(rows[pos.Y], pos.X)
		}
	}
	return m.hintBar() + "\n" + strings.Join(rows, "\n")
}

// renderScrolled composes termRows lines from scrollback + the bottom of the
// live screen. Content is [scrollback(0..sbLen-1), screen(0..h-1)]; the live
// view is the last termRows of that. scrollTop lifts the window up by N lines.
func (m *sessionModel) renderScrolled() string {
	sbLen := m.emu.ScrollbackLen()
	w, h := m.emu.Width(), m.emu.Height()
	if m.scrollTop > sbLen {
		m.scrollTop = sbLen
	}
	base := sbLen - m.scrollTop // absolute index of the top visible line
	rows := make([]string, m.termRows)
	for r := 0; r < m.termRows; r++ {
		abs := base + r
		switch {
		case abs < 0 || abs >= sbLen+h:
			rows[r] = strings.Repeat(" ", w)
		case abs < sbLen:
			rows[r] = styledRow(func(x int) *uv.Cell { return m.emu.ScrollbackCellAt(x, abs) }, w)
		default:
			y := abs - sbLen
			rows[r] = styledRow(func(x int) *uv.Cell { return m.emu.CellAt(x, y) }, w)
		}
	}
	return m.hintBar() + "\n" + strings.Join(rows, "\n")
}

// styledRow renders one row of cells to ANSI: SGR transitions between cells,
// grapheme content verbatim, a trailing reset so style never leaks. Mirrors the
// daemon's replay writeCells. Empty cells print as spaces to hold columns.
func styledRow(cell func(x int) *uv.Cell, w int) string {
	var b strings.Builder
	cur := uv.Style{}
	styled := false
	for x := 0; x < w; x++ {
		c := cell(x)
		if c == nil {
			b.WriteByte(' ')
			continue
		}
		if c.Width == 0 && c.Content == "" {
			continue
		}
		if !c.Style.Equal(&cur) {
			b.WriteString(c.Style.Diff(&cur))
			cur = c.Style
			styled = !cur.IsZero()
		}
		if c.Content == "" {
			b.WriteByte(' ')
		} else {
			b.WriteString(c.Content)
		}
	}
	if styled {
		b.WriteString("\x1b[m")
	}
	return b.String()
}
```
Add `uv "github.com/charmbracelet/ultraviolet"` to the imports.

- [ ] **Step 5: Show the scroll state in the hint bar**

Change `hintBar` so the marker appears while scrolled:
```go
func (m *sessionModel) hintBar() string {
	bar := " ctrl+t → switch sessions"
	if m.scrollTop > 0 {
		bar += "   ·   SCROLLBACK ↑" + strconv.Itoa(m.scrollTop) + " (press any key for live)"
	}
	if m.width > 0 {
		return hintBarStyle.Width(m.width).MaxWidth(m.width).Render(bar)
	}
	return bar
}
```
Add `"strconv"` to the imports.

- [ ] **Step 6: Update the old wheel test for the new branch**

In `wheel_test.go`, `TestHandleWheelEnqueuesOnlyForVerticalWheel` now uses an emulator with **mouse tracking on** so wheel-up/down still forward (its assertions expect one enqueue). Change the emulator construction in that test to:
```go
	emu := newVTEmu(80, 24, strings.NewReader("\x1b[?1002h"), io.Discard)
	defer emu.Close()
	waitUntil(t, emu.MouseTrackingOn)
```
The "click ignored" and "parked ignored" subtests are unchanged and still valid.

- [ ] **Step 7: Run the tui suite**

Run: `go test -race ./internal/tui/`
Expected: PASS.

- [ ] **Step 8: Full build + suite**

Run: `go build ./... && go test -race ./...`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/session.go internal/tui/wheel_test.go internal/tui/pager_test.go
git commit -m "feat(tui): scrollback pager — wheel scrolls history when agent has no mouse

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Manual verification (before merge)

Build and dogfood on the branch:
```bash
go build -o /tmp/agenton ./cmd/agenton
/tmp/agenton stop && /tmp/agenton --lan
```
- At a **shell** prompt in a session: wheel up pages back through output, `SCROLLBACK ↑N` shows, any key returns to live. ✓
- Inside **claude/codex**: wheel scrolls the agent's own conversation (no `SCROLLBACK` marker). ✓
- Resize the terminal while scrolled and while live — no corruption. ✓
- Open the **web** client and **iOS** app against the same session: scroll behaves exactly as before (untouched). ✓

### Task 5: Document scroll + text selection (README)

**Files:**
- Modify: `README.md` (the "Using the TUI" section)

- [ ] **Step 1: Add a "Scrolling & copying" note**

Under "Using the TUI", add:
```markdown
**Scrolling & copying.** Mouse-wheel up/down pages through session history — at
a shell you scroll agenton's scrollback (a `SCROLLBACK ↑N` marker shows in the
top bar; press any key to jump back to live), and inside claude/codex the wheel
scrolls their own view. To **select and copy** text, hold your terminal's
selection modifier and drag — **Option+drag** in iTerm2/Ghostty, **Shift+drag**
in most Linux terminals and macOS Terminal. Scroll the history into view first,
then modifier-drag to copy from it.
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: document TUI scrollback + modifier-drag text selection

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

## Notes

`AGENTS.md` optionally gains a line that the TUI now owns its emulator
(`internal/tui/vtemu.go`); the code comments there are the primary doc.
