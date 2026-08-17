package tui

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/sys/unix"
	"golang.org/x/term"

	"github.com/nduwork/agenton-pocket/internal/client"
)

// hintHeight reserves the top line for the persistent ctrl+t hint. The agent's
// grid is sized (cols, rows-1) below it, so the hint never fights the agent's
// own status line (claude/codex draw theirs at the bottom).
const hintHeight = 1

// hintText is the persistent top-line hint.
const hintText = " ctrl+t → switch sessions   ·   Option/Shift+mouse drag to select text"

// sessionView is the raw-mode session view: it attaches to the daemon PTY and
// passes bytes straight through to the host terminal, so the session feels
// exactly like a native terminal — real cursor, real mouse, native scrollback,
// exact colors, zero latency. The only reserved key is ctrl+t, which returns to
// the session list. A shadow emulator (fed the same output) powers the parked
// overlay's frozen frame and triggers hint-bar redraws.
type sessionView struct {
	c   *client.Client
	id  uint32
	emu *vtEmu

	// mu guards the view state and serializes writes to the host terminal.
	// Output passthrough, hint redraws, and the parked overlay all go through
	// it, so a stale chunk can never overwrite a freshly drawn overlay.
	mu       sync.Mutex
	parked   bool     // another client owns the shared PTY size: freeze the view
	cols     int
	rows     int
	termRows int
	frozen   []string // last screen rows, captured when parked

	emuCh    chan []byte // feeds the shadow emulator
	detachCh chan struct{}
	doneCh   chan struct{}
	stopCh   chan struct{}
}

func newSessionView(c *client.Client, id uint32) *sessionView {
	return &sessionView{
		c:        c,
		id:       id,
		emuCh:    make(chan []byte, 64),
		detachCh: make(chan struct{}, 1),
		doneCh:   make(chan struct{}),
		stopCh:   make(chan struct{}),
	}
}

// runSession runs the raw-mode session view against a daemon socket. Returns a
// notice for the entry screen and an error (nil on normal exit).
func runSession(socketPath string, id uint32, name string) (string, error) {
	c, err := dialClient(socketPath)
	if err != nil {
		return "", err
	}
	// Claim the shared PTY size before attaching: the desk is primary, so any
	// phone/web client attached to this session parks into Controller mode. The
	// daemon broadcasts the handover to the other clients; we're not subscribed
	// yet, so we start active by default.
	_ = c.SetActive(id, true)
	out, active, err := c.Attach(id)
	if err != nil {
		c.Close()
		return "", err
	}

	v := newSessionView(c, id)
	v.emu = newVTEmu(80, 24, newChanReader(v.emuCh), io.Discard)
	defer v.emu.Close()

	// Raw mode: ECHO/ICANON/ISIG/ICRNL off so keystrokes pass through unmodified
	// (Enter = \r, ctrl+c = 0x03, no line buffering).
	fd := int(os.Stdin.Fd())
	restore, err := makeRaw(fd)
	if err != nil {
		c.Close()
		return "", err
	}

	// Size the PTY to (cols, rows-1) so the hint bar on row 1 never fights the
	// agent's grid.
	if cols, rows, err := term.GetSize(fd); err == nil {
		v.setSize(cols, rows)
		_ = c.Resize(id, cols, v.termRows)
	}

	v.mu.Lock()
	v.drawHintLocked()
	v.mu.Unlock()

	// The output loop is tracked separately: it drains the stream, bounded by
	// the doneCh timeout below. The rest stop on stopCh / channel close and are
	// waited on so none of them writes to the terminal after restore().
	var wg sync.WaitGroup
	wg.Add(4)
	go func() { defer wg.Done(); v.inputLoop() }()
	go func() { defer wg.Done(); v.activeLoop(active) }()
	go func() { defer wg.Done(); v.hintLoop() }()
	go func() { defer wg.Done(); v.sigwinchLoop() }()
	go v.outputLoop(out)

	// Wait for detach (ctrl+t) or the stream ending (session exited / conn drop).
	var notice string
	select {
	case <-v.detachCh:
		notice = "detached — session keeps running"
	case <-v.doneCh:
		notice = "session ended"
	}

	// Stop the loops and restore the terminal. Closing the conn ends the Attach
	// reader, which closes out/active; the output loop then drains and closes
	// doneCh. The timeout guards a slow terminal blocking the final write.
	close(v.stopCh)
	_ = c.Close()
	select {
	case <-v.doneCh:
	case <-time.After(2 * time.Second):
	}
	wg.Wait()
	_ = restore()
	return notice, nil
}

// setSize records the terminal size and derives the agent's grid height.
func (v *sessionView) setSize(cols, rows int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.cols = cols
	v.rows = rows
	v.termRows = rows - hintHeight
	if v.termRows < 3 {
		v.termRows = 3
	}
}

