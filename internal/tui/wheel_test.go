package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/vt"
	"github.com/taigrr/bubbleterm/emulator"
)

// wheelButton maps a Bubble Tea wheel button to the vt mouse-button code the
// emulator's SendMouseWheel expects. Non-wheel buttons (clicks, motion) are
// out of scope for the wheel-only fix and report ok=false.
func TestWheelButtonMapping(t *testing.T) {
	cases := []struct {
		b    tea.MouseButton
		ok   bool
		want int
	}{
		{tea.MouseButtonWheelUp, true, int(vt.MouseWheelUp)},
		{tea.MouseButtonWheelDown, true, int(vt.MouseWheelDown)},
		{tea.MouseButtonLeft, false, 0},
		{tea.MouseButtonRight, false, 0},
		{tea.MouseButtonWheelLeft, false, 0}, // horizontal wheels aren't history scroll
	}
	for _, c := range cases {
		got, ok := wheelButton(c.b)
		if ok != c.ok || got != c.want {
			t.Errorf("wheelButton(%v) = (%d, %v), want (%d, %v)", c.b, got, ok, c.want, c.ok)
		}
	}
}

// wheelCoords maps a Bubble Tea mouse position (0-based, relative to the whole
// program window — which includes the 1-line hint bar on top) to 0-based
// emulator cell coordinates, subtracting the hint and clamping to the grid.
func TestWheelCoords(t *testing.T) {
	cases := []struct {
		name     string
		msg      tea.MouseMsg
		hint     int
		cols     int
		rows     int
		wantX    int
		wantY    int
	}{
		{"plain", tea.MouseMsg{X: 5, Y: 3}, 1, 80, 24, 5, 2},
		{"over hint bar clamps to top row", tea.MouseMsg{X: 0, Y: 0}, 1, 80, 24, 0, 0},
		{"past bottom-right clamps", tea.MouseMsg{X: 999, Y: 999}, 1, 80, 24, 79, 23},
		{"unsized terminal still maps", tea.MouseMsg{X: 3, Y: 4}, 1, 0, 0, 3, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotX, gotY := wheelCoords(c.msg, c.hint, c.cols, c.rows)
			if gotX != c.wantX || gotY != c.wantY {
				t.Errorf("wheelCoords(%+v) = (%d, %d), want (%d, %d)", c.name, gotX, gotY, c.wantX, c.wantY)
			}
		})
	}
}

// handleWheel must enqueue exactly one SendMouseWheel for a vertical wheel,
// and enqueue nothing for clicks/motion (wheel-only) or while parked (a frozen
// frame has nothing live to scroll).
func TestHandleWheelEnqueuesOnlyForVerticalWheel(t *testing.T) {
	emu, err := emulator.New(80, 24)
	if err != nil {
		t.Fatalf("emulator.New: %v", err)
	}
	defer emu.Close()

	newModel := func() *sessionModel {
		m := &sessionModel{emu: emu, cols: 80, termRows: 24}
		m.inputCh = make(chan func(), 8)
		return m
	}

	t.Run("wheel up enqueues once", func(t *testing.T) {
		m := newModel()
		m.handleWheel(tea.MouseMsg{X: 10, Y: 5, Button: tea.MouseButtonWheelUp})
		if n := len(m.inputCh); n != 1 {
			t.Fatalf("wheel up: enqueued %d actions, want 1", n)
		}
	})

	t.Run("wheel down enqueues once", func(t *testing.T) {
		m := newModel()
		m.handleWheel(tea.MouseMsg{X: 10, Y: 5, Button: tea.MouseButtonWheelDown})
		if n := len(m.inputCh); n != 1 {
			t.Fatalf("wheel down: enqueued %d actions, want 1", n)
		}
	})

	t.Run("click ignored", func(t *testing.T) {
		m := newModel()
		m.handleWheel(tea.MouseMsg{X: 10, Y: 5, Button: tea.MouseButtonLeft})
		if n := len(m.inputCh); n != 0 {
			t.Fatalf("click: enqueued %d actions, want 0", n)
		}
	})

	t.Run("parked ignored", func(t *testing.T) {
		m := newModel()
		m.parked = true
		m.handleWheel(tea.MouseMsg{X: 10, Y: 5, Button: tea.MouseButtonWheelUp})
		if n := len(m.inputCh); n != 0 {
			t.Fatalf("parked wheel: enqueued %d actions, want 0", n)
		}
	})
}