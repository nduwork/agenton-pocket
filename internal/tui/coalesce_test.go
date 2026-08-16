package tui

import (
	"io"
	"testing"
	"time"
)

// waitForDamage must coalesce a redraw burst rather than firing a render on the
// first byte — that immediate-render behavior is what painted agents'
// half-drawn intermediate frames (the overlapping-text bug). We can't assert
// "no overlap" directly, but we can assert the render is deferred past the
// quiet window: a regression to immediate-return makes elapsed ~0 and fails.
func TestWaitForDamageCoalesces(t *testing.T) {
	pr, pw := io.Pipe()
	emu := newVTEmu(80, 24, pr, io.Discard)
	t.Cleanup(func() { _ = emu.Close(); _ = pw.Close() })

	// One write => damage. With no further writes, the render should fire after
	// coalesceQuiet, not instantly.
	if _, err := pw.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	msg := waitForDamage(emu)()
	elapsed := time.Since(start)

	if _, ok := msg.(renderTickMsg); !ok {
		t.Fatalf("expected renderTickMsg, got %T", msg)
	}
	if elapsed < coalesceQuiet {
		t.Fatalf("rendered after %v, before the %v coalesce window — not coalescing", elapsed, coalesceQuiet)
	}
	if elapsed > coalesceMax+200*time.Millisecond {
		t.Fatalf("rendered after %v — coalesce cap %v not honored", elapsed, coalesceMax)
	}
}
