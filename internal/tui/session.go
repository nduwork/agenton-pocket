package tui

import (
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"

	"github.com/nduwork/agenton-pocket/internal/client"
)

// The session view is a thin terminal wrapper: it attaches to the daemon PTY
// and passes every keystroke straight through, so it looks and behaves almost
// like running the agent directly. The only reserved key is ctrl+t, which
// returns to the session list to switch sessions (or start a new one). All the
// on-screen buttons / shortcut modes live on the phone + web clients; on a real
// keyboard you just type.
type sessionModel struct {
	client *client.Client
	id     uint32
	name   string

	emu      *vtEmu
	reader   *chanReader
	outCh    <-chan []byte
	activeCh <-chan bool
	closed   bool
	width    int
	height   int
	cols     int
	termRows int
	ready    bool

	// parked is true when another client currently owns the PTY size (e.g. a
	// phone client took over). While parked the live terminal is no longer
	// safe to render (it's sized for someone else), so the view freezes the
	// last good frame and dims it behind a takeover hint.
	parked bool
	frozen string

	// scrollTop is the absolute scrollback offset of the top visible row: 0 is
	// the live bottom, N pages N lines up into history. Only used when the agent
	// hasn't enabled mouse tracking (a shell); claude/codex get the wheel forwarded
	// and scroll their own view instead.
	scrollTop int

	// inputCh serializes all outbound input (keystrokes, take-over claims) so it
	// reaches the daemon PTY in the order typed. Bubble Tea runs every returned
	// Cmd in its own goroutine, so forwarding each keystroke as a separate Cmd
	// let fast input race and arrive reordered/interleaved. A single drain
	// goroutine (started in newSessionModel) preserves order.
	inputCh chan func()

	// cursorOn is the blink phase of the composited cursor: Bubble Tea (v1) owns
	// the real hardware cursor and parks it bottom-left every flush, so it can't
	// mark the agent's caret. renderTerminal draws its own reverse-video block at
	// the emulator's cursor instead, toggled on/off by blinkTickMsg.
	cursorOn bool
}

type renderTickMsg struct{}
type streamEndedMsg struct{}
type activeMsg bool
type blinkTickMsg struct{}

// cursorBlink is the composited cursor's blink half-period.
const cursorBlink = 500 * time.Millisecond

func blinkTick() tea.Cmd {
	return tea.Tick(cursorBlink, func(time.Time) tea.Msg { return blinkTickMsg{} })
}

// waitForActive blocks for the next ownership change on the Attach active
// channel (Task 4) and reports it as an activeMsg. Returns nil (no message)
// once the channel closes.
func waitForActive(ch <-chan bool) tea.Cmd {
	return func() tea.Msg {
		v, ok := <-ch
		if !ok {
			return nil
		}
		return activeMsg(v)
	}
}

// hintHeight reserves the top line for the persistent ctrl+t hint.
const hintHeight = 1

func newSessionModel(c *client.Client, id uint32, name string) *sessionModel {
	m := &sessionModel{client: c, id: id, name: name, cursorOn: true}
	m.startInputWriter()
	out, active, err := c.Attach(id)
	if err == nil {
		m.outCh = out
		m.activeCh = active
		m.reader = newChanReader(out)
		m.emu = newVTEmu(80, 24, m.reader, newForwardingWriter(c, id))
	}
	return m
}

func (m *sessionModel) Init() tea.Cmd {
	cmds := []tea.Cmd{waitForDamage(m.emu)}
	if m.reader != nil {
		cmds = append(cmds, waitForStreamEnd(m.reader.doneCh()))
	}
	if m.activeCh != nil {
		cmds = append(cmds, waitForActive(m.activeCh))
	}
	cmds = append(cmds, blinkTick()) // drive the composited cursor's blink
	// Entering the TUI claims the session: the desk is primary, so any phone/web
	// client attached to it parks into Controller mode. Input no longer
	// auto-claims the size, so claim explicitly on entry (the daemon broadcasts
	// the handover; a WindowSizeMsg resize follows to reflow the PTY to us).
	if m.emu != nil {
		c, id := m.client, m.id
		cmds = append(cmds, func() tea.Msg { _ = c.SetActive(id, true); return nil })
	}
	return tea.Batch(cmds...)
}

