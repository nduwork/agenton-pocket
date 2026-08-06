package daemon

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// TestSubscribePrependsModeRestore: the tracked DEC private modes survive any
// history depth and lead the replay so a re-attach restores the agent's
// screen state (bracketed paste, focus reporting, alt screen, ...).
func TestSubscribePrependsModeRestore(t *testing.T) {
	s := NewSession(1, "t", Preset{Command: "claude"})
	s.handleOutput([]byte("\x1b[?2004h\x1b[?1004hscreen frame"))

	_, snap := s.SubscribeWithScrollback(&connWriter{})
	if len(snap) == 0 {
		t.Fatal("empty snapshot")
	}
	lead := snap[0]
	if !bytes.Contains(lead, []byte("\x1b[?1004h")) || !bytes.Contains(lead, []byte("\x1b[?2004h")) {
		t.Fatalf("lead = %q, want focus + bracketed-paste escapes", lead)
	}
	if !bytes.Contains(bytes.Join(snap, nil), []byte("screen frame")) {
		t.Fatalf("snapshot lost screen content: %q", snap)
	}
}

// TestSubscribeNoModesNoLead: a session with no DEC private modes set gets no
// mode-restore lead — the replay starts straight at the content.
func TestSubscribeNoModesNoLead(t *testing.T) {
	s := NewSession(1, "t", Preset{Command: "bash"})
	s.handleOutput([]byte("$ ls\r\n"))
	_, snap := s.SubscribeWithScrollback(&connWriter{})
	if len(snap) == 0 {
		t.Fatal("empty snapshot")
	}
	if bytes.Contains(snap[0], []byte("\x1b[?")) {
		t.Fatalf("unexpected mode lead: %q", snap[0])
	}
	if !bytes.Contains(bytes.Join(snap, nil), []byte("$ ls")) {
		t.Fatalf("snapshot lost content: %q", snap)
	}
}

// TestSubscribeReplaysFullHistory is the point of the emulator-backed replay:
// transcript lines that scrolled off the live screen long ago (and would be
// gone from any raw-chunk ring) come back on attach, each terminated CRLF so
// the client's own scrollback holds them.
func TestSubscribeReplaysFullHistory(t *testing.T) {
	s := NewSession(1, "t", Preset{Command: "claude"})
	for i := 1; i <= 100; i++ { // default emulator is 24 rows; 100 lines overflow it 4x
		s.handleOutput([]byte(fmt.Sprintf("transcript line %d\r\n", i)))
	}
	_, snap := s.SubscribeWithScrollback(&connWriter{})
	all := string(bytes.Join(snap, nil))
	if !strings.Contains(all, "transcript line 1\r\n") {
		t.Fatalf("oldest line missing from replay")
	}
	if !strings.Contains(all, "transcript line 100") {
		t.Fatalf("newest line missing from replay")
	}
}

// TestSubscribeRestoresCursor: the replay parks the cursor where the agent
// left it using bottom-relative moves, so it lands correctly even when the
// client terminal is taller than the session PTY.
func TestSubscribeRestoresCursor(t *testing.T) {
	s := NewSession(1, "t", Preset{Command: "claude"})
	s.handleOutput([]byte("line 1\r\nline 2")) // cursor: row 1, col 6 of a 24-row screen
	_, snap := s.SubscribeWithScrollback(&connWriter{})
	all := string(bytes.Join(snap, nil))
	if !strings.HasSuffix(all, "\r\x1b[22A\x1b[6C") {
		t.Fatalf("replay tail = %q, want CR + 22 up + 6 right", all[max(0, len(all)-30):])
	}
}

// TestSubscribeReplaysStyles: styled cells replay with their SGR attributes
// and each row ends style-clean so colors never bleed across lines.
func TestSubscribeReplaysStyles(t *testing.T) {
	s := NewSession(1, "t", Preset{Command: "claude"})
	s.handleOutput([]byte("\x1b[1;31mred\x1b[m plain\r\n"))
	_, snap := s.SubscribeWithScrollback(&connWriter{})
	all := string(bytes.Join(snap, nil))
	i := strings.Index(all, "red")
	if i < 0 {
		t.Fatalf("styled text missing: %q", all)
	}
	if !strings.Contains(all[:i], "31") {
		t.Fatalf("red foreground not replayed before text: %q", all[:i+3])
	}
}
