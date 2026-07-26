package tui

import (
	"io"
	"sync"

	"github.com/nduwork/agenton-pocket/internal/client"
)

// chanReader adapts a <-chan []byte (the daemon's PTY output stream from
// client.Attach) to an io.Reader for the headless terminal emulator. It blocks
// on the channel until bytes arrive. When the channel closes (stream ended),
// it returns io.EOF exactly once and signals done so the TUI can route back to
// the entry screen.
type chanReader struct {
	ch   <-chan []byte
	buf  []byte
	done chan struct{}
	once sync.Once
}

func newChanReader(ch <-chan []byte) *chanReader {
	return &chanReader{ch: ch, done: make(chan struct{})}
}

func (r *chanReader) Read(p []byte) (int, error) {
	if len(r.buf) > 0 {
		n := copy(p, r.buf)
		r.buf = r.buf[n:]
		return n, nil
	}
	b, ok := <-r.ch
	if !ok {
		r.once.Do(func() { close(r.done) })
		return 0, io.EOF
	}
	n := copy(p, b)
	if n < len(b) {
		// Stash the remainder for the next Read; the channel already consumed it.
		r.buf = append(r.buf[:0], b[n:]...)
	}
	return n, nil
}

// doneCh returns a channel closed when the underlying output stream ends.
func (r *chanReader) doneCh() <-chan struct{} { return r.done }

// stop marks the stream ended without waiting for EOF. Used on explicit
// detach so goroutines waiting on doneCh don't leak.
func (r *chanReader) stop() { r.once.Do(func() { close(r.done) }) }

// forwardingWriter is the io.WriteCloser the emulator writes keyboard input
// and terminal-query responses (DA/DSR) to. Each write is forwarded to the
// daemon's PTY stdin as a text_input control frame.
type forwardingWriter struct {
	c  *client.Client
	id uint32
}

func newForwardingWriter(c *client.Client, id uint32) *forwardingWriter {
	return &forwardingWriter{c: c, id: id}
}

func (w *forwardingWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := w.c.TextInput(w.id, string(p)); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *forwardingWriter) Close() error { return nil }
