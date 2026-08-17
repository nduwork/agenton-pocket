package tui

import (
	"io"
	"sync"

	"golang.org/x/term"
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

// makeRaw puts the terminal into raw mode: ECHO/ICANON/ISIG/ICRNL off so
// keystrokes pass through unmodified (Enter = \r, ctrl+c = 0x03, no line
// buffering). Returns a restore function.
func makeRaw(fd int) (func() error, error) {
	old, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return func() error { return term.Restore(fd, old) }, nil
}

// crlf inserts \r before bare \n bytes so the host terminal (raw mode, OPOST
// off) renders the agent's newlines as CRLF instead of dropping down a column.
// The daemon PTY is raw, so a child's \n stays \n; most output already carries
// \r\n, and this only touches the bare ones. Escape sequences never contain \n
// (params/final bytes are 0x20-0x7E), so scanning is safe. Returns b unchanged
// when there is nothing to fix.
func crlf(b []byte) []byte {
	hasBare := false
	for i := 0; i < len(b); i++ {
		if b[i] == '\n' && (i == 0 || b[i-1] != '\r') {
			hasBare = true
			break
		}
	}
	if !hasBare {
		return b
	}
	out := make([]byte, 0, len(b)+8)
	for i := 0; i < len(b); i++ {
		if b[i] == '\n' && (i == 0 || b[i-1] != '\r') {
			out = append(out, '\r')
		}
		out = append(out, b[i])
	}
	return out
}
