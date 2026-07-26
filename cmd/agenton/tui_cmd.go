package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/nduwork/agenton-pocket/internal/tui"
)

func runTUI(args []string) {
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	var socket string
	fs.StringVar(&socket, "socket", defaultSocketPath(), "daemon socket path")
	_ = fs.Parse(args)
	// New sessions start in the TUI's working directory, so `cd repo && agenton
	// tui` (or bare `agenton` from a repo) launches agents in that repo.
	cwd, _ := os.Getwd()
	if err := tui.Run(socket, cwd); err != nil {
		fmt.Fprintln(os.Stderr, "agenton tui:", err)
		os.Exit(1)
	}
}
