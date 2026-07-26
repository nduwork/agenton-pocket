package transport

import (
	"bytes"
	"testing"
)

func TestStdioConnRoundTrip(t *testing.T) {
	var out bytes.Buffer
	in := bytes.NewReader([]byte("ping"))
	c := NewStdioConn(in, &out)
	defer c.Close()
	buf := make([]byte, 4)
	if _, err := c.Read(buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "ping" {
		t.Fatalf("got %q want ping", buf)
	}
	if _, err := c.Write([]byte("pong")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if out.String() != "pong" {
		t.Fatalf("got %q want pong", out.String())
	}
}
