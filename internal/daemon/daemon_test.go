package daemon

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/nduwork/agenton-pocket/internal/protocol"
	"github.com/nduwork/agenton-pocket/internal/transport"
)

// startTestDaemon spins up a daemon with a "stub" preset backed by the testdata
// echo program, listening on a temp Unix socket.
func startTestDaemon(t *testing.T) string {
	sock, _ := startTestDaemonD(t)
	return sock
}

// startTestDaemonD also returns the *Daemon for tests that need to inspect
// live sessions.
func startTestDaemonD(t *testing.T) (string, *Daemon) {
	t.Helper()
	dir, err := osMkdirTemp("/tmp", "agenton")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = osRemoveAll(dir) })
	stub, err := filepath.Abs(filepath.Join("testdata", "echo.go"))
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.toml")
	writeFile(t, cfgPath, "[preset.stub]\nagent=\"stub\"\ncommand=\"go\"\nargs=[\"run\",\""+stub+"\"]\n")
	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "s.sock") // short name to fit macOS 104-char socket limit
	d := New(cfg, sock)
	ln, err := transport.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		d.Shutdown()
		ln.Close()
	})
	go d.Serve(ln)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := osStat(sock); err == nil {
			return sock, d
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("socket never appeared")
	return "", nil
}

func sendControl(t *testing.T, conn transport.Conn, env protocol.Envelope) {
	t.Helper()
	b, err := protocol.EncodeEnvelope(env)
	if err != nil {
		t.Fatal(err)
	}
	if err := protocol.WriteFrame(conn, protocol.Frame{Type: protocol.TypeControl, Payload: b}); err != nil {
		t.Fatal(err)
	}
}

func recvControl(t *testing.T, conn transport.Conn) protocol.Envelope {
	t.Helper()
	for {
		f, err := protocol.ReadFrame(conn)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if f.Type == protocol.TypeControl {
			e, err := protocol.DecodeEnvelope(f.Payload)
			if err != nil {
				t.Fatal(err)
			}
			// The daemon restores custom-button bindings on attach via set_button
			// pushes; they precede the session_state frames these helpers assert on,
			// so skip them (TestAttachRestoresCustomButtons covers them directly).
			if e.Type == protocol.MsgSetButton {
				continue
			}
			return e
		}
	}
}

func drainOutput(t *testing.T, conn transport.Conn, want []byte, deadline time.Duration) {
	t.Helper()
	var got []byte
	end := time.After(deadline)
	for !bytes.Contains(got, want) {
		select {
		case <-end:
			t.Fatalf("timeout, got: %q", got)
		default:
		}
		f, err := protocol.ReadFrame(conn)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if f.Type == protocol.TypeOutput {
			got = append(got, f.Payload...)
		}
	}
}

