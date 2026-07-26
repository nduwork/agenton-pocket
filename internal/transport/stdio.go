package transport

import "io"

// StdioConn adapts a separate reader/writer into a Conn. Closing is a no-op
// (the process owns stdin/stdout). This is what `agenton client` uses to bridge
// the socket over an SSH session's stdio.
type StdioConn struct {
	r io.Reader
	w io.Writer
}

func NewStdioConn(in io.Reader, out io.Writer) Conn {
	return &StdioConn{r: in, w: out}
}

func (s *StdioConn) Read(p []byte) (int, error)  { return s.r.Read(p) }
func (s *StdioConn) Write(p []byte) (int, error) { return s.w.Write(p) }
func (s *StdioConn) Close() error                { return nil }