// outputLoop forwards the daemon's raw PTY bytes to the host terminal while we
// own the size, and feeds the shadow emulator always (so the parked overlay and
// hint redraws stay current). Exits when the output stream ends.
func (v *sessionView) outputLoop(out <-chan []byte) {
	defer close(v.doneCh)
	defer close(v.emuCh)
	for b := range out {
		v.mu.Lock()
		if !v.parked {
			os.Stdout.Write(crlf(b))
		}
		v.mu.Unlock()
		// Feed the shadow emulator non-blocking: it's only for the parked overlay
		// and hint redraws, and dropping keeps a slow emulator from stalling the
		// passthrough.
		select {
		case v.emuCh <- b:
		default:
		}
	}
}

// inputLoop reads raw stdin and forwards it to the daemon, intercepting ctrl+t
// (detach) and translating SGR mouse events for the hint bar's 1-row offset.
// Polls stdin so it can notice stopCh while blocked on input.
func (v *sessionView) inputLoop() {
	buf := make([]byte, 4096)
	var pending []byte
	for {
		fds := []unix.PollFd{{Fd: int32(os.Stdin.Fd()), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, 100)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return
		}
		select {
		case <-v.stopCh:
			return
		default:
		}
		if n > 0 && fds[0].Revents&unix.POLLIN != 0 {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				pending = append(pending, buf[:n]...)
				pending = v.processInput(pending)
			}
			if err != nil {
				return
			}
		}
	}
}

// processInput forwards as much of pending as is complete, returning the
// leftover (an incomplete SGR mouse sequence). ctrl+t is intercepted as the
// detach key; SGR mouse events are translated for the hint bar's 1-row offset.
func (v *sessionView) processInput(pending []byte) []byte {
	for {
		idx := bytes.Index(pending, []byte("\x1b[<"))
		if idx < 0 {
			v.forward(pending)
			return nil
		}
		if idx > 0 {
			v.forward(pending[:idx])
			pending = pending[idx:]
		}
		out, consumed, state := sgrMouseSeq(pending)
		switch state {
		case sgrComplete:
			if out != nil {
				v.forward(out)
			}
			pending = pending[consumed:]
		case sgrMalformed:
			// Not a mouse sequence after all: forward the ESC[< literally.
			v.forward(pending[:3])
			pending = pending[3:]
		case sgrIncomplete:
			return pending
		}
	}
}

// forward sends b to the daemon PTY, intercepting ctrl+t (0x14) as the detach
// key. While parked, any key claims the shared PTY size and resumes the live
// view — the "press any key to activate" contract.
func (v *sessionView) forward(b []byte) {
	if len(b) == 0 {
		return
	}
	v.mu.Lock()
	parked := v.parked
	v.mu.Unlock()
	if parked {
		_ = v.c.SetActive(v.id, true)
		v.mu.Lock()
		v.parked = false
		v.mu.Unlock()
		v.redrawScreen()
	}
	segs, ctrlT := splitInput(b)
	for _, seg := range segs {
		v.send(seg)
	}
	if ctrlT {
		select {
		case v.detachCh <- struct{}{}:
		default:
		}
	}
}

// splitInput splits b into segments at ctrl+t (0x14) boundaries, returning the
// segments (never empty) and whether a ctrl+t was found. The input loop uses it
// to forward everything except the reserved detach key.
func splitInput(b []byte) ([][]byte, bool) {
	var segs [][]byte
	start := 0
	ctrlT := false
	for i := 0; i < len(b); i++ {
		if b[i] == 0x14 {
			ctrlT = true
			if i > start {
				segs = append(segs, b[start:i])
			}
			start = i + 1
		}
	}
	if start < len(b) {
		segs = append(segs, b[start:])
	}
	if len(segs) == 0 {
		segs = [][]byte{{}}
	}
	return segs, ctrlT
}

func (v *sessionView) send(b []byte) {
	if len(b) == 0 {
		return
	}
	if err := v.c.TextInput(v.id, string(b)); err != nil {
		// The conn is gone (daemon restarted, session ended); the output loop
		// notices and exits.
	}
}

// activeLoop parks/unparks the view on ownership changes broadcast by the
// daemon (e.g. the phone taking over the shared PTY size).
func (v *sessionView) activeLoop(active <-chan bool) {
	for a := range active {
		if a {
			v.unpark()
		} else {
			v.park()
		}
	}
}

// park freezes the current frame and draws the dimmed parked overlay.
func (v *sessionView) park() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.parked {
		return
	}
	v.parked = true
	v.frozen = v.emu.GetScreen().Rows
	v.drawParkedLocked()
}

// unpark resumes the live view: re-applies our size so the PTY reflows back to
// this terminal, then redraws the current screen.
func (v *sessionView) unpark() {
	v.mu.Lock()
	defer v.mu.Unlock()
	if !v.parked {
		return
	}
	v.parked = false
	if v.cols > 0 && v.termRows > 0 {
		_ = v.c.Resize(v.id, v.cols, v.termRows)
	}
	v.redrawScreenLocked()
}

// hintLoop redraws the hint bar on emulator damage — full-screen clears
// (CSI 2J) and alt-screen transitions wipe row 1, and the agent's grid can't
// touch it, so damage is the only signal that it needs repainting.
func (v *sessionView) hintLoop() {
	notify := v.emu.NotifyChanged()
	done := v.emu.Done()
	for {
		select {
		case <-v.stopCh:
			return
		case <-done:
			return
		case <-notify:
			v.mu.Lock()
			if !v.parked {
				v.drawHintLocked()
			}
			v.mu.Unlock()
		}
	}
}

