package tui

import (
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
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

	// Wheel down pages back toward live and floors at 0 (never negative).
	up := m.scrollTop
	m.handleWheel(tea.MouseMsg{X: 1, Y: 1, Button: tea.MouseButtonWheelDown})
	if m.scrollTop != up-3 {
		t.Fatalf("wheel down: scrollTop=%d want %d", m.scrollTop, up-3)
	}
	for i := 0; i < 10; i++ {
		m.handleWheel(tea.MouseMsg{X: 1, Y: 1, Button: tea.MouseButtonWheelDown})
	}
	if m.scrollTop != 0 {
		t.Fatalf("wheel down past live: scrollTop=%d want 0", m.scrollTop)
	}
}

func TestHandleWheelSendsArrowsOnAltScreen(t *testing.T) {
	// Scrollback exists, then the app enters the alt screen without enabling
	// mouse tracking (less/man/vim without mouse). The wheel must not page the
	// main screen's history over the alt screen; it becomes arrow keys instead.
	e := newVTEmu(20, 3, strings.NewReader(strings.Repeat("x\r\n", 10)+"\x1b[?1049h"), io.Discard)
	defer e.Close()
	if !waitUntil(t, e.IsAltScreen) {
		t.Fatal("emulator never entered the alt screen")
	}

	m := &sessionModel{emu: e, cols: 20, termRows: 3}
	m.inputCh = make(chan func(), 8)
	m.handleWheel(tea.MouseMsg{X: 1, Y: 1, Button: tea.MouseButtonWheelUp})
	if m.scrollTop != 0 {
		t.Fatalf("pager engaged on alt screen: scrollTop=%d", m.scrollTop)
	}
	if n := len(m.inputCh); n != 1 {
		t.Fatalf("alt-screen wheel must enqueue arrow keys: enqueued %d, want 1", n)
	}
}

func TestPinScrollAnchorsWhileOutputArrives(t *testing.T) {
	m := &sessionModel{scrollTop: 5, lastSbLen: 10}
	m.pinScroll(14) // 4 new lines entered scrollback while paged
	if m.scrollTop != 9 || m.lastSbLen != 14 {
		t.Fatalf("pinScroll: scrollTop=%d lastSbLen=%d, want 9/14", m.scrollTop, m.lastSbLen)
	}
	// At live (scrollTop 0) growth must not move the view.
	m = &sessionModel{scrollTop: 0, lastSbLen: 10}
	m.pinScroll(20)
	if m.scrollTop != 0 {
		t.Fatalf("pinScroll moved a live view: scrollTop=%d", m.scrollTop)
	}
}

func TestRenderScrolledShowsOldestAtFullScroll(t *testing.T) {
	// 3-row screen, 10 numbered lines => line0..line6 land in scrollback.
	var sb strings.Builder
	for i := 0; i < 10; i++ {
		fmtLine := "line" + string(rune('0'+i)) + "\r\n"
		sb.WriteString(fmtLine)
	}
	e := newVTEmu(20, 3, strings.NewReader(sb.String()), io.Discard)
	defer e.Close()
	if !waitUntil(t, func() bool { return e.ScrollbackLen() >= 7 }) {
		t.Fatalf("scrollback never filled: len=%d", e.ScrollbackLen())
	}

	m := &sessionModel{emu: e, cols: 20, termRows: 3, scrollTop: 1 << 20}
	out := m.renderScrolled()
	lines := strings.Split(out, "\n")
	if len(lines) != 1+m.termRows {
		t.Fatalf("renderScrolled emitted %d lines, want %d (hint + rows)", len(lines), 1+m.termRows)
	}
	if m.scrollTop != e.ScrollbackLen() {
		t.Fatalf("scrollTop not clamped to sbLen: %d vs %d", m.scrollTop, e.ScrollbackLen())
	}
	// Fully scrolled: the window starts at the oldest history line.
	if !strings.Contains(lines[1], "line0") || !strings.Contains(lines[2], "line1") {
		t.Fatalf("full scroll should show oldest lines, got %q / %q", lines[1], lines[2])
	}
}

// Regression: rendering a scrolled view while the read loop is writing must
// not race — cells are copied under the emulator lock, never aliased. The
// pre-fix code handed out *uv.Cell pointers into the live buffer and the race
// detector flagged exactly this interleaving.
func TestRenderScrolledConcurrentWithOutput(t *testing.T) {
	pr, pw := io.Pipe()
	e := newVTEmu(20, 3, pr, io.Discard)
	defer e.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			_, _ = io.WriteString(pw, "styled \x1b[31mred\x1b[m text\r\n")
		}
		pw.Close()
	}()
	m := &sessionModel{emu: e, cols: 20, termRows: 3}
	for i := 0; i < 200; i++ {
		m.scrollTop = 2
		_ = m.renderScrolled()
	}
	<-done
}

func TestStyledRowHoldsColumns(t *testing.T) {
	cell := func(s string) uv.Cell { return uv.Cell{Content: s, Width: 1} }
	row := []uv.Cell{
		cell("a"),
		uv.EmptyCell,                     // blank cell renders as a space
		{Content: "宽", Width: 2},         // wide grapheme...
		{},                               // ...followed by its zero-width shadow (skipped)
		cell("b"),
	}
	if got, want := styledRow(row), "a 宽b"; got != want {
		t.Fatalf("styledRow=%q want %q", got, want)
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
