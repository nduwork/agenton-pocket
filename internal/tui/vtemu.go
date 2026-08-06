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

// splitIntoRows / padRow port bubbleterm's screen slicing: exactly height rows,
// each padded to width with an SGR reset before the padding so styles don't
// bleed across the join.
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
