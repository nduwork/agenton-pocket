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

type curPos struct{ X, Y int }
type frame struct {
	Rows    []string
	Changed bool
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
	e.vt.SetScrollbackSize(vtmode.ScrollbackLines)
	// Track DEC private modes off the vt's own parser rather than re-scanning
	// the byte stream: the parser handles sequences split across read chunks,
	// which a byte scan misses — and a missed ?1002h/l misroutes the wheel.
	// Callbacks fire inside vt.Write, which only runs under mu (readLoop).
	e.vt.SetCallbacks(vt.Callbacks{
		EnableMode: func(m ansi.Mode) {
			if dm, ok := m.(ansi.DECMode); ok {
				e.modes[int(dm)] = true
			}
		},
		DisableMode: func(m ansi.Mode) {
			if dm, ok := m.(ansi.DECMode); ok {
				delete(e.modes, int(dm))
			}
		},
	})
	go e.responseLoop()
	go e.readLoop(r)
	return e
}

// readLoop feeds child output into the emulator. Same structure as
// bubbleterm's ptyReadLoop.
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
// loop under mu and freezes the terminal — which is also why a write error
// must not end the drain: keep reading and drop the bytes instead.
func (e *vtEmu) responseLoop() {
	buf := make([]byte, 4096)
	w := e.writer
	for {
		select {
		case <-e.stopChan:
			return
		default:
		}
		n, err := e.vt.Read(buf)
		if n > 0 && w != nil {
			if _, werr := w.Write(buf[:n]); werr != nil {
				w = nil // connection gone: keep draining so vt.Write never blocks
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
// endpoints (reader.stop / client.Close in sessionModel.Close) ends the read
// loop. responseLoop stays parked in vt.Read — an accepted per-attach leak
// until x/vt grows a race-safe Close — so drop the scrollback here to keep
// what it pins small.
func (e *vtEmu) Close() error {
	e.closeOnce.Do(func() {
		close(e.stopChan)
		e.mu.Lock()
		e.vt.ClearScrollback()
		e.mu.Unlock()
	})
	return nil
}

// GetScreen renders the current screen; Changed reports whether the frame
// differs from the previous call (the TUI keys re-render off it).
func (e *vtEmu) GetScreen() frame {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.damaged {
		return frame{Rows: e.lastRows}
	}
	rendered := e.vt.Render()
	e.damaged = false
	changed := rendered != e.lastRender
	if changed {
		e.lastRender = rendered
	}
	rows := splitIntoRows(rendered, e.height, e.width)
	e.lastRows = rows
	return frame{Rows: rows, Changed: changed}
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

// ScrolledView copies the nRows-row window whose top sits scrollTop lines
// above the live bottom — the full content being [scrollback, screen] — in a
// single lock hold. Cells are copied by value: x/vt's CellAt/ScrollbackCellAt
// return pointers into buffers the read loop keeps mutating, so handing those
// out would race once the lock is released. Rows past either end come back as
// blanks. Returns the rows and scrollTop re-clamped to the current history.
func (e *vtEmu) ScrolledView(scrollTop, nRows int) ([][]uv.Cell, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	sbLen := e.vt.ScrollbackLen()
	if scrollTop > sbLen {
		scrollTop = sbLen
	}
	w, h := e.vt.Width(), e.vt.Height()
	base := sbLen - scrollTop // absolute index of the top visible line
	rows := make([][]uv.Cell, nRows)
	for r := range rows {
		abs := base + r
		row := make([]uv.Cell, w)
		for x := 0; x < w; x++ {
			var c *uv.Cell
			switch {
			case abs >= 0 && abs < sbLen:
				c = e.vt.ScrollbackCellAt(x, abs)
			case abs >= sbLen && abs < sbLen+h:
				c = e.vt.CellAt(x, abs-sbLen)
			}
			if c != nil {
				row[x] = *c
			} else {
				row[x] = uv.EmptyCell
			}
		}
		rows[r] = row
	}
	return rows, scrollTop
}

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
