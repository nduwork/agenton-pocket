package tui

import (
	"io"
	"strings"
	"testing"
	"time"
)

// waitUntil polls f up to 500ms so we don't race the async read loop.
func waitUntil(t *testing.T, f func() bool) bool {
	t.Helper()
	for i := 0; i < 50; i++ {
		if f() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func TestVTEmuRendersTextAndTracksMouseMode(t *testing.T) {
	// Text, then enable mouse tracking (?1002h).
	e := newVTEmu(20, 3, strings.NewReader("hello\x1b[?1002h"), io.Discard)
	defer e.Close()

	if !waitUntil(t, func() bool { return strings.Contains(e.GetScreen().Rows[0], "hello") }) {
		t.Fatalf("screen never showed text: %q", e.GetScreen().Rows)
	}
	if !waitUntil(t, e.MouseTrackingOn) {
		t.Fatal("MouseTrackingOn stayed false after ?1002h")
	}
}

func TestVTEmuMouseModeOffByDefault(t *testing.T) {
	e := newVTEmu(20, 3, strings.NewReader("plain text"), io.Discard)
	defer e.Close()
	if !waitUntil(t, func() bool { return strings.Contains(e.GetScreen().Rows[0], "plain") }) {
		t.Fatal("emulator never rendered the input")
	}
	if e.MouseTrackingOn() {
		t.Fatal("MouseTrackingOn true with no tracking sequence")
	}
}

// chunkReader hands out one chunk per Read, so escape sequences can be forced
// to straddle read boundaries.
type chunkReader struct{ chunks []string }

func (r *chunkReader) Read(p []byte) (int, error) {
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.chunks[0])
	r.chunks = r.chunks[1:]
	return n, nil
}

// Mode tracking rides the vt parser, so a mode sequence split across read
// chunks must still register — the old byte-rescan missed those, misrouting
// the wheel for the rest of the session.
func TestVTEmuTracksModeSplitAcrossChunks(t *testing.T) {
	e := newVTEmu(20, 3, &chunkReader{chunks: []string{"\x1b[?10", "02h"}}, io.Discard)
	defer e.Close()
	if !waitUntil(t, e.MouseTrackingOn) {
		t.Fatal("split ?1002h did not enable mouse tracking")
	}

	e2 := newVTEmu(20, 3, &chunkReader{chunks: []string{"\x1b[?1002h", "\x1b[?100", "2l"}}, io.Discard)
	defer e2.Close()
	if !waitUntil(t, func() bool { return !e2.MouseTrackingOn() }) {
		t.Fatal("split ?1002l did not disable mouse tracking")
	}
}

func TestVTEmuResizeReshapesScreen(t *testing.T) {
	e := newVTEmu(20, 3, strings.NewReader("hello"), io.Discard)
	defer e.Close()
	if !waitUntil(t, func() bool { return strings.Contains(e.GetScreen().Rows[0], "hello") }) {
		t.Fatal("emulator never rendered the input")
	}
	if err := e.Resize(40, 5); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	rows := e.GetScreen().Rows
	if len(rows) != 5 {
		t.Fatalf("after Resize: %d rows, want 5", len(rows))
	}
}

func TestVTEmuScrollbackGrows(t *testing.T) {
	// 3-row screen; write 10 newline-terminated lines so ~7 fall into scrollback.
	var sb strings.Builder
	for i := 0; i < 10; i++ {
		sb.WriteString("line\r\n")
	}
	e := newVTEmu(20, 3, strings.NewReader(sb.String()), io.Discard)
	defer e.Close()
	if !waitUntil(t, func() bool { return e.ScrollbackLen() > 0 }) {
		t.Fatalf("scrollback stayed empty, len=%d", e.ScrollbackLen())
	}
}
