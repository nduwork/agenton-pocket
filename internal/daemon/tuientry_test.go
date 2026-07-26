package daemon

import (
	"testing"
	"time"

	"github.com/nduwork/agenton-pocket/internal/protocol"
	"github.com/nduwork/agenton-pocket/internal/transport"
)

// Entering the TUI claims the session: the desk is primary. This replays the
// exact wire sequence the TUI emits on entry (attach → resize → set_active) and
// asserts both effects the user asked for — the remote (phone) is parked into
// Controller mode (active=false), and the PTY is reflowed to the desk
// terminal's dimensions rather than staying at the phone's.
func TestTUIEntryClaimsSizeAndParksRemote(t *testing.T) {
	sock, d := startTestDaemonD(t)

	phone, err := transport.DialSocket(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close()
	tui, err := transport.DialSocket(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer tui.Close()

	// A phone opened the session first and owns the size at phone dimensions.
	sendControl(t, phone, protocol.Envelope{Type: protocol.MsgNewSession, Preset: "stub"})
	id := recvControl(t, phone).SessionID
	s := d.session(id)
	if s == nil {
		t.Fatal("session not found")
	}
	sendControl(t, phone, protocol.Envelope{Type: protocol.MsgAttach, SessionID: id})
	time.Sleep(150 * time.Millisecond) // let streamTo register before the claim broadcast
	sendControl(t, phone, protocol.Envelope{Type: protocol.MsgResize, SessionID: id, Cols: 47, Rows: 20})
	waitSize(t, s, 47, 20)
	wantActive(t, recvControl(t, phone), true) // resize (unowned) claimed → phone active

	// --- desk TUI entry: attach, resize to desk dims, then claim ---
	sendControl(t, tui, protocol.Envelope{Type: protocol.MsgAttach, SessionID: id})
	time.Sleep(150 * time.Millisecond) // let streamTo register before we claim
	wantActive(t, recvControl(t, tui), false) // phone owns → TUI attaches parked
	sendControl(t, tui, protocol.Envelope{Type: protocol.MsgResize, SessionID: id, Cols: 160, Rows: 50})
	take := true
	sendControl(t, tui, protocol.Envelope{Type: protocol.MsgSetActive, SessionID: id, Active: &take})

	// PTY optimized for the TUI (refocus re-applied the desk dims we remembered).
	waitSize(t, s, 160, 50)
	// TUI now owns; the phone is told it's parked → Controller mode.
	wantActive(t, recvControl(t, tui), true)
	wantActive(t, recvControl(t, phone), false)
}

// wantActive fails unless env is a session_state carrying active == want.
func wantActive(t *testing.T, env protocol.Envelope, want bool) {
	t.Helper()
	if env.Type != protocol.MsgSessionState || env.Active == nil {
		t.Fatalf("want session_state with active, got %+v", env)
	}
	if *env.Active != want {
		t.Fatalf("active = %v, want %v", *env.Active, want)
	}
}
