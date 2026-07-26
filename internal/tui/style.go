package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/nduwork/agenton-pocket/internal/protocol"
)

// Codex Micro palette: monochrome + a single accent. ANSI-indexed colors so
// the UI adapts to the user's terminal theme (readable on light and dark).
var (
	accent   = lipgloss.Color("6") // the one accent color
	dimColor = lipgloss.Color("8")

	titleStyle   = lipgloss.NewStyle().Bold(true)
	selStyle     = lipgloss.NewStyle().Foreground(accent).Bold(true)
	dimStyle     = lipgloss.NewStyle().Foreground(dimColor)
	noticeStyle  = lipgloss.NewStyle().Foreground(dimColor).Italic(true)
	errStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	runningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	keyStyle     = lipgloss.NewStyle().Foreground(accent)
)

// hint renders a "[key] label" pair: accent key, dim label.
func hint(key, label string) string {
	return keyStyle.Render("["+key+"]") + " " + dimStyle.Render(label)
}

// rule renders a thin horizontal rule w cells wide.
func rule(w int) string {
	if w <= 0 {
		w = 60
	}
	out := make([]rune, w)
	for i := range out {
		out[i] = '─'
	}
	return dimStyle.Render(string(out))
}

// trunc cuts s to at most w runes, ending with an ellipsis when cut.
// ponytail: rune count, not display cells — fine for ASCII paths/names.
func trunc(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}

// repoLabel is the short working-directory label for a session list row: the
// daemon-provided repo/folder name, falling back to the cwd's folder name.
func repoLabel(s protocol.SessionInfo) string {
	if s.Repo != "" {
		return s.Repo
	}
	if s.Cwd != "" {
		if i := strings.LastIndexByte(s.Cwd, '/'); i >= 0 {
			return s.Cwd[i+1:]
		}
		return s.Cwd
	}
	return "—"
}

// terminalName turns an agent path/command into a friendly terminal label
// ("claude" or "/usr/local/bin/claude" → "Claude"). It is display-only; the
// full Agent value is kept on the protocol for "start another like this".
func terminalName(agent string) string {
	if agent == "" {
		return "—"
	}
	if i := strings.LastIndexByte(agent, '/'); i >= 0 {
		agent = agent[i+1:]
	}
	r := []rune(agent)
	if r[0] >= 'a' && r[0] <= 'z' {
		r[0] -= 'a' - 'A'
	}
	return string(r)
}
