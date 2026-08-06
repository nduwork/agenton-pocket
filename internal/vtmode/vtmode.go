// Package vtmode scans a terminal byte stream for DEC private-mode set/reset
// sequences. Shared by the daemon (to replay modes on attach) and the desktop
// TUI (to know when the agent owns the mouse).
package vtmode

import (
	"strconv"
	"strings"
)

// ScrollbackLines is the emulator history depth, shared so the desktop TUI's
// pager can page back through exactly what the daemon-side emulator retains.
const ScrollbackLines = 10000

// UpdatePrivateModes scans b for DEC private-mode set/reset sequences
// (ESC [ ? params h/l) and applies them to modes: set on 'h', delete on 'l'. A
// sequence split across chunks won't match; that's rare (escapes are tiny vs.
// chunk size) and self-corrects on the next mode change. claude sets its modes
// once at startup in a single chunk, so they're captured reliably.
func UpdatePrivateModes(b []byte, modes map[int]bool) {
	for i := 0; i < len(b); i++ {
		if b[i] != 0x1b || i+1 >= len(b) || b[i+1] != '[' {
			continue
		}
		j := i + 2
		if j >= len(b) || b[j] != '?' {
			continue
		}
		j++
		start := j
		for j < len(b) && (b[j] >= '0' && b[j] <= '9' || b[j] == ';') {
			j++
		}
		if j >= len(b) || (b[j] != 'h' && b[j] != 'l') {
			continue
		}
		set := b[j] == 'h'
		for _, field := range strings.Split(string(b[start:j]), ";") {
			if field == "" {
				continue
			}
			if n, err := strconv.Atoi(field); err == nil {
				if set {
					modes[n] = true
				} else {
					delete(modes, n)
				}
			}
		}
		i = j // skip past the sequence
	}
}
