package client

import (
	"sync"

	"github.com/nduwork/agenton-pocket/internal/protocol"
	"github.com/nduwork/agenton-pocket/internal/transport"
)

type Client struct {
	conn transport.Conn
	mu   sync.Mutex // serializes frame writes across goroutines
}

func New(conn transport.Conn) *Client { return &Client{conn: conn} }

func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) send(env protocol.Envelope) error {
	b, err := protocol.EncodeEnvelope(env)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return protocol.WriteFrame(c.conn, protocol.Frame{Type: protocol.TypeControl, Payload: b})
}

// roundtrip writes a request and returns the next control-frame reply while
// holding the lock the whole time, so concurrent callers (e.g. the TUI's
// periodic session-list poll racing a user action) can't interleave each
// other's replies on the shared connection. Only control clients use this;
// session clients are send-only + a separate attach read goroutine.
func (c *Client) roundtrip(env protocol.Envelope) (protocol.Envelope, error) {
	b, err := protocol.EncodeEnvelope(env)
	if err != nil {
		return protocol.Envelope{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := protocol.WriteFrame(c.conn, protocol.Frame{Type: protocol.TypeControl, Payload: b}); err != nil {
		return protocol.Envelope{}, err
	}
	for {
		f, err := protocol.ReadFrame(c.conn)
		if err != nil {
			return protocol.Envelope{}, err
		}
		if f.Type == protocol.TypeControl {
			return protocol.DecodeEnvelope(f.Payload)
		}
	}
}

func (c *Client) ListSessions() ([]protocol.SessionInfo, error) {
	env, err := c.roundtrip(protocol.Envelope{Type: protocol.MsgListSessions})
	if err != nil {
		return nil, err
	}
	if env.Type == protocol.MsgError {
		return nil, &Error{Message: env.Message}
	}
	return env.Sessions, nil
}

func (c *Client) NewSession(preset string) (uint32, error) {
	env, err := c.roundtrip(protocol.Envelope{Type: protocol.MsgNewSession, Preset: preset})
	if err != nil {
		return 0, err
	}
	if env.Type == protocol.MsgError {
		return 0, &Error{Message: env.Message}
	}
	return env.SessionID, nil
}

// NewSessionCmd launches an ad-hoc session from a raw command line rather than
// a configured preset. agent is a free label for the entry list (defaults to
// command when empty). cwd may be "" to inherit the daemon's working dir.
func (c *Client) NewSessionCmd(command, agent, cwd string, args []string) (uint32, error) {
	env, err := c.roundtrip(protocol.Envelope{
		Type:    protocol.MsgNewSessionCmd,
		Command: command,
		Args:    args,
		Cwd:     cwd,
		Agent:   agent,
	})
	if err != nil {
		return 0, err
	}
	if env.Type == protocol.MsgError {
		return 0, &Error{Message: env.Message}
	}
	return env.SessionID, nil
}

// Attach returns a channel of raw output bytes for a session, plus a channel of
// active-state updates (true/false on each ownership change, e.g. multiple
// clients sharing one PTY). It spawns a reader goroutine that emits scrollback
// replay then live output. Action/TextInput are sent over the same underlying
// conn — the attach goroutine only forwards TypeOutput frames and
// session_state control frames; other control replies (none expected) are
// simply dropped.
func (c *Client) Attach(id uint32) (<-chan []byte, <-chan bool, error) {
	if err := c.send(protocol.Envelope{Type: protocol.MsgAttach, SessionID: id}); err != nil {
		return nil, nil, err
	}
	out := make(chan []byte, 64)
	active := make(chan bool, 8)
	go func() {
		defer close(out)
		defer close(active)
		for {
			f, err := protocol.ReadFrame(c.conn)
			if err != nil {
				return
			}
			switch f.Type {
			case protocol.TypeOutput:
				cp := make([]byte, len(f.Payload))
				copy(cp, f.Payload)
				// Blocking send: dropping frames here would cut bytes out of
				// the middle of the VT escape stream and corrupt the terminal
				// emulator's state (screen freezes/garbles). Backpressure is
				// safe — the consumer is the local emulator read loop.
				out <- cp
			case protocol.TypeControl:
				if e, err := protocol.DecodeEnvelope(f.Payload); err == nil &&
					e.Type == protocol.MsgSessionState {
					if e.Status == "exited" || e.Status == "killed" {
						// The session ended — stop the stream. The deferred
						// close(out)/close(active) surface as end-of-stream, which
						// routes the TUI back to the session list.
						return
					}
					if e.Active != nil {
						select {
						case active <- *e.Active:
						default: // never block the output path on a slow active reader
						}
					}
				}
			}
		}
	}()
	return out, active, nil
}

// SetActive requests (take=true) or releases (take=false) active ownership of
// a shared session's PTY.
func (c *Client) SetActive(id uint32, take bool) error {
	return c.send(protocol.Envelope{Type: protocol.MsgSetActive, SessionID: id, Active: &take})
}

func (c *Client) Action(id uint32, action string) error {
	return c.send(protocol.Envelope{Type: protocol.MsgAction, SessionID: id, Action: action})
}

// SetButton rebinds a customizable pad button (custom_1 or custom_2) to a
// single key name / combo or literal string, for this session only.
func (c *Client) SetButton(id uint32, action, value string) error {
	return c.send(protocol.Envelope{Type: protocol.MsgSetButton, SessionID: id, Action: action, Text: value})
}

func (c *Client) TextInput(id uint32, text string) error {
	return c.send(protocol.Envelope{Type: protocol.MsgTextInput, SessionID: id, Text: text})
}

// Resize updates the remote PTY size. Fire-and-forget (no reply) for the same
// reason as Kill: a reply would orphan on the next control recv.
func (c *Client) Resize(id uint32, cols, rows int) error {
	return c.send(protocol.Envelope{Type: protocol.MsgResize, SessionID: id, Cols: cols, Rows: rows})
}

func (c *Client) Kill(id uint32) error {
	return c.send(protocol.Envelope{Type: protocol.MsgKillSession, SessionID: id})
}

type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }
