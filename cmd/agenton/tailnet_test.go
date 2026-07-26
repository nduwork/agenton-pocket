package main

import (
	"net/netip"
	"os"
	"strings"
	"testing"

	"tailscale.com/ipn/ipnstate"
)

func TestSelectAppEndpoint(t *testing.T) {
	st := &ipnstate.Status{Self: &ipnstate.PeerStatus{
		DNSName: "nduworkstation.tail04aac9.ts.net.", // trailing dot, as tailscaled reports
		TailscaleIPs: []netip.Addr{
			netip.MustParseAddr("fd7a:115c:a1e0::1234"), // v6 first — must be skipped
			netip.MustParseAddr("100.106.40.12"),
		},
	}}
	host, ip, err := selectAppEndpoint(st)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if host != "nduworkstation.tail04aac9.ts.net" {
		t.Errorf("host = %q, want trailing dot trimmed", host)
	}
	if ip != "100.106.40.12" {
		t.Errorf("ip = %q, want the v4 address", ip)
	}
}

func TestSelectAppEndpoint_Errors(t *testing.T) {
	cases := map[string]*ipnstate.Status{
		"nil status": nil,
		"no self":    {Self: nil},
		"no v4": {Self: &ipnstate.PeerStatus{
			DNSName:      "host.ts.net.",
			TailscaleIPs: []netip.Addr{netip.MustParseAddr("fd7a:115c:a1e0::1")},
		}},
		"no magicdns": {Self: &ipnstate.PeerStatus{
			DNSName:      "",
			TailscaleIPs: []netip.Addr{netip.MustParseAddr("100.106.40.12")},
		}},
	}
	for name, st := range cases {
		if _, _, err := selectAppEndpoint(st); err == nil {
			t.Errorf("%s: expected error, got nil", name)
		}
	}
}

func TestConnectURL(t *testing.T) {
	got := connectURL(tailnetInfo{Host: "box.tail1234.ts.net", Port: 9787})
	want := "agenton://connect?host=box.tail1234.ts.net&port=9787"
	if got != want {
		t.Fatalf("connectURL = %q, want %q", got, want)
	}
}

// No TLS anywhere: the QR must never carry a tls hint, and the URL must stay
// http:// even on 443 (someone fronting agenton by hand still gets plain HTTP).
func TestEndpointURL_AlwaysPlainHTTP(t *testing.T) {
	got := endpointURL(tailnetInfo{Host: "box.tail1234.ts.net", Port: 443})
	want := "http://box.tail1234.ts.net:443"
	if got != want {
		t.Fatalf("endpointURL = %q, want %q", got, want)
	}
	if strings.Contains(connectURL(tailnetInfo{Host: "h", Port: 443}), "tls") {
		t.Fatal("connectURL still emits a tls param")
	}
}

func TestTailnetInfoRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	in := tailnetInfo{Host: "box.tail1234.ts.net", Port: 9787}
	if err := writeTailnetInfo(in); err != nil {
		t.Fatal(err)
	}
	out, ok := readTailnetInfo()
	if !ok || out != in {
		t.Fatalf("round-trip = %+v ok=%v, want %+v", out, ok, in)
	}
	_ = os.Remove(tailnetStatePath())
}

func TestReadTailnetInfo_Missing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, ok := readTailnetInfo(); ok {
		t.Fatal("expected ok=false when no state file exists")
	}
}
