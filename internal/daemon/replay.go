package daemon

import (
	"fmt"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

const sgrReset = "\x1b[m"

// serializeReplay renders a session's terminal state as ANSI bytes a freshly
// attached client feeds straight into its own terminal: a DEC private-mode
// restore lead, every scrollback line (each ending CRLF so it flows into the
// client's scrollback), the current screen, and the cursor position. This is
// what lets a late attach scroll back through the whole conversation — agents
// like claude repaint in place, so replaying raw output chunks could only ever
// reconstruct the final screen.
//
// The cursor is repositioned with relative moves from the end of the last
// screen row (CR, cursor-up, cursor-forward) so the result is correct even
// when the client's terminal is taller than the session's PTY.
func serializeReplay(emu *vt.Emulator, modes map[int]bool) [][]byte {
	var snap [][]byte
	if lead := modeRestoreBytes(modes); len(lead) > 0 {
		snap = append(snap, lead)
	}
	if emu == nil {
		return snap
	}

	var sb strings.Builder
	// History. The alt screen has no scrollback of its own, and main-screen
	// history is invisible behind it — replay it anyway? No: the mode lead
	// already switched the client to the alt screen, where injected lines
	// would corrupt the frame. (Neither claude nor codex uses the alt screen;
	// this is defensive for agents that do.)
	if !emu.IsAltScreen() {
		for _, line := range emu.Scrollback().Lines() {
			writeCells(&sb, line)
			sb.WriteString("\r\n")
		}
	}

	// Current screen, top row first. Rows are joined (not terminated) with
	// CRLF so the final row doesn't push a blank line into scrollback.
	w, h := emu.Width(), emu.Height()
	for y := 0; y < h; y++ {
		if y > 0 {
			sb.WriteString("\r\n")
		}
		row := make(uv.Line, 0, w)
		for x := 0; x < w; x++ {
			if c := emu.CellAt(x, y); c != nil {
				row = append(row, *c)
			}
		}
		// Trim trailing unstyled blanks — the client's fresh rows are already
		// blank, and this keeps a mostly-empty screen from bloating the replay.
		for len(row) > 0 {
			last := &row[len(row)-1]
			if (last.Content == "" || last.Content == " ") && last.Style.IsZero() && last.Link.IsZero() {
				row = row[:len(row)-1]
				continue
			}
			break
		}
		writeCells(&sb, row)
	}

	// Park the cursor where the agent left it, relative to the bottom row.
	cur := emu.CursorPosition()
	sb.WriteString("\r")
	if up := h - 1 - cur.Y; up > 0 {
		fmt.Fprintf(&sb, "\x1b[%dA", up)
	}
	if cur.X > 0 {
		fmt.Fprintf(&sb, "\x1b[%dC", cur.X)
	}

	if sb.Len() > 0 {
		snap = append(snap, []byte(sb.String()))
	}
	return snap
}

// writeCells appends one row of cells as styled text: SGR transitions between
// cells, grapheme content verbatim, and a trailing reset so the row's style
// never leaks into the next line. Zero-width cells (the shadow of a preceding
// wide grapheme) are skipped; empty cells print as spaces to preserve column
// alignment.
func writeCells(sb *strings.Builder, line uv.Line) {
	cur := uv.Style{}
	styled := false
	for i := range line {
		c := &line[i]
		if c.Width == 0 && c.Content == "" {
			continue
		}
		if !c.Style.Equal(&cur) {
			sb.WriteString(c.Style.Diff(&cur))
			cur = c.Style
			styled = !cur.IsZero()
		}
		if c.Content == "" {
			sb.WriteByte(' ')
		} else {
			sb.WriteString(c.Content)
		}
	}
	if styled {
		sb.WriteString(sgrReset)
	}
}
