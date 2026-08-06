package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/nduwork/agenton-pocket/internal/transport"
)

// 9787, not 8787: Dask dashboards own 8787 by default, and bioinformatics
// machines usually have one running.
const defaultWebAddr = "127.0.0.1:9787"

const startUsage = `usage: agenton (vpn | lan) [flags]

Starting agenton means choosing how your phone reaches you:
  agenton vpn           bind this machine's tailnet IP via the system Tailscale
                        app, reachable anywhere. Free plan, nothing to approve —
                        agenton registers no tailnet node of its own.
  agenton lan           bind the local network: publish this machine's LAN IP so
                        phones/browsers on the same Wi-Fi can reach it. No tailnet.

Once running, ` + "`agenton`" + ` (no subcommand) resumes the session; ` + "`agenton stop`" + ` stops it.
Starting is refused while a daemon is already up, so the reach can't change
mid-run.

agenton serves plain HTTP. With vpn the tailnet is the security boundary; with
lan your local network is — nothing is exposed to the internet either way.

Flags:
`

// daemonRunning reports whether an agenton daemon is already listening.
func daemonRunning() bool {
	c, err := transport.DialSocket(defaultSocketPath())
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// runResume is bare `agenton`: reopen the TUI of an already-running daemon.
// Nothing running → print how to start, since the reach (vpn/lan) is never
// chosen for you.
func runResume() {
	if !daemonRunning() {
		fmt.Fprintln(os.Stderr, "agenton: not running. Start it by choosing how your phone reaches you:")
		fmt.Fprintln(os.Stderr, "  agenton vpn   over your tailnet (Tailscale) — works anywhere")
		fmt.Fprintln(os.Stderr, "  agenton lan   over your local network (same Wi-Fi)")
		os.Exit(1)
	}
	runTUI(nil)
}

// runStart brings up the daemon + web in mode ("tailnet" or "lan") and opens the
// TUI. It refuses when a daemon is already running, so a stray `agenton lan`
// can't spin up a second web in a different mode behind the one you started —
// the reach is fixed for the life of the daemon. Quitting the TUI leaves daemon
// + web (and all sessions) running. args carries optional flags (-no-tui).
func runStart(mode string, args []string) {
	fs := flag.NewFlagSet(mode, flag.ExitOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, startUsage); fs.PrintDefaults() }
	noTUI := fs.Bool("no-tui", false, "start daemon + web only (headless/server use)")
	_ = fs.Parse(args)

	if daemonRunning() {
		fmt.Fprintln(os.Stderr, "agenton: already running — the reach is fixed for this run.")
		fmt.Fprintln(os.Stderr, "  agenton        resume the session")
		fmt.Fprintln(os.Stderr, "  agenton stop   stop it (ends all sessions), then start again")
		os.Exit(1)
	}

	sock := defaultSocketPath()
	if err := ensureDaemon(sock); err != nil {
		log.Fatalf("agenton: daemon did not come up: %v", err)
	}
	if err := ensureWeb(mode); err != nil {
		log.Fatalf("agenton: web server did not come up: %v", err)
	}
	fmt.Printf("agenton: daemon ready (%s)\n", sock)

	switch mode {
	case "lan":
		if info, ok := readTailnetInfo(); ok {
			announceEndpoint(info)
		} else {
			// serveLan couldn't resolve a LAN IP and fell back to localhost.
			fmt.Printf("agenton: web ready (http://%s) — localhost only (no LAN IP found).\n", defaultWebAddr)
		}
	default: // tailnet
		if info, ok := readTailnetInfo(); ok {
			announceEndpoint(info)
		} else {
			// serveApp fell back to localhost (no Tailscale app reachable).
			fmt.Printf("agenton: web ready (http://%s) — Tailscale app not detected; localhost only.\n", defaultWebAddr)
			fmt.Println("         start the Tailscale app, then `agenton stop && agenton vpn` to publish")
			fmt.Println("         over the tailnet.")
		}
	}
	if *noTUI {
		return
	}
	runTUI(nil)
}

// announceEndpoint tells the user the published URL and how to reach it: the
// iOS-app QR, the browser URL, and the camera-scannable web QR.
func announceEndpoint(info tailnetInfo) {
	fmt.Printf("agenton: web ready (%s)\n", endpointURL(info))
	printConnectQR(connectURL(info))
	fmt.Printf("agenton: in a browser, open %s — or `agenton qr --web` for a phone-camera QR.\n", endpointURL(info))
}