// startInputWriter spins up the single goroutine that drains inputCh in order.
// Called once from newSessionModel.
func (m *sessionModel) startInputWriter() {
	m.inputCh = make(chan func(), 1024)
	go func(ch <-chan func()) {
		for fn := range ch {
			fn()
		}
	}(m.inputCh)
}

// enqueue queues an outbound-input action to run in order on the writer
// goroutine. Update is single-threaded in Bubble Tea, so sends here are already
// serialized; the channel just carries that order to the network write. A nil
// channel (unit tests that build a bare sessionModel) drops the action.
func (m *sessionModel) enqueue(fn func()) {
	if m.inputCh == nil {
		return
	}
	m.inputCh <- fn
}

// Close releases the emulator goroutines and the session connection. Idempotent.
func (m *sessionModel) Close() error {
	if m.closed {
		return nil
	}
	m.closed = true
	if m.inputCh != nil {
		close(m.inputCh) // drains remaining queued input, then the goroutine exits
	}
	if m.emu != nil {
		_ = m.emu.Close()
	}
	if m.reader != nil {
		m.reader.stop()
	}
	if m.outCh != nil {
		// Drain until the attach goroutine closes the channel (it exits when
		// the conn below is closed). Without this a blocking send in
		// client.Attach could wedge that goroutine after the emulator stops
		// reading.
		go func(ch <-chan []byte) {
			for range ch {
			}
		}(m.outCh)
	}
	return m.client.Close()
}

// waitForDamage blocks until the emulator signals screen changes (or the
// emulator closes), then returns a renderTickMsg to trigger a re-render.
func waitForDamage(emu *vtEmu) tea.Cmd {
	if emu == nil {
		return nil
	}
	notify := emu.NotifyChanged()
	done := emu.Done()
	return func() tea.Msg {
		// Surface any damage that arrived before we armed (e.g. the initial
		// frame produced by Resize) without blocking.
		if f := emu.GetScreen(); len(f.Damage) > 0 {
			return renderTickMsg{}
		}
		select {
		case <-done:
			return nil
		case <-notify:
			return renderTickMsg{}
		}
	}
}

func waitForStreamEnd(done <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-done
		return streamEndedMsg{}
	}
}

func (m *sessionModel) Update(msg tea.Msg) (*sessionModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.cols = m.width
		m.termRows = m.height - hintHeight // reserve the top line for the hint
		if m.termRows < 3 {
			m.termRows = 3
		}
		m.ready = true
		if m.emu != nil {
			_ = m.emu.Resize(m.cols, m.termRows)
		}
		// Tell the daemon's PTY to match so the agent reflows its TUI to fit.
		_ = m.client.Resize(m.id, m.cols, m.termRows)
		return m, waitForDamage(m.emu)
	case renderTickMsg:
		// Re-arm regardless of parked state; while parked View() ignores live
		// emulator state and shows the frozen frame, so this just keeps the
		// pipe drained for when we un-park.
		return m, waitForDamage(m.emu)
	case blinkTickMsg:
		// Toggle the cursor's blink phase and re-arm. Only the cursor's line
		// changes, so the standard renderer repaints just that row.
		m.cursorOn = !m.cursorOn
		return m, blinkTick()
	case activeMsg:
		if !bool(msg) {
			// Parked: freeze the current frame so live output (now sized for
			// the other client) doesn't garble the view.
			m.parked = true
			m.frozen = m.renderTerminal()
		} else {
			// Became the owner again (e.g. the phone switched to Controller):
			// un-freeze and re-apply our size so the PTY reflows back to this
			// terminal instead of staying at the other device's dimensions.
			m.parked = false
			if m.cols > 0 && m.termRows > 0 {
				_ = m.client.Resize(m.id, m.cols, m.termRows)
			}
		}
		return m, waitForActive(m.activeCh)
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.handleWheel(msg)
	}
	return m, nil
}

