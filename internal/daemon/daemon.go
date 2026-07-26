package daemon

import (
	"log"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nduwork/agenton-pocket/internal/protocol"
)

type Daemon struct {
	cfg      Config
	mu       sync.Mutex
	sessions map[uint32]*Session
	nextID   uint32
	logf     *log.Logger
}

func New(cfg Config, socketPath string) *Daemon {
	return &Daemon{
		cfg:      cfg,
		sessions: map[uint32]*Session{},
		nextID:   1,
		logf:     log.New(os.Stderr, "agenton-daemon: ", log.LstdFlags),
	}
}

func (d *Daemon) Serve(ln net.Listener) error {
	go d.watchAgents() // push live session-title changes to attached clients
	for {
		c, err := ln.Accept()
		if err != nil {
			return err
		}
		go d.handleConn(c)
	}
}

// watchAgents keeps each attached session's title in sync with the agent
// actually running inside it. A session started as a plain shell reports "zsh"
// until you launch claude in it; the daemon already resolves keys live per tap
// (currentKeys), but the displayed title is pushed, not polled — so this scans
// the process table on an interval and broadcasts a session_state title update
// whenever a session's live agent changes (shell → claude → shell). One `ps`
// per tick, and only when at least one session has a client watching.
func (d *Daemon) watchAgents() {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for range t.C {
		d.mu.Lock()
		sessions := make([]*Session, 0, len(d.sessions))
		for _, s := range d.sessions {
			sessions = append(sessions, s)
		}
		d.mu.Unlock()

		watched := false
		for _, s := range sessions {
			if s.hasSubs() {
				watched = true
				break
			}
		}
		if !watched {
			continue
		}

		tree := scanProcs()
		for _, s := range sessions {
			pid := s.pid()
			if pid == 0 || !s.hasSubs() {
				continue
			}
			agent := tree.agentUnder(pid)
			if agent == "" {
				agent = s.Agent // no sub-agent running → the session's own launch agent
			}
			if s.updateLiveAgent(agent) {
				d.broadcastAgent(s, agent)
			}
		}
	}
}

// handleConn reads control frames and dispatches them. Output is streamed on a
// separate goroutine per attached session (see streamTo).
func (d *Daemon) handleConn(conn net.Conn) {
	defer conn.Close()
	cw := newConnWriter(conn)
	// On disconnect, drop any size ownership this conn held so the next client
	// to resize or interact takes over the shared PTY size.
	defer func() {
		d.mu.Lock()
		sessions := make([]*Session, 0, len(d.sessions))
		for _, s := range d.sessions {
			sessions = append(sessions, s)
		}
		d.mu.Unlock()
		for _, s := range sessions {
			s.reassignOwnerAfter(cw)
			d.broadcastActive(s)
		}
	}()
	// This conn's last requested PTY size per session. Size focus follows
	// activity: when this client types or taps an action, its size is
	// re-applied, so whichever device the user is actually driving wins.
	sizes := map[uint32][2]int{}
	for {
		f, err := protocol.ReadFrame(conn)
		if err != nil {
			return
		}
		if f.Type != protocol.TypeControl {
			continue
		}
		env, err := protocol.DecodeEnvelope(f.Payload)
		if err != nil {
			d.sendError(cw, "invalid message")
			continue
		}
		d.handle(cw, env, sizes)
	}
}

// refocusSize re-applies this conn's requested size for s if another client
// resized the shared PTY since — called on input/action, so the actively-used
// device's layout wins without any explicit ownership handoff.
func refocusSize(s *Session, sizes map[uint32][2]int) {
	sz, ok := sizes[s.ID]
	if !ok {
		return
	}
	if c, r := s.Size(); c != sz[0] || r != sz[1] {
		_ = s.Resize(sz[0], sz[1])
	}
}

