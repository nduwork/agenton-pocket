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