// sigwinchLoop re-sizes the PTY and redraws on terminal resize.
func (v *sessionView) sigwinchLoop() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)
	defer signal.Stop(sigCh)
	for {
		select {
		case <-v.stopCh:
			return
		case <-sigCh:
			v.handleResize()
		}
	}
}

func (v *sessionView) handleResize() {
	cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.cols = cols
	v.rows = rows
	v.termRows = rows - hintHeight
	if v.termRows < 3 {
		v.termRows = 3
	}
	_ = v.emu.Resize(cols, v.termRows)
	// Only resize the shared PTY when we own it; the daemon ignores us
	// otherwise (the other client's size stays).
	if !v.parked {
		_ = v.c.Resize(v.id, cols, v.termRows)
	}
	if v.parked {
		v.drawParkedLocked()
	} else {
		v.drawHintLocked()
	}
}

// redrawScreen redraws the current emulator screen (used when unparking so the
// live view returns without waiting for the agent's next redraw).
func (v *sessionView) redrawScreen() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.redrawScreenLocked()
}

// redrawScreenLocked clears the screen and redraws the current emulator screen.
// Caller holds mu.
func (v *sessionView) redrawScreenLocked() {
	rows := v.emu.GetScreen().Rows
	fmt.Fprint(os.Stdout, "\x1b[2J\x1b[H")
	v.drawHintLocked()
	for i, r := range rows {
		fmt.Fprintf(os.Stdout, "\x1b[%d;1H%s", 2+i, r)
	}
}

// drawHintLocked draws the persistent hint bar on host row 1. Caller holds mu.
func (v *sessionView) drawHintLocked() {
	bar := hintBarText(v.cols)
	fmt.Fprintf(os.Stdout, "\x1b7\x1b[1;1H\x1b[7m%s\x1b[0m\x1b8", bar)
}

// drawParkedLocked clears the screen and draws the frozen frame dimmed behind a
// centered takeover hint. Caller holds mu.
func (v *sessionView) drawParkedLocked() {
	fmt.Fprint(os.Stdout, "\x1b[2J\x1b[H")
	v.drawHintLocked()
	for i, r := range v.frozen {
		fmt.Fprintf(os.Stdout, "\x1b[%d;1H\x1b[2m%s\x1b[0m", 2+i, r)
	}
	hint := "Phone active — press any key to activate"
	x := (v.cols - ansi.StringWidth(hint)) / 2
	if x < 0 {
		x = 0
	}
	y := 2 + len(v.frozen)/2
	fmt.Fprintf(os.Stdout, "\x1b[%d;%dH\x1b[7m%s\x1b[0m", y, x+1, hint)
}

// hintBarText returns the hint padded to width so the reverse video covers the
// whole row, truncated when the terminal is narrower than the hint.
func hintBarText(width int) string {
	bar := hintText
	if width > 0 {
		if ansi.StringWidth(bar) > width {
			bar = ansi.Truncate(bar, width, "")
		}
		if pad := width - ansi.StringWidth(bar); pad > 0 {
			bar += strings.Repeat(" ", pad)
		}
	}
	return bar
}

// sgrMouseSeq parses a complete SGR mouse sequence at the start of b (which
// begins with ESC[<). The agent's grid starts at host row 2 (hint bar on row
// 1), so the y coordinate is decremented and events on the hint row are dropped
// (out=nil). Returns the translated bytes, bytes consumed, and a state:
// sgrComplete (out may be nil for a dropped event), sgrIncomplete (need more
// bytes), or sgrMalformed (not a mouse sequence — forward the ESC[< literally).
func sgrMouseSeq(b []byte) (out []byte, n int, state int) {
	if len(b) < 4 || b[0] != 0x1b || b[1] != '[' || b[2] != '<' {
		return nil, 0, sgrMalformed
	}
	i := 3
	nums := make([]int, 0, 3)
	cur := 0
	have := false
	for i < len(b) {
		c := b[i]
		switch {
		case c >= '0' && c <= '9':
			cur = cur*10 + int(c-'0')
			have = true
			i++
		case c == ';' && have:
			nums = append(nums, cur)
			cur, have = 0, false
			i++
		case (c == 'M' || c == 'm') && have:
			nums = append(nums, cur)
			i++
			if len(nums) < 3 {
				return nil, 0, sgrMalformed
			}
			y := nums[2]
			if y <= 1 {
				return nil, i, sgrComplete // on the hint row: drop the event
			}
			y--
			out = fmt.Appendf(nil, "\x1b[<%d;%d;%d%c", nums[0], nums[1], y, c)
			return out, i, sgrComplete
		default:
			return nil, 0, sgrMalformed
		}
	}
	return nil, 0, sgrIncomplete
}

const (
	sgrIncomplete = iota
	sgrComplete
	sgrMalformed
)
