package main

import (
	"fmt"
	"os"
	"strings"
)

// version is stamped at release time via -ldflags "-X main.version=…"
// (see .goreleaser.yaml). Defaults to "dev" for local/source builds.
var version = "dev"

const usage = `agenton — run coding agents on this machine, reach them from your phone.

usage: agenton [command] [flags]

Commands:
  (none)      start daemon + web if needed, then open the TUI (same as ` + "`up`" + `)
  up          start daemon + web, publish over your tailnet; --lan for localhost only
  daemon      run the session daemon in the foreground
  tui         attach the terminal UI to a running daemon
  web         run the web server in the foreground
  qr          print a QR code to connect a phone (--web for a browser-camera QR)
  stop        stop the background daemon + web server (ends all sessions)
  version     print the version
  help        show this help

Run ` + "`agenton <command> -h`" + ` for a command's flags (e.g. ` + "`agenton up -h`" + `).
`

func main() {
	if len(os.Args) < 2 {
		// Bare `agenton` = start everything: daemon + web if missing, then TUI.
		refuseNested("agenton")
		runUp(nil)
		return
	}
	switch os.Args[1] {
	case "help", "-h", "--help":
		fmt.Print(usage)
	case "up":
		refuseNested("up")
		runUp(os.Args[2:])
	case "daemon":
		refuseNested("daemon")
		runDaemon(os.Args[2:])
	case "tui":
		runTUI(os.Args[2:])
	case "web":
		runWeb(os.Args[2:])
	case "qr":
		runQR(os.Args[2:])
	case "stop":
		runStop(os.Args[2:])
	case "version", "-version", "--version":
		fmt.Println(version)
	default:
		// `agenton --lan`, `agenton --no-tui`, … = the same flags on `up`.
		if strings.HasPrefix(os.Args[1], "-") {
			refuseNested("up")
			runUp(os.Args[1:])
			return
		}
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\nusage: agenton [up|daemon|tui|web|qr|stop|version|help] [args]\n", os.Args[1])
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
