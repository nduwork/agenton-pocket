package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nduwork/agenton-pocket/internal/client"
	"github.com/nduwork/agenton-pocket/internal/transport"
)

type mode int

const (
	modeEntry mode = iota
	modeSession
)

type model struct {
	mode          mode
	socket        string
	cwd           string         // new sessions start here (the TUI's launch dir)
	controlClient *client.Client // entry screen: list/new/kill (own conn)
	entry         entryModel
	session       *sessionModel
	width, height int // last window size, re-applied when screens are rebuilt
	err           string
}

// Run launches the Bubble Tea program against a daemon socket. cwd is the
// working directory new sessions inherit.
func Run(socketPath, cwd string) error {
	c, err := dialClient(socketPath)
	if err != nil {
		return fmt.Errorf("connect to daemon at %s: %w\n(is `agenton daemon` running?)", socketPath, err)
	}
	m := model{
		mode:          modeEntry,
		socket:        socketPath,
		cwd:           cwd,
		controlClient: c,
		entry:         newEntryModel(c, cwd),
	}
	// WithMouseCellMotion makes the host terminal report real mouse events
	// (wheel as button 64/65) instead of applying alternate-scroll mode, which
	// would convert trackpad/mouse wheel into Up/Down arrow keys that we'd
	// forward to the agent as input-history navigation. handleWheel turns the
	// vertical wheel back into a mouse event for the agent so it scrolls its
	// own conversation. Cell motion (not all-motion) is enough for the wheel and
	// avoids a stream of motion events.
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		return err
	}
	return nil
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
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case attachMsg:
		// open a dedicated connection for the session to avoid a read race with
		// the entry screen's list/new calls on the control connection.
		sc, err := dialClient(m.socket)
		if err != nil {
			m.err = err.Error()
			return m, nil
		}
		sm := newSessionModel(sc, msg.id, msg.name)
		m.session = sm
		m.mode = modeSession
		initCmd := sm.Init()
		// Bubble Tea only sends WindowSizeMsg at startup and on real resizes,
		// so a view created mid-run must be given the current size explicitly
		// or the emulator stays at its 80x24 default.
		if m.width > 0 && m.height > 0 {
			var sizeCmd tea.Cmd
			m.session, sizeCmd = sm.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
			return m, tea.Batch(initCmd, sizeCmd)
		}
		return m, initCmd
	case backToEntryMsg:
		if m.session != nil {
			_ = m.session.Close()
			m.session = nil
		}
		m.mode = modeEntry
		m.err = ""
		m.entry = newEntryModel(m.controlClient, m.cwd)
		m.entry.width = m.width
		m.entry.notice = "detached — session keeps running"
		return m, m.entry.Init()
	case streamEndedMsg:
		// The output stream closed. If we're still in session mode, the session
		// process exited (or the conn dropped) — return to the entry screen. If
		// we're already in entry mode, this is a late notification from an
		// explicit detach and we ignore it (don't quit, don't double-transition).
		if m.mode != modeSession {
			return m, nil
		}
		if m.session != nil {
			_ = m.session.Close()
			m.session = nil
		}
		m.mode = modeEntry
		m.entry = newEntryModel(m.controlClient, m.cwd)
		m.entry.width = m.width
		m.entry.notice = "session ended"
		return m, m.entry.Init()
	case errMsg:
		m.err = msg.message
		// Surface the error on the entry screen (its View renders errLine).
		// Session-mode errors are rare and logged via m.err only.
		if m.mode == modeEntry {
			m.entry.err = msg.message
		}
	}

	switch m.mode {
	case modeEntry:
		var cmd tea.Cmd
		m.entry, cmd = m.entry.Update(msg)
		return m, cmd
	case modeSession:
		if m.session == nil {
			return m, nil
		}
		var cmd tea.Cmd
		m.session, cmd = m.session.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) View() string {
	switch m.mode {
	case modeEntry:
		return m.entry.View()
	case modeSession:
		if m.session == nil {
			return ""
		}
		return m.session.View()
	}
	return ""
}