func (m *sessionModel) handleKey(k tea.KeyMsg) (*sessionModel, tea.Cmd) {
	// ctrl+t is the one key the wrapper never forwards — it returns to the
	// session list to switch sessions. The session keeps running (detach is
	// implicit on disconnect), so this doubles as detach.
	if k.String() == "ctrl+t" {
		return m, func() tea.Msg { return backToEntryMsg{} }
	}
	m.scrollTop = 0 // any key returns to the live prompt (like tmux copy-mode exit)
	b := encodeTeaKey(k)
	// Any key takes over while parked: input no longer auto-claims the size, so
	// claim it explicitly — the "press any key to take over" contract. Clear
	// parked locally to avoid a one-frame flicker of the stale overlay; the
	// daemon confirms with activeMsg(true) shortly. The claim + keystroke go in
	// the command (network I/O off the update loop).
	if m.parked {
		m.parked = false
		c, id := m.client, m.id
		m.enqueue(func() {
			_ = c.SetActive(id, true)
			if len(b) > 0 {
				_ = c.TextInput(id, string(b))
			}
		})
		return m, nil
	}
	// Everything else goes straight to the PTY.
	if len(b) > 0 {
		c, id := m.client, m.id
		m.enqueue(func() { _ = c.TextInput(id, string(b)) })
	}
	return m, nil
}

