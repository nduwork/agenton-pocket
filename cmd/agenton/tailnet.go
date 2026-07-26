package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"tailscale.com/client/tailscale"
	"tailscale.com/ipn/ipnstate"
)

// tailnetInfo is the resolved tailnet endpoint the phone should hit. The web
// process writes it; `qr` and `up` read it. Keeping the handoff in a file means
// the short-lived qr/up processes never have to talk to Tailscale themselves.
type tailnetInfo struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

func tailnetStatePath() string { return filepath.Join(stateDir(), "tailnet.json") }

// endpointURL renders the human-facing URL for a published endpoint. agenton
// always serves plain HTTP; the tailnet is the security boundary.
func endpointURL(info tailnetInfo) string {
	return fmt.Sprintf("http://%s:%d", info.Host, info.Port)
}

// connectURL builds the agenton:// payload the iOS app scans from the QR.
func connectURL(info tailnetInfo) string {
	q := url.Values{"host": {info.Host}, "port": {strconv.Itoa(info.Port)}}
	return "agenton://connect?" + q.Encode()
}

func writeTailnetInfo(info tailnetInfo) error {
	b, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return os.WriteFile(tailnetStatePath(), b, 0o644)
}

// readTailnetInfo returns the last-published endpoint, or ok=false if none has
// been written yet (server not started, or no reachable tailnet).
func readTailnetInfo() (tailnetInfo, bool) {
	b, err := os.ReadFile(tailnetStatePath())
	if err != nil {
		return tailnetInfo{}, false
	}
	var info tailnetInfo
	if json.Unmarshal(b, &info) != nil || info.Host == "" {
		return tailnetInfo{}, false
	}
	return info, true
}

// selectAppEndpoint picks the host + IPv4 for tailnet mode from the system
// tailscaled's status: the MagicDNS name (trailing dot trimmed) to advertise to
// the phone, and the first IPv4 tailnet address to bind. Pure; unit-tested.
func selectAppEndpoint(st *ipnstate.Status) (host, ip string, err error) {
	if st == nil || st.Self == nil {
		return "", "", fmt.Errorf("no Self in tailscaled status")
	}
	host = strings.TrimSuffix(st.Self.DNSName, ".")
	if host == "" {
		return "", "", fmt.Errorf("no MagicDNS name (is MagicDNS enabled?)")
	}
	for _, a := range st.Self.TailscaleIPs {
		if a.Is4() {
			return host, a.String(), nil
		}
	}
	return "", "", fmt.Errorf("no IPv4 tailnet address on this node")
}

// serveApp is the default path: query the *system* Tailscale app's tailscaled,
// bind the machine's own tailnet IP on :9787 (tailnet-only — not 0.0.0.0), and
// advertise the MagicDNS host over plain HTTP. agenton registers no tailnet node
// of its own, so it works on the free plan with nothing to approve. If the
// Tailscale app isn't running, fall back to localhost so desk use still works
// (and write no tailnet.json, so `qr` reports no endpoint). Blocks serving.
func serveApp(handler http.Handler) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	st, err := (&tailscale.LocalClient{}).Status(ctx)
	cancel()
	if err != nil {
		log.Printf("agenton: Tailscale app not running; serving localhost only. " +
			"Start the Tailscale app and re-run for phone access.")
		serveLocal(handler)
		return
	}
	host, ip, err := selectAppEndpoint(st)
	if err != nil {
		log.Printf("agenton: tailnet endpoint unavailable (%v); serving localhost only.", err)
		serveLocal(handler)
		return
	}
	addr := net.JoinHostPort(ip, "9787")
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("agenton: cannot bind tailnet address %s: %v", addr, err)
	}
	// Write tailnet.json only after a successful bind, so its presence is a real
	// liveness signal for `up`/`qr` (not a stale file from a dead run).
	_ = writeTailnetInfo(tailnetInfo{Host: host, Port: 9787})
	log.Printf("agenton: serving http://%s:9787 over the tailnet", host)
	log.Printf("agenton: serve ended: %v", http.Serve(ln, handler))
}

// serveLocal binds localhost only — the tailnet-mode fallback when there's no
// reachable tailnet. No tailnet.json is written.
func serveLocal(handler http.Handler) {
	log.Printf("agenton web on http://%s", defaultWebAddr)
	if err := http.ListenAndServe(defaultWebAddr, handler); err != nil {
		log.Fatal(err)
	}
}
