package daemon

import (
	"net"
	"sync"

	"github.com/nduwork/agenton-pocket/internal/protocol"
)

// connWriter serializes all frame writes to one client connection. Control
// replies (handle goroutine), streamed output (streamTo goroutine), and active
// broadcasts all share the same conn, so their writes must not interleave.
type connWriter struct {
	conn net.Conn
	mu   sync.Mutex
}

func newConnWriter(c net.Conn) *connWriter { return &connWriter{conn: c} }

func (w *connWriter) write(f protocol.Frame) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return protocol.WriteFrame(w.conn, f)
}
