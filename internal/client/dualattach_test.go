package client

import (
	"bytes"
	"testing"
	"time"
)

// TestDualAttachBothReceive proves the TUI and the web client can watch the
// same session at once: two independent connections attach to one session and
// both receive the same live output (input injected by either side).
func TestDualAttachBothReceive(t *testing.T) {
	sock := startDaemon(t)

	creator := New(dial(t, sock))
	defer creator.Close()
	id, err := creator.NewSession("stub")
	if err != nil || id == 0 {
		t.Fatalf("new: %v id=%d", err, id)
	}

	a := New(dial(t, sock)) // "the TUI"
	defer a.Close()
	b := New(dial(t, sock)) // "the web client"
	defer b.Close()
	outA, _, err := a.Attach(id)
	if err != nil {
		t.Fatal(err)
	}
	outB, _, err := b.Attach(id)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)

	// input from client A must reach the session; output must reach BOTH.
	if err := a.TextInput(id, "shared"); err != nil {
		t.Fatal(err)
	}
	if err := a.Action(id, "accept"); err != nil {
		t.Fatal(err)
	}

	want := []byte("echo: shared")
	for name, ch := range map[string]<-chan []byte{"A": outA, "B": outB} {
		var got []byte
		deadline := time.After(3 * time.Second)
		for !bytes.Contains(got, want) {
			select {
			case buf, ok := <-ch:
				if !ok {
					t.Fatalf("client %s: stream closed early, got %q", name, got)
				}
				got = append(got, buf...)
			case <-deadline:
				t.Fatalf("client %s: timeout, got %q", name, got)
			}
		}
	}
}

// TestAttachSurfacesActiveState proves Attach's active channel reflects
// ownership changes broadcast by the daemon (Task 3) when a peer calls
// SetActive.
func TestAttachSurfacesActiveState(t *testing.T) {
	sock := startDaemon(t)

	creator := New(dial(t, sock))
	defer creator.Close()
	id, err := creator.NewSession("stub")
	if err != nil || id == 0 {
		t.Fatalf("new: %v id=%d", err, id)
	}

	a := New(dial(t, sock))
	defer a.Close()
	b := New(dial(t, sock))
	defer b.Close()
	_, activeA, err := a.Attach(id)
	if err != nil {
		t.Fatal(err)
	}
	_, activeB, err := b.Attach(id)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)

	if err := b.SetActive(id, true); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(3 * time.Second)
	select {
	case v, ok := <-activeB:
		if !ok || !v {
			t.Fatalf("client B: expected active=true, got %v ok=%v", v, ok)
		}
	case <-deadline:
		t.Fatal("client B: timeout waiting for active update")
	}
	select {
	case v, ok := <-activeA:
		if !ok || v {
			t.Fatalf("client A: expected active=false, got %v ok=%v", v, ok)
		}
	case <-deadline:
		t.Fatal("client A: timeout waiting for active update")
	}
}
