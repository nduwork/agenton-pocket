package tui

import (
	"bytes"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// ctrl+t (0x14) is the one reserved key: it returns to the session list and
// never reaches the PTY. Everything else passes through in order.
func TestSplitInputInterceptsCtrlT(t *testing.T) {
	segs, ctrlT := splitInput([]byte("ab\x14cd"))
	if !ctrlT {
		t.Fatal("ctrl+t not detected")
	}
	if len(segs) != 2 || string(segs[0]) != "ab" || string(segs[1]) != "cd" {
		t.Fatalf("segments = %q, want [ab cd]", segs)
	}
}

func TestSplitInputPassesThrough(t *testing.T) {
	segs, ctrlT := splitInput([]byte("hello"))
	if ctrlT {
		t.Fatal("ctrl+t detected in plain input")
	}
	if len(segs) != 1 || string(segs[0]) != "hello" {
		t.Fatalf("segments = %q, want [hello]", segs)
	}
}

func TestSplitInputLeadingAndTrailingCtrlT(t *testing.T) {
	segs, ctrlT := splitInput([]byte("\x14a\x14"))
	if !ctrlT {
		t.Fatal("ctrl+t not detected")
	}
	if len(segs) != 1 || string(segs[0]) != "a" {
		t.Fatalf("segments = %q, want [a]", segs)
	}
}

// SGR mouse events arrive in the agent's grid coordinates plus the hint bar's
// 1-row offset; the y coordinate must be decremented so clicks land on the
// right element.
func TestSGRMouseSeqTranslatesY(t *testing.T) {
	out, n, state := sgrMouseSeq([]byte("\x1b[<0;5;10M"))
	if state != sgrComplete {
		t.Fatalf("state = %d, want sgrComplete", state)
	}
	if n != len("\x1b[<0;5;10M") {
		t.Fatalf("consumed %d bytes, want %d", n, len("\x1b[<0;5;10M"))
	}
	if want := "\x1b[<0;5;9M"; string(out) != want {
		t.Fatalf("translated = %q, want %q", out, want)
	}
}

// Events on the hint row (y=1) are dropped — the agent's grid starts at host
// row 2.
func TestSGRMouseSeqDropsHintRow(t *testing.T) {
	out, n, state := sgrMouseSeq([]byte("\x1b[<0;5;1M"))
	if state != sgrComplete {
		t.Fatalf("state = %d, want sgrComplete", state)
	}
	if out != nil {
		t.Fatalf("hint-row event not dropped: %q", out)
	}
	if n != len("\x1b[<0;5;1M") {
		t.Fatalf("consumed %d bytes, want %d", n, len("\x1b[<0;5;1M"))
	}
}

// A sequence split across read chunks must wait for the rest, not be forwarded
// half-parsed.
func TestSGRMouseSeqIncomplete(t *testing.T) {
	_, _, state := sgrMouseSeq([]byte("\x1b[<0;5;"))
	if state != sgrIncomplete {
		t.Fatalf("state = %d, want sgrIncomplete", state)
	}
}

// A non-mouse sequence that happens to start with ESC[< is malformed, not
// incomplete — the caller forwards the ESC[< literally instead of waiting.
func TestSGRMouseSeqMalformed(t *testing.T) {
	_, _, state := sgrMouseSeq([]byte("\x1b[<foo"))
	if state != sgrMalformed {
		t.Fatalf("state = %d, want sgrMalformed", state)
	}
}

// The hint bar must fill the row (reverse video covers it edge to edge) and
// truncate when the terminal is narrower than the hint.
func TestHintBarTextPadsAndTruncates(t *testing.T) {
	wide := hintBarText(200)
	if ansi.StringWidth(wide) != 200 {
		t.Fatalf("hint padded to %d cells, want 200", ansi.StringWidth(wide))
	}
	if !bytes.HasSuffix([]byte(wide), bytes.Repeat([]byte(" "), 10)) {
		t.Fatal("hint not padded with trailing spaces")
	}
	narrow := hintBarText(10)
	if ansi.StringWidth(narrow) != 10 {
		t.Fatalf("hint truncated to %d cells, want 10", ansi.StringWidth(narrow))
	}
}

// The daemon PTY is raw, so a child's bare \n must become \r\n on the host
// terminal (raw mode, OPOST off) or it drops down a column.
func TestCRLFInsertsCRBeforeBareLF(t *testing.T) {
	if got, want := string(crlf([]byte("a\nb"))), "a\r\nb"; got != want {
		t.Fatalf("crlf = %q, want %q", got, want)
	}
	// Existing CRLF is untouched (no double CR).
	if got, want := string(crlf([]byte("a\r\nb"))), "a\r\nb"; got != want {
		t.Fatalf("crlf = %q, want %q", got, want)
	}
	// No bare LF: the input buffer is returned unchanged (no allocation).
	in := []byte("a\r\nb")
	if out := crlf(in); &out[0] != &in[0] {
		t.Fatal("crlf reallocated input with no bare LF")
	}
}
