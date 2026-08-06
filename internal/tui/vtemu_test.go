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
	waitUntil(t, func() bool { return strings.Contains(e.GetScreen().Rows[0], "plain") })
	if e.MouseTrackingOn() {
		t.Fatal("MouseTrackingOn true with no tracking sequence")
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
