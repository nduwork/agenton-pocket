package main

import (
	"log"
	"net/http"

	"github.com/nduwork/agenton-pocket/internal/web"
)

// runWeb serves the phone bridge in one of two modes (see start_cmd.go):
//
//	tailnet (default) bind this machine's tailnet IP via the system Tailscale app
//	lan               localhost only; no tailnet publish
func runWeb(args []string) {
	sock := flagValue(args, "-socket", defaultSocketPath())
	mode := flagValue(args, "-mode", "tailnet")
	handler := web.Handler(sock)

	switch {
	case mode == "tailnet":
		serveApp(handler) // owns its own listen address; blocks
	case mode == "lan" && !hasFlag(args, "-listen"):
		serveLan(handler) // binds all interfaces, publishes the LAN IP; blocks
	default:
		listen := flagValue(args, "-listen", defaultWebAddr)
		log.Printf("agenton web on http://%s (daemon socket: %s)", listen, sock)
		if err := http.ListenAndServe(listen, handler); err != nil {
			log.Fatal(err)
		}
	}
}
