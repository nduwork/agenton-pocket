package main

import (
	"flag"
	"log"

	"github.com/nduwork/agenton-pocket/internal/protocol"
	"github.com/nduwork/agenton-pocket/internal/transport"
)

// runClient bridges a daemon Unix socket to this process's stdio. This is the
// exact path a remote TUI (over Tailscale SSH) would carry: SSH execs
// `agenton client`, and the protocol frames flow over the SSH session's stdio.
func runClient(args []string) {
	fs := flag.NewFlagSet("client", flag.ExitOnError)
	var socket string
	fs.StringVar(&socket, "socket", defaultSocketPath(), "daemon socket path")
	_ = fs.Parse(args)

	up, err := transport.DialSocket(socket)
	if err != nil {
		log.Fatalf("dial daemon: %v", err)
	}
	defer up.Close()

	stdio := transport.NewStdioConn(osStdin(), osStdout())

	// pump: stdio -> daemon
	go func() {
		for {
			f, err := protocol.ReadFrame(stdio)
			if err != nil {
				return
			}
			if err := protocol.WriteFrame(up, f); err != nil {
				return
			}
		}
	}()
	// pump: daemon -> stdio (blocks until either side closes)
	for {
		f, err := protocol.ReadFrame(up)
		if err != nil {
			return
		}
		if err := protocol.WriteFrame(stdio, f); err != nil {
			return
		}
	}
}
