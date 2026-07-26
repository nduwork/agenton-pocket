package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nduwork/agenton-pocket/internal/client"
	"github.com/nduwork/agenton-pocket/internal/protocol"
)

// messages
type attachMsg struct {
	id   uint32
	name string
}
type backToEntryMsg struct{}
type errMsg struct{ message string }

type entryModel struct {
	c        *client.Client
	cwd      string // new sessions start here (the TUI's launch dir)
	sessions []protocol.SessionInfo
	cursor   int
	width    int
	err      string
	notice   string // non-error status line ("session ended", "detached", …)

	// pending kill confirmation (`d` pressed once); 0 = none
	confirmKill uint32
}

func newEntryModel(c *client.Client, cwd string) entryModel {
	return entryModel{c: c, cwd: cwd}
}

type loadedSessionsMsg struct{ sessions []protocol.SessionInfo }
type reloadMsg struct{}
type pollTickMsg struct{}

// pollInterval is how often the entry screen re-lists sessions so work done
// elsewhere (the phone/web client starting or killing sessions) shows up
// without a manual refresh.
const pollInterval = 2 * time.Second

func pollTick() tea.Cmd {
	return tea.Tick(pollInterval, func(time.Time) tea.Msg { return pollTickMsg{} })
}

func (e entryModel) Init() tea.Cmd { return tea.Batch(loadSessions(e.c), pollTick()) }

func loadSessions(c *client.Client) tea.Cmd {
	return func() tea.Msg {
		s, err := c.ListSessions()
		if err != nil {
			return errMsg{err.Error()}
		}
		return loadedSessionsMsg{s}
	}
}

// createShellSessionCmd starts a session running the user's default shell in
// the TUI's working directory and attaches to it. The TUI's "new session" is
// just a terminal — you launch claude/codex from inside it, wherever you cd to.
func createShellSessionCmd(c *client.Client, cwd string) tea.Cmd {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	label := filepath.Base(shell)
	return func() tea.Msg {
		// -l: a login shell loads the user's PATH/rc so claude/codex resolve.
		id, err := c.NewSessionCmd(shell, label, cwd, []string{"-l"})
		if err != nil {
			return errMsg{err.Error()}
		}
		return attachMsg{id: id, name: label}
	}
}

func (e entryModel) Update(msg tea.Msg) (entryModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		e.width = msg.Width
		return e, nil
	case loadedSessionsMsg:
		e.sessions = msg.sessions
		if e.cursor >= len(e.sessions) {
			e.cursor = len(e.sessions) - 1
		}
		if e.cursor < 0 {
			e.cursor = 0
		}
		return e, nil
	case pollTickMsg:
		// re-arm every interval (not gated on the reply) so a transient list
		// error can't stop the poll; roundtrips are serialized, so an
		// overlapping user action can't corrupt the shared connection.
		return e, tea.Batch(loadSessions(e.c), pollTick())
	case reloadMsg:
		return e, loadSessions(e.c)
	case errMsg:
		e.err = msg.message
		return e, nil
	case tea.KeyMsg:
		// A pending kill confirmation captures the next key: `d` confirms,
		// anything else cancels.
		if e.confirmKill != 0 {
			id := e.confirmKill
			e.confirmKill = 0
			if msg.String() == "d" {
				_ = e.c.Kill(id)
				e.notice = "session killed"
				return e, loadSessions(e.c)
			}
			return e, nil
		}
		// Navigation (up/down) just moves the cursor; it shouldn't dismiss
		// the status notice ("detached — session keeps running", etc.). Any
		// other key is an action, so clear the stale notice.
		if k := msg.String(); k != "up" && k != "down" && k != "k" && k != "j" {
			e.notice = ""
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return e, tea.Quit
		case "up", "k":
			if e.cursor > 0 {
				e.cursor--
			}
		case "down", "j":
			if e.cursor < len(e.sessions)-1 {
				e.cursor++
			}
		case "r":
			return e, loadSessions(e.c)
		case "n":
			// New session = a plain shell in the TUI's cwd; run agents inside it.
			e.err = ""
			return e, createShellSessionCmd(e.c, e.cwd)
		case "enter":
			if len(e.sessions) > 0 {
				s := e.sessions[e.cursor]
				return e, func() tea.Msg { return attachMsg{id: s.ID, name: s.Name} }
			}
		case "d":
			if len(e.sessions) > 0 {
				e.confirmKill = e.sessions[e.cursor].ID
			}
		}
	}
	return e, nil
}

