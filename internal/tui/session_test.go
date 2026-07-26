package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ctrl+t is the one reserved key: it returns to the session list to switch
// sessions, and never reaches the PTY. Everything else is raw passthrough
// (covered by TestEncodeTeaKey).
func TestSessionCtrlTSwitchesSessions(t *testing.T) {
	m := &sessionModel{}
	_, cmd := m.handleKey(tea.KeyMsg{Type: tea.KeyCtrlT})
	if cmd == nil {
		t.Fatal("ctrl+t returned no command")
	}
	if _, ok := cmd().(backToEntryMsg); !ok {
		t.Fatalf("ctrl+t should emit backToEntryMsg, got %T", cmd())
	}
}

// activeMsg(false) parks the view and freezes the current frame; activeMsg(true)
// un-parks. Both cases must re-arm waitForActive so the next ownership change
// is still observed.
func TestSessionActiveMsgParksAndUnparks(t *testing.T) {
	ch := make(chan bool)
	m := &sessionModel{activeCh: ch}

	m2, cmd := m.Update(activeMsg(false))
	if !m2.parked {
		t.Fatal("activeMsg(false) should park the view")
	}
	if cmd == nil {
		t.Fatal("activeMsg(false) should re-arm waitForActive")
	}

	m3, cmd := m2.Update(activeMsg(true))
	if m3.parked {
		t.Fatal("activeMsg(true) should un-park the view")
	}
	if cmd == nil {
		t.Fatal("activeMsg(true) should re-arm waitForActive")
	}
}

// Any key un-parks locally (not just ctrl+t) so the overlay doesn't linger
// for the frame before the daemon confirms Active:true.
func TestSessionAnyKeyUnparks(t *testing.T) {
	m := &sessionModel{parked: true}
	m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if m.parked {
		t.Fatal("keypress while parked should clear parked immediately")
	}
}

// The input writer must deliver every enqueued action exactly once, in order —
// the fix for keystrokes racing when each was forwarded as its own Bubble Tea
// Cmd (goroutine-per-key, no ordering guarantee).
func TestSessionInputWriterPreservesOrder(t *testing.T) {
	m := &sessionModel{}
	m.startInputWriter()
	var got []int
	for i := 0; i < 200; i++ {
		i := i
		m.enqueue(func() { got = append(got, i) })
	}
	done := make(chan struct{})
	m.enqueue(func() { close(done) }) // FIFO: runs after all 200
	<-done
	if len(got) != 200 {
		t.Fatalf("lost input: got %d of 200", len(got))
	}
	for i, v := range got {
		if v != i {
			t.Fatalf("input reordered at index %d: got %d", i, v)
		}
	}
}

// renderParked must keep the frame's line count (so it still fills the
// terminal) and must show the takeover hint text somewhere in the output.
func TestSessionRenderParked(t *testing.T) {
	m := &sessionModel{
		parked: true,
		frozen: "line0\nline1\nline2\nline3\nline4",
		width:  40,
		height: 5,
	}
	out := m.View()
	lines := strings.Split(out, "\n")
	if len(lines) != 5 {
		t.Fatalf("renderParked changed line count: got %d, want 5", len(lines))
	}
	if !strings.Contains(out, "Phone active") {
		t.Fatalf("renderParked output missing takeover hint: %q", out)
	}
}