func TestDaemonListNewAttachAction(t *testing.T) {
	sock := startTestDaemon(t)
	conn, err := transport.DialSocket(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// list -> empty
	sendControl(t, conn, protocol.Envelope{Type: protocol.MsgListSessions})
	list := recvControl(t, conn)
	if list.Type != protocol.MsgSessionList || len(list.Sessions) != 0 {
		t.Fatalf("got %+v", list)
	}

	// new
	sendControl(t, conn, protocol.Envelope{Type: protocol.MsgNewSession, Preset: "stub"})
	state := recvControl(t, conn)
	if state.Type != protocol.MsgSessionState || state.Status != "running" || state.SessionID == 0 {
		t.Fatalf("got %+v", state)
	}
	id := state.SessionID

	// attach on a separate conn to read the output stream
	outConn, err := transport.DialSocket(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer outConn.Close()
	sendControl(t, outConn, protocol.Envelope{Type: protocol.MsgAttach, SessionID: id})
	time.Sleep(200 * time.Millisecond)

	// text + accept via the control conn
	sendControl(t, conn, protocol.Envelope{Type: protocol.MsgTextInput, SessionID: id, Text: "hello"})
	sendControl(t, conn, protocol.Envelope{Type: protocol.MsgAction, SessionID: id, Action: "accept"})

	drainOutput(t, outConn, []byte("echo: hello"), 3*time.Second)

	// kill is fire-and-forget (no reply); verify via a fresh list that the
	// session is gone and the list is not corrupted by an orphan reply.
	sendControl(t, conn, protocol.Envelope{Type: protocol.MsgKillSession, SessionID: id})
	time.Sleep(150 * time.Millisecond)
	sendControl(t, conn, protocol.Envelope{Type: protocol.MsgListSessions})
	after := recvControl(t, conn)
	if after.Type != protocol.MsgSessionList {
		t.Fatalf("expected session_list after kill, got %+v", after)
	}
	for _, s := range after.Sessions {
		if s.ID == id {
			t.Fatalf("session %d should be gone, still listed: %+v", id, after.Sessions)
		}
	}
}

// TestDaemonNewSessionCmd verifies an ad-hoc session can be launched from a
// raw command line (no preset) and attached to like a preset session.
func TestDaemonNewSessionCmd(t *testing.T) {
	sock := startTestDaemon(t)
	conn, err := transport.DialSocket(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	stub, _ := filepath.Abs(filepath.Join("testdata", "echo.go"))
	sendControl(t, conn, protocol.Envelope{
		Type:    protocol.MsgNewSessionCmd,
		Command: "go",
		Args:    []string{"run", stub},
		Agent:   "echo",
	})
	state := recvControl(t, conn)
	if state.Type != protocol.MsgSessionState || state.Status != "running" || state.SessionID == 0 {
		t.Fatalf("got %+v", state)
	}
	id := state.SessionID

	outConn, err := transport.DialSocket(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer outConn.Close()
	sendControl(t, outConn, protocol.Envelope{Type: protocol.MsgAttach, SessionID: id})
	time.Sleep(200 * time.Millisecond)

	sendControl(t, conn, protocol.Envelope{Type: protocol.MsgTextInput, SessionID: id, Text: "world"})
	sendControl(t, conn, protocol.Envelope{Type: protocol.MsgAction, SessionID: id, Action: "accept"})
	drainOutput(t, outConn, []byte("echo: world"), 3*time.Second)
}

// attachTestClient dials a fresh conn and attaches it to id. The same conn is
// used for control sends (resize/set_active) and control reads (session_state
// broadcasts), mirroring how a real client shares one connection.
func attachTestClient(t *testing.T, sock string, id uint32) transport.Conn {
	t.Helper()
	conn, err := transport.DialSocket(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	sendControl(t, conn, protocol.Envelope{Type: protocol.MsgAttach, SessionID: id})
	return conn
}

// waitActive reads control frames from conn until a session_state with a
// non-nil Active arrives, and returns its value.
func waitActive(t *testing.T, conn transport.Conn) bool {
	t.Helper()
	if dc, ok := conn.(interface{ SetReadDeadline(time.Time) error }); ok {
		_ = dc.SetReadDeadline(time.Now().Add(3 * time.Second))
	}
	for {
		f, err := protocol.ReadFrame(conn)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if f.Type != protocol.TypeControl {
			continue
		}
		e, err := protocol.DecodeEnvelope(f.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if e.Type == protocol.MsgSessionState && e.Active != nil {
			return *e.Active
		}
	}
}

// TestSetActiveMovesOwnershipAndBroadcasts verifies that size ownership moves
// between attached clients via resize and set_active, and that every
// ownership change broadcasts a per-client session_state.active to all
// subscribers of the session.
func TestSetActiveMovesOwnershipAndBroadcasts(t *testing.T) {
	sock, d := startTestDaemonD(t)
	conn, err := transport.DialSocket(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sendControl(t, conn, protocol.Envelope{Type: protocol.MsgNewSession, Preset: "stub"})
	state := recvControl(t, conn)
	if state.Type != protocol.MsgSessionState || state.SessionID == 0 {
		t.Fatalf("got %+v", state)
	}
	id := state.SessionID
	s := d.session(id)
	if s == nil {
		t.Fatal("session not found")
	}

	a := attachTestClient(t, sock, id)
	b := attachTestClient(t, sock, id)
	time.Sleep(100 * time.Millisecond) // let both subscriptions register before a resizes

	// a resizes first -> claims ownership; both learn a is active.
	sendControl(t, a, protocol.Envelope{Type: protocol.MsgResize, SessionID: id, Cols: 100, Rows: 40})
	if got := waitActive(t, a); !got {
		t.Fatal("a should be active")
	}
	if got := waitActive(t, b); got {
		t.Fatal("b should be parked")
	}

	// b takes over via set_active.
	active := true
	sendControl(t, b, protocol.Envelope{Type: protocol.MsgSetActive, SessionID: id, Active: &active})
	if got := waitActive(t, b); !got {
		t.Fatal("b should be active after set_active")
	}
	if got := waitActive(t, a); got {
		t.Fatal("a should be parked after b took over")
	}

	if cols, _ := s.Size(); cols != 100 {
		t.Fatalf("PTY should hold a's size until b resizes; got %d", cols)
	}
}

// expectNoActiveWithin fails the test if conn receives a session_state with a
// non-nil Active within wait — used to confirm a lone first attacher isn't
// unsolicitedly parked.
func expectNoActiveWithin(t *testing.T, conn transport.Conn, wait time.Duration) {
	t.Helper()
	dc, ok := conn.(interface{ SetReadDeadline(time.Time) error })
	if !ok {
		t.Fatal("conn does not support SetReadDeadline")
	}
	_ = dc.SetReadDeadline(time.Now().Add(wait))
	for {
		f, err := protocol.ReadFrame(conn)
		if err != nil {
			return // deadline hit (or conn closed) with nothing unwanted received
		}
		if f.Type != protocol.TypeControl {
			continue
		}
		e, err := protocol.DecodeEnvelope(f.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if e.Type == protocol.MsgSessionState && e.Active != nil {
			t.Fatalf("unexpected unsolicited session_state.active=%v", *e.Active)
		}
	}
}

// TestAttachIntoOwnedSessionParksNewcomer verifies the fix for the "newcomer
// never told current ownership" bug: a client attaching into a session that
// another client already owns must be told right away (active:false), not
// left to default to active:true until the next ownership change. Also
// verifies the inverse: a lone/first attacher must NOT be unsolicitedly
// parked just for attaching into an empty (unowned) session.
func TestAttachIntoOwnedSessionParksNewcomer(t *testing.T) {
	sock, d := startTestDaemonD(t)
	conn, err := transport.DialSocket(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sendControl(t, conn, protocol.Envelope{Type: protocol.MsgNewSession, Preset: "stub"})
	state := recvControl(t, conn)
	if state.Type != protocol.MsgSessionState || state.SessionID == 0 {
		t.Fatalf("got %+v", state)
	}
	id := state.SessionID
	if d.session(id) == nil {
		t.Fatal("session not found")
	}

	// A attaches first into an unowned session: must not be unsolicitedly
	// parked just for attaching.
	a := attachTestClient(t, sock, id)
	expectNoActiveWithin(t, a, 300*time.Millisecond)

	// A claims ownership via resize.
	sendControl(t, a, protocol.Envelope{Type: protocol.MsgResize, SessionID: id, Cols: 100, Rows: 40})
	if got := waitActive(t, a); !got {
		t.Fatal("a should be active after claiming size")
	}

	// B attaches into the now-owned session: must be told right away that
	// it's parked, without any further interaction.
	b := attachTestClient(t, sock, id)
	if got := waitActive(t, b); got {
		t.Fatal("b should be parked immediately on attach into an owned session")
	}
}

// TestInputDoesNotClaimSize verifies that input from a parked client (a button
// action or text) does NOT claim the shared PTY size or flip ownership — a
// phone in Controller mode is a pure remote control. Ownership changes only via
// set_active (the toggle) or the TUI's explicit take-over on a parked keypress.
func TestInputDoesNotClaimSize(t *testing.T) {
	sock, d := startTestDaemonD(t)
	conn, err := transport.DialSocket(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	sendControl(t, conn, protocol.Envelope{Type: protocol.MsgNewSession, Preset: "stub"})
	state := recvControl(t, conn)
	if state.Type != protocol.MsgSessionState || state.SessionID == 0 {
		t.Fatalf("got %+v", state)
	}
	id := state.SessionID
	s := d.session(id)
	if s == nil {
		t.Fatal("session not found")
	}

	a := attachTestClient(t, sock, id)
	b := attachTestClient(t, sock, id)
	time.Sleep(100 * time.Millisecond) // let both subscriptions register

	// a claims ownership via resize; drain the resulting broadcasts.
	sendControl(t, a, protocol.Envelope{Type: protocol.MsgResize, SessionID: id, Cols: 100, Rows: 40})
	if got := waitActive(t, a); !got {
		t.Fatal("a should be active")
	}
	if got := waitActive(t, b); got {
		t.Fatal("b should be parked")
	}

	// b (parked) drives the agent — an action and a text input. Neither may
	// claim the size or flip ownership: no session_state.active should arrive,
	// and the PTY must keep a's size.
	sendControl(t, b, protocol.Envelope{Type: protocol.MsgAction, SessionID: id, Action: "up"})
	sendControl(t, b, protocol.Envelope{Type: protocol.MsgTextInput, SessionID: id, Text: "x"})
	expectNoActiveWithin(t, b, 400*time.Millisecond)

	if cols, _ := s.Size(); cols != 100 {
		t.Fatalf("parked client input must not resize the PTY; got %d, want 100", cols)
	}
	if s.ownerConn() == nil {
		t.Fatal("ownership should still be held by a, not cleared by b's input")
	}
}
