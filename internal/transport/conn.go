package transport

import "io"

// Conn is a framed, duplex connection, satisfied by *net.UnixConn.
type Conn interface {
	io.ReadWriteCloser
}
