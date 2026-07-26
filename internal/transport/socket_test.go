package transport

import (
	"path/filepath"
	"testing"
)

func TestSocketRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agenton.sock")
	ln, err := Listen(path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	accepted := make(chan Conn, 1)
	go func() {
		c, _ := ln.Accept()
		accepted <- c
	}()

	client, err := DialSocket(path)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()
	server := <-accepted
	if server == nil {
		t.Fatal("accept returned nil")
	}
	defer server.Close()

	if _, err := client.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 5)
	if _, err := server.Read(buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "hello" {
		t.Fatalf("got %q want hello", buf)
	}
}
