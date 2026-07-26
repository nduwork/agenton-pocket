package main

import (
	"fmt"
	"os"

	"github.com/mdp/qrterminal/v3"
)

// runQR prints a scannable QR for connecting a phone.
//
//	agenton qr                 iOS app deep link (agenton://…) from the running server
//	agenton qr --web           web-client URL (http://…) — scan with the phone
//	                           camera to open it in a browser, no app needed
//	agenton qr <payload-url>   encode an explicit URL instead
//
// The endpoint is published by the web process (see tailnet.go), which records
// it in tailnet.json. If it isn't there yet, the server either isn't running or
// is serving localhost-only (no Tailscale app).
func runQR(args []string) {
	webLink := hasFlag(args, "-web") || hasFlag(args, "--web") ||
		hasFlag(args, "-url") || hasFlag(args, "--url")

	// An explicit payload (a non-flag positional arg) is encoded as-is.
	for _, a := range args {
		if a != "" && a[0] != '-' {
			printConnectQR(a)
			return
		}
	}

	info, ok := readTailnetInfo()
	if !ok {
		fmt.Fprintln(os.Stderr, "agenton: no tailnet endpoint yet.")
		fmt.Fprintln(os.Stderr, "         start the server first (`agenton up` or `agenton up -no-tui`)")
		fmt.Fprintln(os.Stderr, "         with the Tailscale app running, then re-run `agenton qr`.")
		fmt.Fprintln(os.Stderr, "         or pass an explicit URL: agenton qr 'agenton://connect?host=…&port=9787'")
		os.Exit(1)
	}
	if webLink {
		printWebQR(endpointURL(info))
		return
	}
	printConnectQR(connectURL(info))
}

// printConnectQR renders the agenton:// deep link for the iOS app.
func printConnectQR(payload string) {
	renderQR("Scan with the agenton iOS app (⚙︎ → Scan QR):", payload)
}

// printWebQR renders the web-client URL for a browser — scannable by the phone
// camera (opens the browser directly), or paste into any browser on your tailnet.
func printWebQR(url string) {
	renderQR("Open the agenton web client — scan with your phone camera, or paste in a browser:", url)
}

// renderQR prints a terminal QR of payload under header, with the raw text below.
func renderQR(header, payload string) {
	fmt.Println("\n" + header)
	qrterminal.GenerateHalfBlock(payload, qrterminal.L, os.Stdout)
	fmt.Println(payload)
}
