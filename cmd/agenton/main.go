package main

import (
	"fmt"
	"os"
)

// version is stamped at release time via -ldflags "-X main.version=…"
// (see .goreleaser.yaml). Defaults to "dev" for local/source builds.
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		// Bare `agenton` = start everything: daemon + web if missing, then TUI.
		refuseNested("agenton")
		runUp(nil)
		return
	}
	switch os.Args[1] {
	case "up":
		refuseNested("up")
		runUp(os.Args[2:])
	case "daemon":
		refuseNested("daemon")
		runDaemon(os.Args[2:])
	case "client":
		runClient(os.Args[2:])
	case "tui":
		runTUI(os.Args[2:])
	case "web":
		runWeb(os.Args[2:])
	case "qr":
		runQR(os.Args[2:])
	case "version", "-version", "--version":
		fmt.Println(version)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\nusage: agenton [up|daemon|client|tui|web|qr|version] [args]\n", os.Args[1])
		os.Exit(2)
	}
}

// refuseNested stops a daemon (or bare/up, which spawns one) from launching
// inside a shell the daemon itself owns. transport.Listen removes the existing
// socket before binding, so a nested daemon would silently steal the running
// daemon's socket and orphan every attached client. AGENTON_SESSION is set on
// every session's environment by the daemon (see internal/daemon/session.go).
func refuseNested(cmd string) {
	if os.Getenv("AGENTON_SESSION") == "" {
		return
	}
	fmt.Fprintf(os.Stderr,
		"agenton: refusing to run %q — you're already inside an agenton session.\n"+
			"A nested daemon would take over the socket and disconnect your other clients.\n"+
			"Use ctrl+t to get back to the session list; the daemon is already running.\n", cmd)
	os.Exit(1)
}