// ruleWidth caps the rule at the window (fallback 60 before the first resize).
func (e entryModel) ruleWidth() int {
	if e.width <= 0 {
		return 60
	}
	return min(e.width, 72)
}

func (e entryModel) View() string {
	title := titleStyle.Render("agenton")
	var b strings.Builder
	b.WriteString(title + "\n" + rule(e.ruleWidth()) + "\n")
	if len(e.sessions) == 0 {
		b.WriteString("\n  " + dimStyle.Render("no sessions") + "\n\n  " +
			hint("n", "new session") + "   " + hint("q", "quit"))
		return b.String() + e.statusLines()
	}

	// Column widths from the data. The row shows a short repo label (the agent's
	// live working directory) and the terminal (agent) name — not full paths.
	repoW, termW, statusW := 4, 5, 7
	for _, s := range e.sessions {
		repoW = max(repoW, len([]rune(repoLabel(s))))
		termW = max(termW, len([]rune(terminalName(s.Agent))))
		statusW = max(statusW, len(s.Status))
	}
	idW := 2
	for _, s := range e.sessions {
		idW = max(idW, len(strconv.FormatUint(uint64(s.ID), 10))+1)
	}
	// Clamp repo/term only when the natural row would overflow the window — a
	// wide terminal shows them in full instead of truncating with an ellipsis
	// against empty space. Fixed per-row overhead: marker(2) + id + the three
	// gap runs (1+2+2) + status.
	if e.width > 0 {
		budget := e.width - (2 + idW + 1 + 2 + 2 + statusW)
		if repoW+termW > budget {
			termW = max(5, min(termW, budget/3)) // term keeps up to a third…
			repoW = max(4, budget-termW)         // …repo takes the rest
		}
	} else {
		repoW, termW = min(repoW, 24), min(termW, 12) // pre-first-resize fallback
	}

	for i, s := range e.sessions {
		id := dimStyle.Render(fmt.Sprintf("%*s", idW, "#"+strconv.FormatUint(uint64(s.ID), 10)))
		repo := fmt.Sprintf("%-*s", repoW, trunc(repoLabel(s), repoW))
		term := dimStyle.Render(fmt.Sprintf("%-*s", termW, trunc(terminalName(s.Agent), termW)))
		status := fmt.Sprintf("%-*s", statusW, s.Status)
		if s.Status == "running" {
			status = runningStyle.Render(status)
		} else {
			status = dimStyle.Render(status)
		}
		marker := "  "
		if i == e.cursor {
			marker = selStyle.Render("▶ ")
			repo = selStyle.Render(repo)
		}
		b.WriteString(marker + id + " " + repo + "  " + term + "  " + status + "\n")
	}

	b.WriteString("\n  ")
	if e.confirmKill != 0 {
		name := ""
		for _, s := range e.sessions {
			if s.ID == e.confirmKill {
				name = s.Name
			}
		}
		b.WriteString(errStyle.Render("kill "+name+"?") + "   " +
			hint("d", "confirm") + "   " + dimStyle.Render("any other key cancels"))
	} else {
		b.WriteString(hint("enter", "attach") + "   " + hint("n", "new") + "   " +
			hint("d", "kill") + "   " + hint("r", "refresh") + "   " + hint("q", "quit"))
	}
	return b.String() + e.statusLines()
}

// statusLines renders the notice and/or error footer (may be empty).
func (e entryModel) statusLines() string {
	out := ""
	if e.notice != "" {
		out += "\n  " + noticeStyle.Render(e.notice)
	}
	if e.err != "" {
		out += "\n  " + errStyle.Render("error: "+e.err)
	}
	return out
}