func (d *Daemon) handle(cw *connWriter, env protocol.Envelope, sizes map[uint32][2]int) {
	switch env.Type {
	case protocol.MsgListSessions:
		tree := scanProcs() // one process-table snapshot for titles + discovery
		d.sendControl(cw, protocol.Envelope{
			Type:      protocol.MsgSessionList,
			Sessions:  d.listSessions(tree),
			Unmanaged: d.unmanagedSessions(tree),
		})

	case protocol.MsgNewSession:
		preset, ok := d.cfg.Presets[env.Preset]
		if !ok {
			d.sendError(cw, "unknown preset: "+env.Preset)
			return
		}
		applyButtonDefaults(&preset) // fill gaps with per-agent defaults
		id := d.allocID()
		s := NewSession(id, env.Preset, preset)
		s.onExit = func() { d.handleSessionExit(s) }
		d.mu.Lock()
		d.sessions[id] = s
		d.mu.Unlock()
		if err := s.Start(); err != nil {
			d.mu.Lock()
			delete(d.sessions, id)
			d.mu.Unlock()
			d.sendError(cw, "start failed: "+err.Error())
			return
		}
		d.sendControl(cw, protocol.Envelope{Type: protocol.MsgSessionState, SessionID: id, Status: s.status()})
		// Streaming is explicit: the client sends Attach to receive output.
		// (We do not auto-stream here, to avoid duplicate streams.)

	case protocol.MsgNewSessionCmd:
		// Build an ad-hoc preset from the caller-supplied command line. The
		// agent label defaults to the command name so the entry list shows
		// something meaningful even when the caller omits it.
		command, args := env.Command, env.Args
		if command == "" {
			// No command → a plain login shell, same as the TUI's "n": you land
			// in a shell and run claude/codex yourself. The session title then
			// follows whatever agent you launch inside it (see listSessions).
			command = loginShell()
			args = []string{"-l"}
		}
		agent := env.Agent
		if agent == "" {
			agent = filepath.Base(command)
		}
		preset := Preset{
			Agent:   agent,
			Cwd:     env.Cwd,
			Command: command,
			Args:    args,
			// ccstatusline probes the PTY width and truncates its status line to
			// fit; on a phone-width terminal that collapses "Model: …" down to
			// "Mo…". Pin a comfortable render width so it emits the full line and
			// the terminal wraps it instead of truncating. Harmless for non-
			// ccstatusline commands (unknown env var) and on wide terminals
			// (content is short, so a fixed 200 never adds truncation).
			// ponytail: assumes no flex-expanding separator widget; if the user's
			// ccstatusline config gains one, lower this to avoid pad-to-width wrap.
			Env: map[string]string{"CCSTATUSLINE_WIDTH": "200"},
		}
		applyButtonDefaults(&preset) // ad-hoc sessions have no [buttons] table
		id := d.allocID()
		s := NewSession(id, agent, preset)
		s.onExit = func() { d.handleSessionExit(s) }
		d.mu.Lock()
		d.sessions[id] = s
		d.mu.Unlock()
		if err := s.Start(); err != nil {
			d.mu.Lock()
			delete(d.sessions, id)
			d.mu.Unlock()
			d.sendError(cw, "start failed: "+err.Error())
			return
		}
		d.sendControl(cw, protocol.Envelope{Type: protocol.MsgSessionState, SessionID: id, Status: s.status()})

	case protocol.MsgAttach:
		s := d.session(env.SessionID)
		if s == nil {
			d.sendError(cw, "no such session")
			return
		}
		go d.streamTo(cw, s)
		// Restore the session's custom-button bindings on the newcomer's pad: they
		// live on the persistent Session (SetCustom mutates s.preset), so a client
		// that detached and re-attached — or a second client — must be told the
		// current values or its pad shows the default "Custom N" labels. Reuse
		// set_button in the daemon→client direction; an empty value resets to the
		// default. Sent directly to cw, like the ownership unicast below.
		c1, c2 := s.customBindings()
		d.sendControl(cw, protocol.Envelope{Type: protocol.MsgSetButton, SessionID: s.ID, Action: "custom_1", Text: c1})
		d.sendControl(cw, protocol.Envelope{Type: protocol.MsgSetButton, SessionID: s.ID, Action: "custom_2", Text: c2})
		// Tell the newcomer the current ownership right away: broadcastActive
		// only fires on ownership *changes*, so without this a client attaching
		// into an already-owned session keeps its default active=true and
		// renders the owner's wide frame clipped. A lone/first client (no owner
		// yet) stays active by default, so only unicast when someone else
		// already owns the size. Sent directly to cw (not via s.subs) to avoid
		// a race with streamTo's subscription registration.
		if o := s.ownerConn(); o != nil && o != cw {
			active := false
			d.sendControl(cw, protocol.Envelope{Type: protocol.MsgSessionState,
				SessionID: s.ID, Status: s.status(), Active: &active})
		}

	case protocol.MsgDetach:
		// streaming goroutine exits when conn closes; detach is implicit on disconnect
		d.sendControl(cw, protocol.Envelope{Type: protocol.MsgSessionState, SessionID: env.SessionID, Status: "detached"})

	case protocol.MsgKillSession:
		s := d.session(env.SessionID)
		if s != nil {
			s.Kill()
			d.mu.Lock()
			delete(d.sessions, s.ID)
			d.mu.Unlock()
		}
		// Fire-and-forget: no reply. A reply here would be orphaned because
		// Client.Kill is send-only, and the next ListSessions would consume it
		// as the list reply (making the remaining sessions vanish). The client
		// refreshes the list to confirm.

	case protocol.MsgAction:
		s := d.session(env.SessionID)
		if s == nil {
			d.sendError(cw, "no such session")
			return
		}
		// Input is pure remote-control: it drives the agent but does NOT claim
		// the shared PTY size. Ownership changes only via set_active (the phone's
		// mode toggle) or the TUI's explicit take-over on a parked keypress — so
		// tapping buttons while parked never yanks the size from the active device.
		if err := s.DoAction(env.Action); err != nil {
			d.sendError(cw, err.Error())
		}

	case protocol.MsgSetButton:
		s := d.session(env.SessionID)
		if s == nil {
			d.sendError(cw, "no such session")
			return
		}
		if err := s.SetCustom(env.Action, env.Text); err != nil {
			d.sendError(cw, err.Error())
		}

	case protocol.MsgTextInput:
		s := d.session(env.SessionID)
		if s == nil {
			d.sendError(cw, "no such session")
			return
		}
		// Input is pure remote-control — no size claim (see MsgAction).
		if err := s.WriteText(env.Text); err != nil {
			d.sendError(cw, err.Error())
		}

	case protocol.MsgResize:
		// Fire-and-forget: no reply. A reply would orphan on the client side
		// (Resize is send-only) and corrupt the next control recv. Errors are
		// rare and non-fatal, so we log rather than report them out-of-band.
		s := d.session(env.SessionID)
		if s == nil {
			return
		}
		sizes[env.SessionID] = [2]int{env.Cols, env.Rows} // remember for refocus
		// Only resize the shared PTY if we own the size (or claim it while
		// unowned). A second client attaching/resizing while someone else is
		// actively driving the session no longer shrinks it under them.
		wasOwner := s.ownerConn() == cw
		if !s.ownsSize(cw, true) {
			return
		}
		if err := s.Resize(env.Cols, env.Rows); err != nil {
			d.logf.Printf("resize session %d: %v", env.SessionID, err)
		}
		if !wasOwner {
			// This resize is what claimed ownership (unowned -> cw) — tell
			// every subscriber. Repeat resizes from the same owner (window
			// drags, SIGWINCH) don't change who's active, so skip the chatter.
			d.broadcastActive(s)
		}

	case protocol.MsgSetActive:
		s := d.session(env.SessionID)
		if s == nil || env.Active == nil {
			return
		}
		if *env.Active {
			s.takeSize(cw)
			refocusSize(s, sizes) // re-apply this client's remembered dims now
		} else {
			// Release ownership; hand the size to another attached client if one
			// exists. The new owner re-applies its own dimensions on its next
			// input/resize (iOS re-sends size on toggle, the TUI on keypress), so
			// there is nothing to resize here.
			s.reassignOwnerAfter(cw)
		}
		d.broadcastActive(s)

	case protocol.MsgListDir:
		abs, dirs := listDir(env.Path)
		d.sendControl(cw, protocol.Envelope{Type: protocol.MsgDirList, Path: abs, Dirs: dirs})

	default:
		d.sendError(cw, "unknown message type")
	}
}

