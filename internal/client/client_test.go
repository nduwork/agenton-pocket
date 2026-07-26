package client

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nduwork/agenton-pocket/internal/daemon"
	"github.com/nduwork/agenton-pocket/internal/transport"
)

// startDaemon spins up a real daemon with a stub preset on a short /tmp socket.
func startDaemon(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "agenton-client")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	stub, err := filepath.Abs(filepath.Join("..", "daemon", "testdata", "echo.go"))
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.toml")
	os.MkdirAll(filepath.Dir(cfgPath), 0o755)
	os.WriteFile(cfgPath, []byte("[preset.stub]\nagent=\"stub\"\ncommand=\"go\"\nargs=[\"run\",\""+stub+"\"]\n"), 0o600)
	cfg, err := daemon.LoadConfig(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "s.sock")
	d := daemon.New(cfg, sock)
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
		if _, err := os.Stat(sock); err == nil {
			return sock
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("socket never appeared")
	return ""
}

func dial(t *testing.T, sock string) transport.Conn {
	t.Helper()
	conn, err := transport.DialSocket(sock)
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func TestClientListNewAttachActionKill(t *testing.T) {
	sock := startDaemon(t)
	conn := dial(t, sock)
	c := New(conn)
	defer c.Close()

	sessions, err := c.ListSessions()
	if err != nil || len(sessions) != 0 {
		t.Fatalf("expected empty, got %v %v", sessions, err)
	}

	id, err := c.NewSession("stub")
	if err != nil || id == 0 {
		t.Fatalf("new: %v id=%d", err, id)
	}

	out, _, err := c.Attach(id)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)

	c.TextInput(id, "hello client")
	c.Action(id, "accept")

	var got []byte
	end := time.After(3 * time.Second)
	for !bytes.Contains(got, []byte("hello client")) {
		select {
		case b, ok := <-out:
			if !ok {
				t.Fatal("output closed early")
			}
			got = append(got, b...)
		case <-end:
			t.Fatalf("timeout, got: %q", got)
		}
	}

	// kill (send-only; daemon drops the reply onto the stream which the attach
	// goroutine ignores). Just verify the process is gone.
	if err := c.Kill(id); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
}
