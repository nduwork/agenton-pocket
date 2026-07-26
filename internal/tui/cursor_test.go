package tui

import "testing"

// reverseCellAt must wrap exactly the cell at the target column in reverse video
// (\e[7m … \e[27m), count printable columns past any CSI escapes without
// splitting them, and pad when the cursor sits beyond the row's content.
func TestReverseCellAt(t *testing.T) {
	// plain row, cursor mid-line: only the 'c' at column 2 gets wrapped.
	if got, want := reverseCellAt("abcd", 2), "ab\x1b[7mc\x1b[27md"; got != want {
		t.Errorf("plain: got %q want %q", got, want)
	}
	// an SGR sequence before the cursor must not count as a column nor be split.
	if got, want := reverseCellAt("\x1b[31mXY", 1), "\x1b[31mX\x1b[7mY\x1b[27m"; got != want {
		t.Errorf("styled: got %q want %q", got, want)
	}
	// cursor past the last printable cell: pad with spaces, then a reversed space.
	if got, want := reverseCellAt("ab", 3), "ab \x1b[7m \x1b[27m"; got != want {
		t.Errorf("past-end: got %q want %q", got, want)
	}
}