// listDir returns a directory's absolute path and its subdirectory names for
// the new-session cwd picker. Empty path = home. Hidden dirs are skipped;
// symlinks that point at directories are included. This exposes no more than
// new_session_cmd already does (it runs commands in any cwd), so there's no new
// trust boundary — the daemon is already fully trusted by whoever reaches it.
func listDir(p string) (string, []string) {
	if p == "" {
		p, _ = os.UserHomeDir()
	}
	abs, err := filepath.Abs(expandHome(p))
	if err != nil {
		abs = p
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return abs, nil // unreadable dir -> empty listing, path still shown
	}
	var dirs []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if e.IsDir() {
			dirs = append(dirs, name)
		} else if e.Type()&os.ModeSymlink != 0 {
			if fi, err := os.Stat(filepath.Join(abs, name)); err == nil && fi.IsDir() {
				dirs = append(dirs, name)
			}
		}
	}
	sort.Strings(dirs)
	return abs, dirs
}

// broadcastActive tells every subscriber of s whether it currently owns the
// shared PTY size, so each client can switch between its live-terminal and
// parked UIs. Safe to call from any goroutine — writes go through connWriter,
// and the subscriber list is snapshotted under s.mu before writing, so this
// never holds s.mu while blocked on a client's socket.
func (d *Daemon) broadcastActive(s *Session) {
	owner := s.ownerConn()
	s.mu.Lock()
	writers := make([]*connWriter, 0, len(s.subs))
	for _, cw := range s.subs {
		writers = append(writers, cw)
	}
	s.mu.Unlock()
	for _, cw := range writers {
		active := cw == owner
		b, _ := protocol.EncodeEnvelope(protocol.Envelope{
			Type: protocol.MsgSessionState, SessionID: s.ID, Status: s.status(), Active: &active,
		})
		_ = cw.write(protocol.Frame{Type: protocol.TypeControl, Payload: b})
	}
}