// handleWheel forwards a vertical mouse-wheel event to the agent as a mouse
// event, so an agent that enabled mouse reporting (claude/codex) scrolls its
// own conversation instead of the host terminal silently eating the wheel.
//
// Why this exists: the TUI runs in the alternate screen with no mouse mode of
// its own (only WithAltScreen). Without mouse reporting on, the host terminal
// applies alternate-scroll mode and converts trackpad/mouse wheel into Up/Down
// arrow keys — which handleKey then forwards to the agent, where they navigate
// input history ("refill the previous prompt") instead of scrolling. Enabling
// WithMouseCellMotion makes the host report real mouse events; we forward only
// the vertical wheel (wheel-only scope) via the emulator, which encodes it in
// whatever mouse mode the agent itself enabled and drops it if the agent
// hasn't enabled mouse reporting (nothing to scroll).
func (m *sessionModel) handleWheel(msg tea.MouseMsg) (*sessionModel, tea.Cmd) {
	// A parked view is a frozen frame sized for another client — scrolling it
	// has nothing live to move, and we don't want a wheel to silently claim the
	// PTY the way a keystroke does.
	if m.emu == nil || m.parked {
		return m, nil
	}
	button, ok := wheelButton(msg.Button)
	if !ok {
		return m, nil // wheel-only: clicks and motion are left to the host terminal
	}
	// Agent owns the wheel (claude/codex enabled mouse tracking): forward it so
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

// wheelButton maps a Bubble Tea wheel button to the vt mouse-button code the
// emulator's SendMouseWheel expects. Horizontal wheels and non-wheel buttons
// report ok=false (out of scope for the wheel-only fix).
func wheelButton(b tea.MouseButton) (int, bool) {
	switch b {
	case tea.MouseButtonWheelUp:
		return int(vt.MouseWheelUp), true
	case tea.MouseButtonWheelDown:
		return int(vt.MouseWheelDown), true
	}
	return 0, false
}

// wheelCoords maps a Bubble Tea mouse position (0-based, relative to the whole
// program window, which includes the hint bar on top) to 0-based emulator cell
// coordinates: subtract the hint bar and clamp to the terminal's grid so the
// agent gets an in-bounds location. The vt encoder adds 1 for the 1-based wire
// protocol, so 0-based input is correct here.
func wheelCoords(msg tea.MouseMsg, hint, cols, rows int) (x, y int) {
	x = msg.X
	if cols > 0 && x > cols-1 {
		x = cols - 1
	}
	if x < 0 {
		x = 0
	}
	y = msg.Y - hint
	if y < 0 {
		y = 0
	}
	if rows > 0 && y > rows-1 {
		y = rows - 1
	}
	return x, y
}

// encodeTeaKey turns a Bubble Tea key event into the bytes a real terminal
// would send, for raw passthrough. Unknown keys map to nothing.
func encodeTeaKey(k tea.KeyMsg) []byte {
	switch k.Type {
	case tea.KeyRunes:
		return []byte(string(k.Runes))
	case tea.KeySpace:
		return []byte{' '}
	case tea.KeyEnter:
		return []byte{'\r'}
	case tea.KeyEsc:
		return []byte{0x1b}
	case tea.KeyBackspace:
		return []byte{0x7f}
	case tea.KeyTab:
		return []byte{'\t'}
	case tea.KeyShiftTab:
		return []byte{0x1b, '[', 'Z'}
	case tea.KeyUp:
		return []byte{0x1b, '[', 'A'}
	case tea.KeyDown:
		return []byte{0x1b, '[', 'B'}
	case tea.KeyRight:
		return []byte{0x1b, '[', 'C'}
	case tea.KeyLeft:
		return []byte{0x1b, '[', 'D'}
	case tea.KeyDelete:
		return []byte{0x1b, '[', '3', '~'}
	case tea.KeyHome:
		return []byte{0x1b, '[', 'H'}
	case tea.KeyEnd:
		return []byte{0x1b, '[', 'F'}
	}
	// Control characters: Bubble Tea's KeyType for Ctrl+A..Ctrl+Z is the raw
	// control byte (1..26). ctrl+t (20) never reaches here — it's reserved.
	if k.Type >= 1 && k.Type <= 26 {
		return []byte{byte(k.Type)}
	}
	return nil
}

// View renders a thin persistent hint line on top, then the attached PTY below
// it. The hint sits on top so it never fights the agent's own status line
// (claude/codex draw theirs at the bottom). ctrl+t is the reserved key that
// returns to the session list.
func (m *sessionModel) View() string {
	if m.parked {
		return m.renderParked()
	}
	return m.renderTerminal()
}

// renderTerminal renders the hint bar plus the live attached PTY screen. This
// is the same body View() used before parked-mode was added; it must only be
// called when this client owns the PTY size.
func (m *sessionModel) renderTerminal() string {
	if m.emu == nil {
		return "\n  " + errStyle.Render("could not attach a terminal to this session")
	}
	if m.scrollTop > 0 {
		return m.renderScrolled()
	}
	rows := m.emu.GetScreen().Rows
	// Draw the cursor where the agent's caret is. GetScreen bakes no cursor into
	// its rows and Bubble Tea parks the hardware one bottom-left, so we splice a
	// reverse-video block into the grid ourselves, blinking it via cursorOn. Only
	// when we own the live view (never over the frozen, dimmed parked frame).
	if !m.parked && m.cursorOn {
		if pos, visible := m.emu.Cursor(); visible && pos.Y >= 0 && pos.Y < len(rows) {
			rows = append([]string(nil), rows...) // copy: don't mutate the emulator's cache
			rows[pos.Y] = reverseCellAt(rows[pos.Y], pos.X)
		}
	}
	return m.hintBar() + "\n" + strings.Join(rows, "\n")
}

// renderScrolled composes termRows lines from scrollback + the bottom of the
// live screen. The full content is [scrollback(0..sbLen-1), screen(0..h-1)] and
// the live view is its last termRows; scrollTop lifts that window up by N lines.
// No cursor is spliced — the caret is meaningless while paused in history.
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
			y := abs
			rows[r] = styledRow(func(x int) *uv.Cell { return m.emu.ScrollbackCellAt(x, y) }, w)
		default:
			y := abs - sbLen
			rows[r] = styledRow(func(x int) *uv.Cell { return m.emu.CellAt(x, y) }, w)
		}
	}
	return m.hintBar() + "\n" + strings.Join(rows, "\n")
}