func stateDir() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".local", "state", "agenton")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// spawnSelf re-executes this binary with a subcommand (plus any extra args),
// detached, with output going to a log file in the state dir. The child
// outlives the caller.
func spawnSelf(subcommand string, extra ...string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	logf, err := os.OpenFile(filepath.Join(stateDir(), subcommand+".log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logf.Close()
	c := exec.Command(self, append([]string{subcommand}, extra...)...)
	c.Stdout = logf
	c.Stderr = logf
	return c.Start() // no Wait: detached; it persists after we exit
}

func ensureDaemon(sock string) error {
	if c, err := transport.DialSocket(sock); err == nil {
		c.Close()
		return nil
	}
	if err := spawnSelf("daemon"); err != nil {
		return err
	}
	return waitFor(func() bool {
		c, err := transport.DialSocket(sock)
		if err != nil {
			return false
		}
		c.Close()
		return true
	})
}

// ensureWeb makes sure a web server for the chosen mode is up, spawning one
// (detached) if not. lan binds localhost, so readiness is the local probe.
// tailnet binds the tailnet IP (or localhost on fallback), so readiness is a
// live published endpoint or a live localhost fallback.
func ensureWeb(mode string) error {
	if mode == "tailnet" {
		return ensureWebTailnet()
	}
	// lan: web binds localhost:9787; the local probe is the readiness gate.
	switch webStatus() {
	case webOurs:
		return nil
	case webForeign:
		return fmt.Errorf("port %s is in use by another app; stop it or run `agenton web -listen <addr>` yourself", defaultWebAddr)
	}
	if err := spawnSelf("web", "-mode", mode); err != nil {
		return err
	}
	return waitFor(func() bool { return webStatus() == webOurs })
}

func ensureWebTailnet() error {
	// Already serving the tailnet? (reopening the TUI must not respawn.)
	if info, ok := readTailnetInfo(); ok && agentonAnswers(tailnetAddr(info)) {
		return nil
	}
	// A localhost-fallback web from an earlier run (no Tailscale app then) still
	// holds :9787, so a respawn could only die on "address already in use".
	// Treat it as ready; runStart tells the user how to restart it to publish.
	if webStatus() == webOurs {
		return nil
	}
	_ = os.Remove(tailnetStatePath()) // clear a stale/dead endpoint before respawn
	if err := spawnSelf("web", "-mode", "tailnet"); err != nil {
		return err
	}
	return waitFor(func() bool {
		// tailnet endpoint published & live, or the localhost fallback is live.
		if info, ok := readTailnetInfo(); ok && agentonAnswers(tailnetAddr(info)) {
			return true
		}
		return webStatus() == webOurs
	})
}

// tailnetAddr is the host:port a running web serves for local probing. The
// Mac resolves its own MagicDNS name via Tailscale.
// ponytail: relies on local MagicDNS resolution; if that proves flaky, record
// the bound IP in tailnet.json and probe that instead.
func tailnetAddr(info tailnetInfo) string {
	return net.JoinHostPort(info.Host, strconv.Itoa(info.Port))
}

type webState int

const (
	webDown webState = iota
	webOurs
	webForeign
)

// agentonAnswers reports whether agenton's /healthz is answering at addr — a
// bare TCP check can't tell us apart from e.g. a Dask dashboard.
func agentonAnswers(addr string) bool {
	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Get("http://" + addr + "/healthz")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16))
	return resp.StatusCode == 200 && string(body) == "agenton"
}

// webStatus probes localhost:9787 and distinguishes agenton (ours) from some
// other app squatting the port (foreign) vs nothing there (down).
func webStatus() webState {
	if agentonAnswers(defaultWebAddr) {
		return webOurs
	}
	if c, derr := net.DialTimeout("tcp", defaultWebAddr, 300*time.Millisecond); derr == nil {
		c.Close()
		return webForeign
	}
	return webDown
}

func waitFor(ok func() bool) error {
	for i := 0; i < 30; i++ {
		if ok() {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timed out after 3s (check logs in %s)", stateDir())
}
