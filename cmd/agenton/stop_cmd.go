package main

import (
	"fmt"
	"os"
	"os/exec"
)

// runStop tears down the background daemon and web server. SIGTERM to the daemon
// is a clean shutdown — it closes every session (see daemon_cmd.go); the web
// server just exits. We match the same command lines the `up` messages already
// tell users to `pkill -f` by hand.
func runStop(args []string) {
	stopped := false
	for _, pat := range []string{"agenton daemon", "agenton web"} {
		if killMatching(pat) {
			stopped = true
		}
	}
	// The web server never removes tailnet.json on exit, so clear it here — else
	// `qr`/`up` would report a dead endpoint after a stop.
	_ = os.Remove(tailnetStatePath())
	if stopped {
		fmt.Println("agenton: stopped.")
	} else {
		fmt.Println("agenton: nothing running.")
	}
}

// killMatching SIGTERMs every process whose full command line contains pattern.
// pkill exits 0 when it signalled something, 1 when nothing matched.
func killMatching(pattern string) bool {
	return exec.Command("pkill", "-TERM", "-f", pattern).Run() == nil
}
