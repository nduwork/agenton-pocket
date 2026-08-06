// Package web serves the embedded browser client and bridges WebSocket
// connections to the daemon's Unix socket. One WS binary message carries
// exactly one wire frame, so the browser speaks the same protocol as the TUI.
package web

import (
	"bytes"
	"embed"
	"io/fs"
	"net/http"

	"github.com/coder/websocket"

	"github.com/nduwork/agenton-pocket/internal/protocol"
	"github.com/nduwork/agenton-pocket/internal/transport"
)

//go:embed static
var staticFS embed.FS

// Handler serves the embedded frontend at / and the frame bridge at /ws.
// Each /ws connection gets its own fresh connection to the daemon socket,
// mirroring how the TUI and stdio bridge use one conn per client.
func Handler(socketPath string) http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(staticFS, "static")
	// no-store: embedded files all share a zero modtime, so a browser's
	// If-Modified-Since revalidation would 304 and serve stale JS/CSS forever
	// after the daemon is updated. Never caching keeps clients on the current
	// assets (payloads are tiny; the WS stream is what carries the traffic).
	mux.Handle("/", noStore(http.FileServer(http.FS(static))))
	// Identity probe so the `agenton vpn`/`lan` starter can tell this server apart from whatever
	// else may sit on the port (Dask dashboards also default to 8787).
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("agenton"))
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWS(w, r, socketPath)
	})
	return mux
}

func noStore(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		h.ServeHTTP(w, r)
	})
}

func serveWS(w http.ResponseWriter, r *http.Request, socketPath string) {
	ws, err := websocket.Accept(w, r, nil) // default same-origin check
	if err != nil {
		return
	}
	defer ws.CloseNow()
	ws.SetReadLimit(1 << 20)

	sock, err := transport.DialSocket(socketPath)
	if err != nil {
		ws.Close(websocket.StatusInternalError, "daemon unreachable")
		return
	}
	defer sock.Close()
	ctx := r.Context()

	// ws -> socket: each client message is one frame; the socket is a byte
	// stream, so writing the raw bytes preserves framing.
	go func() {
		defer sock.Close() // unblocks the ReadFrame loop below
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			if _, err := sock.Write(data); err != nil {
				return
			}
		}
	}()

	// socket -> ws: re-frame the stream so every WS message is one frame.
	for {
		f, err := protocol.ReadFrame(sock)
		if err != nil {
			return
		}
		var buf bytes.Buffer
		if err := protocol.WriteFrame(&buf, f); err != nil {
			return
		}
		if err := ws.Write(ctx, websocket.MessageBinary, buf.Bytes()); err != nil {
			return
		}
	}
}
