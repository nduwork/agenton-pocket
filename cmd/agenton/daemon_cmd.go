package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/nduwork/agenton-pocket/internal/daemon"
	"github.com/nduwork/agenton-pocket/internal/transport"
)

func defaultSocketPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".agenton", "agenton.sock")
}

func runDaemon(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	var cfgPath string
	fs.StringVar(&cfgPath, "config", "", "config path (default ~/.config/agenton/config.toml)")
	_ = fs.Parse(args)

	path := cfgPath
	if path == "" {
		path = daemon.DefaultConfigPath()
	}
	cfg, err := daemon.LoadConfig(path)
	if err != nil {
		log.Printf("no config at %s: %v (starting with no presets)", path, err)
	}

	sock := defaultSocketPath()
	if err := os.MkdirAll(filepath.Dir(sock), 0o700); err != nil {
		log.Fatal(err)
	}
	d := daemon.New(cfg, sock)
	ln, err := transport.Listen(sock)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("agenton daemon listening on %s", sock)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sig
		d.Shutdown()
		ln.Close()
		os.Exit(0)
	}()

	if err := d.Serve(ln); err != nil {
		log.Fatal(err)
	}
}
