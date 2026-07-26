package daemon

import (
	"testing"
	"time"

	"github.com/nduwork/agenton-pocket/internal/protocol"
	"github.com/nduwork/agenton-pocket/internal/transport"
)

// A custom-button rebind lives on the persistent Session, so a client that
// detaches and re-attaches (or a second client) must be told the current
// bindings — the daemon pushes them as set_button frames on attach.
func TestAttachRestoresCustomButtons(t *testing.T) {
	sock, d := startTestDaemonD(t)
	c, err := transport.DialSocket(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	sendControl(t, c, protocol.Envelope{Type: protocol.MsgNewSession, Preset: "stub"})
	id := recvControl(t, c).SessionID
	s := d.session(id)
	if s == nil {
		t.Fatal("session not found")
	}
	if err := s.SetCustom("custom_1", "Ctrl+O"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCustom("custom_2", "/compact"); err != nil {
		t.Fatal(err)
	}

	// A fresh client attaches → the daemon restores both bindings.
	client, err := transport.DialSocket(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	sendControl(t, client, protocol.Envelope{Type: protocol.MsgAttach, SessionID: id})

	got := map[string]string{}
	for len(got) < 2 {
		f, err := protocol.ReadFrame(client)
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
		if e.Type == protocol.MsgSetButton {
			got[e.Action] = e.Text
		}
	}
	if got["custom_1"] != "Ctrl+O" || got["custom_2"] != "/compact" {
		t.Errorf("restored bindings = %v, want custom_1=Ctrl+O custom_2=/compact", got)
	}
}

// When the phone toggles to Controller mode it releases the size; the desk TUI
// must be handed ownership and told it's active again (spec R4). Replays:
// TUI claims → phone claims (TUI parks) → phone releases → TUI must reactivate.
func TestPhoneReleaseReactivatesTUI(t *testing.T) {
	sock, d := startTestDaemonD(t)

	tui, err := transport.DialSocket(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer tui.Close()
	phone, err := transport.DialSocket(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close()

	sendControl(t, tui, protocol.Envelope{Type: protocol.MsgNewSession, Preset: "stub"})
	id := recvControl(t, tui).SessionID
	s := d.session(id)
	if s == nil {
		t.Fatal("session not found")
	}

	// TUI attaches and claims (entering the TUI is primary). Settle so streamTo
	// registers the subscriber before the broadcast (real clients have this gap).
	sendControl(t, tui, protocol.Envelope{Type: protocol.MsgAttach, SessionID: id})
	time.Sleep(150 * time.Millisecond)
	take, release := true, false
	sendControl(t, tui, protocol.Envelope{Type: protocol.MsgSetActive, SessionID: id, Active: &take})
	wantActive(t, recvControl(t, tui), true)

	// Phone attaches (unicast: parked) and then claims Phone mode → TUI parks.
	sendControl(t, phone, protocol.Envelope{Type: protocol.MsgAttach, SessionID: id})
	time.Sleep(150 * time.Millisecond)
	wantActive(t, recvControl(t, phone), false)
	sendControl(t, phone, protocol.Envelope{Type: protocol.MsgSetActive, SessionID: id, Active: &take})
	wantActive(t, recvControl(t, phone), true)
	wantActive(t, recvControl(t, tui), false)

	// Phone toggles to Controller → releases. The TUI must be reactivated.
	sendControl(t, phone, protocol.Envelope{Type: protocol.MsgSetActive, SessionID: id, Active: &release})
	wantActive(t, recvControl(t, tui), true)
	wantActive(t, recvControl(t, phone), false)

	if o := s.ownerConn(); o == nil {
		t.Fatal("after phone release, session has no owner (TUI never reactivated)")
	}
}