// broadcastAgent pushes the session's current agent name to every subscriber so
// their header retitles live (see watchAgents). Same snapshot-then-write
// discipline as broadcastActive — s.mu is never held across a client write.
func (d *Daemon) broadcastAgent(s *Session, agent string) {
	s.mu.Lock()
	writers := make([]*connWriter, 0, len(s.subs))
	for _, cw := range s.subs {
		writers = append(writers, cw)
	}
	s.mu.Unlock()
	for _, cw := range writers {
		b, _ := protocol.EncodeEnvelope(protocol.Envelope{
			Type: protocol.MsgSessionState, SessionID: s.ID, Status: s.status(), Agent: agent,
		})
		_ = cw.write(protocol.Frame{Type: protocol.TypeControl, Payload: b})
	}
}

// handleSessionExit runs once when a session's process exits (or it is killed).
// It tells every attached client the session ended (so they leave the session
// view), closes their subscription channels so the streamTo goroutines return,
// and drops the session from the list. Idempotent via the caller's sync.Once.
func (d *Daemon) handleSessionExit(s *Session) {
	status := s.status()
	s.mu.Lock()
	s.ended = true
	s.stopDrain() // ends the response-drain goroutine (race-free; see stopDrain)
	writers := make([]*connWriter, 0, len(s.subs))
	for ch, cw := range s.subs {
		writers = append(writers, cw)
		delete(s.subs, ch)
		close(ch) // readLoop has stopped, so no send can race this close
	}
	s.mu.Unlock()

	for _, cw := range writers {
		if cw == nil {
			continue
		}
		b, _ := protocol.EncodeEnvelope(protocol.Envelope{
			Type: protocol.MsgSessionState, SessionID: s.ID, Status: status,
		})
		_ = cw.write(protocol.Frame{Type: protocol.TypeControl, Payload: b})
	}

	d.mu.Lock()
	delete(d.sessions, s.ID)
	d.mu.Unlock()
}

func (d *Daemon) streamTo(cw *connWriter, s *Session) {
	ch, snap := s.SubscribeWithScrollback(cw)
	defer s.Unsubscribe(ch)
	for _, b := range snap {
		if err := cw.write(protocol.Frame{Type: protocol.TypeOutput, SessionID: s.ID, Payload: b}); err != nil {
			return
		}
	}
	for b := range ch {
		if err := cw.write(protocol.Frame{Type: protocol.TypeOutput, SessionID: s.ID, Payload: b}); err != nil {
			return
		}
	}
}

func (d *Daemon) listSessions(tree procTree) []protocol.SessionInfo {
	d.mu.Lock()
	// Collect live pids so we can report where each agent actually is *now*
	// (the shell cd's around and launches agents), not the dir the session was
	// created in. Snapshot under the lock; the lsof probe runs unlocked below.
	type row struct {
		info protocol.SessionInfo
		pid  int
	}
	rows := make([]row, 0, len(d.sessions))
	for _, s := range d.sessions {
		info, pid := s.snapshot()
		rows = append(rows, row{info: info, pid: pid})
	}
	d.mu.Unlock()

	pids := make([]int, 0, len(rows))
	for _, r := range rows {
		if r.pid != 0 {
			pids = append(pids, r.pid)
		}
	}
	live := cwdForPids(pids) // pid -> current working directory (best-effort)

	out := make([]protocol.SessionInfo, 0, len(rows))
	for _, r := range rows {
		if cwd := live[r.pid]; cwd != "" {
			r.info.Cwd = cwd
			r.info.Repo = repoName(cwd)
		}
		// Title follows what's running inside the session's shell: show the
		// agent (claude/codex/…) if one is running, else the shell itself.
		if agent := tree.agentUnder(r.pid); agent != "" {
			r.info.Agent = agent
		}
		out = append(out, r.info)
	}
	return out
}

func (d *Daemon) session(id uint32) *Session {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sessions[id]
}

func (d *Daemon) allocID() uint32 {
	d.mu.Lock()
	defer d.mu.Unlock()
	id := d.nextID
	d.nextID++
	return id
}

func (d *Daemon) sendControl(cw *connWriter, env protocol.Envelope) {
	b, _ := protocol.EncodeEnvelope(env)
	_ = cw.write(protocol.Frame{Type: protocol.TypeControl, Payload: b})
}

func (d *Daemon) sendError(cw *connWriter, msg string) {
	d.sendControl(cw, protocol.Envelope{Type: protocol.MsgError, Message: msg})
}

// loginShell is the user's shell for command-less sessions, matching the TUI.
func loginShell() string {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "/bin/bash"
}

func (d *Daemon) Shutdown() {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, s := range d.sessions {
		s.Kill()
	}
}
