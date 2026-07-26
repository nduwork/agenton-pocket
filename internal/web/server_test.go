package web

import (
	"bytes"
	"context"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/nduwork/agenton-pocket/internal/daemon"
	"github.com/nduwork/agenton-pocket/internal/protocol"
	"github.com/nduwork/agenton-pocket/internal/transport"
)

// startDaemon runs a real daemon on a temp socket and returns the socket path.
func startDaemon(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	sock := filepath.Join(dir, "agenton.sock")
	d := daemon.New(daemon.Config{Presets: map[string]daemon.Preset{}}, sock)
	ln, err := transport.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	go d.Serve(ln)
	t.Cleanup(func() { ln.Close(); d.Shutdown() })
	return sock
}

type wsConn struct {
	t  *testing.T
	ws *websocket.Conn
}

func dialWS(t *testing.T, url string) *wsConn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(url, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	ws.SetReadLimit(1 << 20)
	t.Cleanup(func() { ws.CloseNow() })
	return &wsConn{t: t, ws: ws}
}

func (c *wsConn) sendControl(env protocol.Envelope) {
	c.t.Helper()
	b, err := protocol.EncodeEnvelope(env)
	if err != nil {
		c.t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := protocol.WriteFrame(&buf, protocol.Frame{Type: protocol.TypeControl, Payload: b}); err != nil {
		c.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.ws.Write(ctx, websocket.MessageBinary, buf.Bytes()); err != nil {
		c.t.Fatal(err)
	}
}

// readFrame reads one WS message and decodes it as exactly one wire frame.
func (c *wsConn) readFrame() protocol.Frame {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, data, err := c.ws.Read(ctx)
	if err != nil {
		c.t.Fatal(err)
	}
	if typ != websocket.MessageBinary {
		c.t.Fatalf("got message type %v, want binary", typ)
	}
	f, err := protocol.ReadFrame(bytes.NewReader(data))
	if err != nil {
		c.t.Fatal(err)
	}
	if len(data) != 9+len(f.Payload) {
		c.t.Fatalf("WS message holds %d bytes, want exactly one frame (%d)", len(data), 9+len(f.Payload))
	}
	return f
}

// waitControl reads frames until a control frame arrives, returning its envelope.
func (c *wsConn) waitControl() protocol.Envelope {
	c.t.Helper()
	for {
		f := c.readFrame()
		if f.Type != protocol.TypeControl {
			continue
		}
		env, err := protocol.DecodeEnvelope(f.Payload)
		if err != nil {
			c.t.Fatal(err)
		}
		return env
	}
}

func echoPath(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", "echo.go"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBridgeFullLifecycle(t *testing.T) {
	sock := startDaemon(t)
	srv := httptest.NewServer(Handler(sock))
	defer srv.Close()

	// entry conn: list (empty) then create
	entry := dialWS(t, srv.URL)
	entry.sendControl(protocol.Envelope{Type: protocol.MsgListSessions})
	if env := entry.waitControl(); env.Type != protocol.MsgSessionList || len(env.Sessions) != 0 {
		t.Fatalf("list: %+v", env)
	}
	entry.sendControl(protocol.Envelope{
		Type: protocol.MsgNewSessionCmd, Command: "go", Args: []string{"run", echoPath(t)}, Agent: "stub",
	})
	state := entry.waitControl()
	if state.Type != protocol.MsgSessionState || state.SessionID == 0 {
		t.Fatalf("new: %+v", state)
	}
	id := state.SessionID

	// the list must expose the full command line so clients can clone sessions
	entry.sendControl(protocol.Envelope{Type: protocol.MsgListSessions})
	if env := entry.waitControl(); len(env.Sessions) != 1 || !strings.HasPrefix(env.Sessions[0].CommandLine, "go run ") {
		t.Fatalf("command_line missing from session list: %+v", env.Sessions)
	}

	// attach conn: drive an echo round-trip through buttons + text
	att := dialWS(t, srv.URL)
	att.sendControl(protocol.Envelope{Type: protocol.MsgAttach, SessionID: id})
	time.Sleep(300 * time.Millisecond) // let the stub print its prompt
	att.sendControl(protocol.Envelope{Type: protocol.MsgTextInput, SessionID: id, Text: "xyz"})
	att.sendControl(protocol.Envelope{Type: protocol.MsgAction, SessionID: id, Action: "accept"})

	var got strings.Builder
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(got.String(), "echo: xyz") {
		if time.Now().After(deadline) {
			t.Fatalf("timeout, got %q", got.String())
		}
		f := att.readFrame()
		if f.Type == protocol.TypeOutput && f.SessionID == id {
			got.Write(f.Payload)
		}
	}

	// reattach on a fresh conn: scrollback replay must contain the exchange
	att.ws.CloseNow()
	att2 := dialWS(t, srv.URL)
	att2.sendControl(protocol.Envelope{Type: protocol.MsgAttach, SessionID: id})
	var replay strings.Builder
	deadline = time.Now().Add(5 * time.Second)
	for !strings.Contains(replay.String(), "echo: xyz") {
		if time.Now().After(deadline) {
			t.Fatalf("timeout on replay, got %q", replay.String())
		}
		f := att2.readFrame()
		if f.Type == protocol.TypeOutput && f.SessionID == id {
			replay.Write(f.Payload)
		}
	}

	// kill via the entry conn
	entry.sendControl(protocol.Envelope{Type: protocol.MsgKillSession, SessionID: id})
	entry.sendControl(protocol.Envelope{Type: protocol.MsgListSessions})
	if env := entry.waitControl(); len(env.Sessions) != 0 {
		t.Fatalf("session not killed: %+v", env.Sessions)
	}
}

func TestStaticServed(t *testing.T) {
	srv := httptest.NewServer(Handler(filepath.Join(t.TempDir(), "nonexistent.sock")))
	defer srv.Close()
	resp, err := srv.Client().Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET / = %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`id="btn-font"`)) {
		t.Fatalf("index.html missing #btn-font button; got: %s", body)
	}
}
