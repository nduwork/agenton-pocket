package transport

import (
	"net"
	"os"
)

func Listen(socketPath string) (net.Listener, error) {
	_ = os.Remove(socketPath) // best-effort: clear stale socket
	return net.Listen("unix", socketPath)
}

func DialSocket(socketPath string) (Conn, error) {
	c, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, err
	}
	return c.(*net.UnixConn), nil
}
