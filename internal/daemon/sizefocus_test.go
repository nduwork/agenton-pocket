package daemon

import (
	"testing"
	"time"

	"github.com/nduwork/agenton-pocket/internal/protocol"
	"github.com/nduwork/agenton-pocket/internal/transport"
)

// Size ownership: the client that owns the shared PTY size holds it. A second
// client resizing (the phone peeking) does NOT steal it. Ownership moves only
// on an explicit set_active (the phone's mode toggle) or when the owner
// disconnects — never on input (that's TestInputDoesNotClaimSize).
func TestSizeOwnership(t *testing.T) {
	sock, d := startTestDaemonD(t)

	laptop, err := transport.DialSocket(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer laptop.Close()
	phone, err := transport.DialSocket(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close()

	sendControl(t, laptop, protocol.Envelope{Type: protocol.MsgNewSession, Preset: "stub"})
	state := recvControl(t, laptop)
	if state.Type != protocol.MsgSessionState || state.SessionID == 0 {
		t.Fatalf("new: %+v", state)
	}
	id := state.SessionID

	// observe the live session's PTY size directly
	s := d.session(id)
	if s == nil {
		t.Fatal("session not found in daemon registry")
	}

	// laptop resizes first (unowned) -> claims ownership, PTY follows it
	sendControl(t, laptop, protocol.Envelope{Type: protocol.MsgResize, SessionID: id, Cols: 120, Rows: 40})
	waitSize(t, s, 120, 40)

	// phone resizes while the laptop owns -> must NOT steal
	sendControl(t, phone, protocol.Envelope{Type: protocol.MsgResize, SessionID: id, Cols: 47, Rows: 20})
	mustStaySize(t, s, 120, 40, 400*time.Millisecond)

	// phone claims explicitly via set_active -> ownership hands over and the
	// phone's remembered size (from its earlier resize) is re-applied at once
	// via refocus, no extra resize needed
	active := true
	sendControl(t, phone, protocol.Envelope{Type: protocol.MsgSetActive, SessionID: id, Active: &active})
	waitSize(t, s, 47, 20)

	// the phone (now owner) disconnects -> ownership releases; the laptop's
	// resize applies again
	phone.Close()
	time.Sleep(300 * time.Millisecond) // let the daemon process the disconnect
	sendControl(t, laptop, protocol.Envelope{Type: protocol.MsgResize, SessionID: id, Cols: 90, Rows: 30})
	waitSize(t, s, 90, 30)
}

// mustStaySize fails if s's PTY size drifts from cols×rows within d.
func mustStaySize(t *testing.T, s *Session, cols, rows int, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if c, r := s.Size(); c != cols || r != rows {
			t.Fatalf("PTY resized to %dx%d, want stable %dx%d", c, r, cols, rows)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitSize(t *testing.T, s *Session, cols, rows int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		c, r := s.Size()
		if c == cols && r == rows {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("size = %dx%d, want %dx%d", c, r, cols, rows)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
