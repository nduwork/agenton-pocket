package client

import (
	"bytes"
	"testing"
	"time"
)

// TestClientDetachReattachScrollback exercises the full lifecycle over a local
// socket: create → attach → input → detach (close conn, session survives) →
// reattach on a fresh conn → scrollback replays prior output → kill removes it.
func TestClientDetachReattachScrollback(t *testing.T) {
	sock := startDaemon(t)

	// conn1: create + attach + produce output
	conn1 := dial(t, sock)
	c1 := New(conn1)
	id, err := c1.NewSession("stub")
	if err != nil || id == 0 {
		t.Fatalf("new: %v id=%d", err, id)
	}
	out1, _, err := c1.Attach(id)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	c1.TextInput(id, "persist-1")
	c1.Action(id, "accept")

	// wait for the echoed output on conn1
	var got []byte
	end := time.After(3 * time.Second)
	for !bytes.Contains(got, []byte("echo: persist-1")) {
		select {
		case b, ok := <-out1:
			if !ok {
				t.Fatal("stream closed early")
			}
			got = append(got, b...)
		case <-end:
			t.Fatalf("timeout waiting for output, got: %q", got)
		}
	}

	// detach: close conn1. The session must survive.
	if err := c1.Close(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)

	// conn2: reattach; scrollback must replay the prior output.
	c2 := New(dial(t, sock))
	defer c2.Close()
	out2, _, err := c2.Attach(id)
	if err != nil {
		t.Fatal(err)
	}
	var got2 []byte
	end2 := time.After(3 * time.Second)
	for !bytes.Contains(got2, []byte("persist-1")) {
		select {
		case b, ok := <-out2:
			if !ok {
				t.Fatalf("scrollback missing prior output, got: %q", got2)
			}
			got2 = append(got2, b...)
		case <-end2:
			t.Fatalf("reattach timeout, got: %q", got2)
		}
	}

	// kill, then verify the session is gone from the listing.
	if err := c2.Kill(id); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	// open a fresh control conn to list (c2's read loop is owned by attach)
	c3 := New(dial(t, sock))
	defer c3.Close()
	sessions, err := c3.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sessions {
		if s.ID == id {
			t.Fatalf("session %d should be gone, still listed: %+v", id, sessions)
		}
	}
}
