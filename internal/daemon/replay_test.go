package daemon

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"
)

// TestReplayRoundTrip: feeding the serialized replay into a fresh terminal of
// the same size must reproduce the original screen and cursor exactly — the
// replay is only correct if a re-attaching client sees the frame the agent
// actually left behind, including in-place repaints (cursor-up + rewrite, the
// way claude animates its live region).
func TestReplayRoundTrip(t *testing.T) {
	s := NewSession(1, "t", Preset{Command: "claude"})
	s.mu.Lock()
	s.emu.Resize(40, 10)
	s.mu.Unlock()
	// Transcript that scrolls, styles, then an in-place repaint of a 2-row
	// live region (spinner frame -> replaced content), cursor parked mid-row.
	for i := 1; i <= 15; i++ {
		s.handleOutput([]byte(fmt.Sprintf("\x1b[32mline %d\x1b[m tail\r\n", i)))
	}
	s.handleOutput([]byte("spinner...\r\nstatus bar"))
	s.handleOutput([]byte("\x1b[1A\r\x1b[Kdone!     \r\n\x1b[Kready> "))

	_, snap := s.SubscribeWithScrollback(&connWriter{})
	replayed := vt.NewEmulator(40, 10)
	_, _ = replayed.Write(bytes.Join(snap, nil))

	want, got := s.emu.String(), replayed.String()
	if strings.TrimRight(want, "\n ") != strings.TrimRight(got, "\n ") {
		t.Fatalf("screen mismatch\n--- original ---\n%s\n--- replayed ---\n%s", want, got)
	}
	if s.emu.CursorPosition() != replayed.CursorPosition() {
		t.Fatalf("cursor mismatch: original %v, replayed %v",
			s.emu.CursorPosition(), replayed.CursorPosition())
	}
}
