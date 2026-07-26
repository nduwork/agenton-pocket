package transport

import "io"

// Conn is a framed, duplex connection. Both *net.UnixConn and StdioConn satisfy it.
type Conn interface {
	io.ReadWriteCloser
}
