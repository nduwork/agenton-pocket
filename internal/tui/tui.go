package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nduwork/agenton-pocket/internal/client"
	"github.com/nduwork/agenton-pocket/internal/transport"
)

// model is the Bubble Tea entry screen. The session view is a separate raw-mode
// program (bubbletea parses input and owns the renderer, so it can't do raw
// passthrough); Run loops entry → raw session → entry, carrying the selected
// session out of p.Run() via attachID/attachName.
type model struct {
	socket string
	cwd    string // new sessions start here (the TUI's launch dir)
	entry  entryModel
	err    string

	// result carried out of p.Run(): the session to attach, or zero if the user
	// quit.
	attachID   uint32
	attachName string
}

// Run launches the TUI against a daemon socket: a Bubble Tea entry screen that
// lists sessions, then a raw-mode session view that passes the daemon's PTY
// bytes straight through to the host terminal (so the session feels exactly like
// a native terminal). ctrl+t in a session returns to the entry screen; the
// session keeps running. cwd is the working directory new sessions inherit.
func Run(socketPath, cwd string) error {
	c, err := dialClient(socketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon at %s: %w\n(is `agenton daemon` running?)", socketPath, err)
	}
	defer c.Close()

	notice := ""
	for {
		m := model{
			socket: socketPath,
			cwd:    cwd,
			entry:  newEntryModel(c, cwd),
		}
		m.entry.notice = notice
		// WithMouseCellMotion makes the host terminal report real mouse events
		// (wheel as button 64/65) instead of applying alternate-scroll mode, which
		// would convert trackpad/mouse wheel into Up/Down arrow keys. The entry
		// screen doesn't use the wheel, but the option is harmless and keeps the
		// program's terminal setup consistent across the loop.
		p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
		final, err := p.Run()
		if err != nil {
			return err
		}
		fm := final.(model)
		if fm.attachID == 0 {
			return nil // user quit
		}
		notice, err = runSession(socketPath, fm.attachID, fm.attachName)
		if err != nil {
			return err
		}
	}
}

func dialClient(socketPath string) (*client.Client, error) {
	conn, err := transport.DialSocket(socketPath)
	if err != nil {
		return nil, err
	}
	return client.New(conn), nil
}

func (m model) Init() tea.Cmd { return m.entry.Init() }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case attachMsg:
		// The entry screen picked a session: carry it out of p.Run() and quit so
		// the outer loop can start the raw session view.
		m.attachID = msg.id
		m.attachName = msg.name
		return m, tea.Quit
	case errMsg:
		m.err = msg.message
		m.entry.err = msg.message
	}
	var cmd tea.Cmd
	m.entry, cmd = m.entry.Update(msg)
	return m, cmd
}

func (m model) View() string { return m.entry.View() }