// styledRow renders one row of cells to ANSI: SGR transitions between cells,
// grapheme content verbatim, and a trailing reset so style never leaks. Mirrors
// the daemon's replay writeCells. nil/empty cells print as spaces to hold columns.
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
			continue // zero-width shadow of a preceding wide grapheme
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

// reverseCellAt wraps the visible cell at column col of an ANSI-styled row in
// reverse video, so it reads as a terminal cursor. It walks the row counting
// printable columns — skipping CSI/SGR escape sequences so they aren't counted
// or split — and toggles only reverse (\e[7m … \e[27m) on that one cell,
// leaving every other attribute the row set intact. When the cursor sits past
// the row's last printable cell it right-pads with spaces so the block still
// shows.
// ponytail: columns are counted per-rune, so a wide (CJK/emoji) glyph left of
// the cursor shifts the block one cell; widen the counter with runewidth if the
// caret ever lands wrong on wide content.
func reverseCellAt(row string, col int) string {
	var b strings.Builder
	vis := 0
	for i := 0; i < len(row); {
		if row[i] == 0x1b { // pass an escape sequence through untouched
			j := i + 1
			if j < len(row) && row[j] == '[' {
				j++
				for j < len(row) && (row[j] < '@' || row[j] > '~') { // params + intermediates
					j++
				}
				if j < len(row) { // final byte
					j++
				}
			}
			b.WriteString(row[i:j])
			i = j
			continue
		}
		_, size := utf8.DecodeRuneInString(row[i:])
		if vis == col {
			b.WriteString("\x1b[7m")
			b.WriteString(row[i : i+size])
			b.WriteString("\x1b[27m")
		} else {
			b.WriteString(row[i : i+size])
		}
		vis++
		i += size
	}
	if vis <= col { // cursor past the printable content: pad, then a reversed space
		b.WriteString(strings.Repeat(" ", col-vis))
		b.WriteString("\x1b[7m \x1b[27m")
	}
	return b.String()
}

var parkedHintStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	Padding(0, 2)

// renderParked dims the frozen frame and overlays a centered takeover hint.
// lipgloss (v1) has no layer/canvas compositing, so column-level splicing of
// the hint box over live ANSI content would mean hand-rolling ANSI-aware
// cuts; instead this replaces whole rows in the vertical center with the
// (horizontally centered) hint box, leaving the dimmed frame visible above
// and below it.
// ponytail: dimming is a whole-line style wrap, not per-cell — content that
// emits its own SGR resets mid-line can locally undo the faint effect. Fine
// for a cosmetic overlay; revisit with cell-level Faint if it looks off.
func (m *sessionModel) renderParked() string {
	lines := strings.Split(m.frozen, "\n")
	dim := lipgloss.NewStyle().Faint(true)
	for i, l := range lines {
		lines[i] = dim.Render(l)
	}

	box := strings.Split(parkedHintStyle.Render("Phone active — press any key to activate"), "\n")
	top := (len(lines) - len(box)) / 2
	if top < 0 {
		top = 0
	}
	for i, bl := range box {
		row := top + i
		if row >= len(lines) {
			break
		}
		lines[row] = lipgloss.PlaceHorizontal(m.width, lipgloss.Center, bl, lipgloss.WithWhitespaceChars(" "))
	}
	return strings.Join(lines, "\n")
}

var hintBarStyle = lipgloss.NewStyle().Reverse(true)

func (m *sessionModel) hintBar() string {
	// ⌥/⇧-drag is the host terminal's own selection (it bypasses our mouse
	// capture); agenton just names it here since the capture hides that it works.
	bar := " ctrl+t → switch sessions   ·   ⌥/⇧-drag → copy"
	if m.scrollTop > 0 {
		bar = " SCROLLBACK ↑" + strconv.Itoa(m.scrollTop) + "   ·   any key → live   ·   ⌥/⇧-drag → copy"
	}
	if m.width > 0 {
		return hintBarStyle.Width(m.width).MaxWidth(m.width).Render(bar)
	}
	return bar
}
