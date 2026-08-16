package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"golang.org/x/sys/unix"

	"github.com/nduwork/agenton-pocket/internal/protocol"
	"github.com/nduwork/agenton-pocket/internal/vtmode"
)

// emuScrollback is the depth of the daemon-side emulator's history. It bounds
// how much transcript a fresh attach can replay ("view all contents"), and the
// per-session memory the emulator holds. Shared with the TUI pager so both
// sides retain the same depth.
const emuScrollback = vtmode.ScrollbackLines

// subBuffer is the per-subscriber channel depth. Full-screen agent redraws
// arrive as bursts of 4 KiB chunks; a deep buffer keeps a briefly-stalled
// client from losing bytes mid-escape-sequence (which corrupts its VT state).
const subBuffer = 1024

type Session struct {
	ID           uint32
	Name         string
	Agent        string
	Cwd          string
	Repo         string // short display label: git repo name or cwd folder name
	Status       string
	LastActivity int64

	preset Preset
	keys   agentKeys // fixed per-agent button sequences (not user-configurable)

	mu         sync.Mutex
	cols, rows int
	cmd        *exec.Cmd
	tty        *os.File
	// emu is a daemon-side terminal emulator fed every PTY byte. It is the
	// session's source of truth for attach replay: agents like claude repaint
	// their live frame in place, so replaying raw output chunks reconstructs
	// only the final screen — the transcript history that scrolled off is lost
	// to a late-attaching client. The emulator keeps that history as scrollback
	// lines; SubscribeWithScrollback serializes scrollback + screen + cursor so
	// a fresh attach gets the whole conversation, scrollable client-side.
	emu *vt.Emulator
	// modes is the set of DEC private modes (?Nm) currently ON, tracked across
	// ALL output. The attach replay prepends the current set (bracketed paste,
	// focus reporting, alt screen, ...) so a re-attaching terminal restores the
	// agent's screen state that a mid-session snapshot can't otherwise carry.
	modes     map[int]bool
	subs      map[chan []byte]*connWriter
	// reqSize is each client conn's last requested dimensions. The shared PTY
	// WIDTH is clamped to the widest entry so a narrow client (phone) never
	// shrinks it under a wider one (desk): lines emitted while narrow freeze at
	// that width in scrollback, and no emulator reflows history — so a desk that
	// later widens is stuck with narrow-wrapped history. Height still follows the
	// active owner (height has no such frozen-history problem). Guarded by mu.
	reqSize   map[*connWriter][2]int
	sizeOwner any  // the client conn whose size the shared PTY follows
	ended     bool // process exited/killed: reject new subscribers with a closed channel
	// liveAgent is the agent name last pushed to attached clients as the session
	// title: the sub-agent running inside the session's shell (claude/codex/…),
	// or the session's own launch agent when none. The agent watcher diffs against
	// it to push a title update only on change. Guarded by mu.
	liveAgent string

	// onExit fires once when the process exits (or is killed). The daemon sets it
	// to notify attached clients the session ended and drop it from the list.
	onExit   func()
	exitOnce sync.Once
}

// ownsSize reports whether conn may drive the shared PTY size. If unowned and
// claim is set, conn takes ownership. This keeps a passive attach/resize from a
// second client (e.g. the phone peeking) from shrinking the PTY under the
// client actively using it — ownership only moves on real input (takeSize).
func (s *Session) ownsSize(conn any, claim bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sizeOwner == nil && claim {
		s.sizeOwner = conn
	}
	return s.sizeOwner == conn
}

// takeSize makes conn the size owner (called when conn sends input — the
// actively-driven device wins the shared PTY size).
func (s *Session) takeSize(conn any) {
	s.mu.Lock()
	s.sizeOwner = conn
	s.mu.Unlock()
}

// ownerConn returns the current size owner as a *connWriter (nil if unowned).
func (s *Session) ownerConn() *connWriter {
	s.mu.Lock()
	defer s.mu.Unlock()
	cw, _ := s.sizeOwner.(*connWriter)
	return cw
}

// reassignOwnerAfter releases cw and, if any other subscriber exists, makes
// one of them the new owner. Returns the new owner (nil if none left). Called
// on disconnect and on an explicit set_active release.
func (s *Session) reassignOwnerAfter(cw *connWriter) *connWriter {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sizeOwner == cw {
		s.sizeOwner = nil
	}
	if s.sizeOwner == nil {
		for _, other := range s.subs {
			if other != cw {
				s.sizeOwner = other
				break
			}
		}
	}
	owner, _ := s.sizeOwner.(*connWriter)
	return owner
}

// recordWidth records cw's requested dimensions and returns the width the shared
// PTY should use: the max requested width across all clients. Clamping to the
// widest client is what stops a narrow phone from shrinking the PTY under a
// wider desk and freezing narrow-wrapped lines into scrollback (see reqSize).
func (s *Session) recordWidth(cw *connWriter, cols, rows int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reqSize[cw] = [2]int{cols, rows}
	return s.maxColsLocked()
}

// dropWidth forgets cw's requested size (on disconnect) and returns the new
// clamped width — 0 if no client remains. The caller shrinks the PTY to it so
// the width comes back down once the widest client leaves.
func (s *Session) dropWidth(cw *connWriter) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.reqSize, cw)
	return s.maxColsLocked()
}

// maxColsLocked returns the widest requested width across attached clients (0 if
// none). Caller holds s.mu.
func (s *Session) maxColsLocked() int {
	w := 0
	for _, sz := range s.reqSize {
		if sz[0] > w {
			w = sz[0]
		}
	}
	return w
}

// hasSubs reports whether any client is attached — the agent watcher skips its
// process scan entirely when nobody is watching.
func (s *Session) hasSubs() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.subs) > 0
}

// updateLiveAgent records the session's current live agent and reports whether
// it changed since the last push (so the watcher broadcasts a title update only
// on a real transition, e.g. shell → claude → shell).
func (s *Session) updateLiveAgent(agent string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if agent == s.liveAgent {
		return false
	}
	s.liveAgent = agent
	return true
}

func NewSession(id uint32, name string, preset Preset) *Session {
	emu := vt.NewEmulator(80, 24) // resized to the real PTY size on first Resize
	emu.SetScrollbackSize(emuScrollback)
	// The emulator answers terminal queries (DA, DSR, ...) on its read pipe;
	// nobody should see those (the attached client's real terminal answers the
	// agent), but the pipe must be drained or the emulator wedges. Stopped at
	// session exit via stopDrain.
	go func() { _, _ = io.Copy(io.Discard, emu) }()
	return &Session{
		ID:        id,
		Name:      name,
		Agent:     preset.Agent,
		Cwd:       preset.Cwd,
		preset:    preset,
		keys:      keysForCommand(strings.Join(append([]string{preset.Command}, preset.Args...), " ")),
		Status:    "starting",
		subs:      map[chan []byte]*connWriter{},
		reqSize:   map[*connWriter][2]int{},
		modes:     map[int]bool{},
		emu:       emu,
		liveAgent: preset.Agent, // updated by the daemon's agent watcher as sub-agents come and go
	}
}

// stopDrain ends the response-drain goroutine started in NewSession. It closes
// the emulator's reply pipe directly instead of calling emu.Close(): Close()
// also flips an unsynchronized `closed` bool that the drain goroutine reads
// inside emu.Read, a data race the detector flags (go test -race gates CI).
// Closing the pipe unblocks the drain through io.Pipe's own synchronization,
// with no shared bool. Caller holds s.mu.
func (s *Session) stopDrain() {
	if pw, ok := s.emu.InputPipe().(*io.PipeWriter); ok {
		_ = pw.CloseWithError(io.EOF)
		return
	}
	_ = s.emu.Close() // fallback if upstream changes the reply pipe's type
}

func (s *Session) Start() error {
	command := s.preset.Command
	if command == "" {
		command = s.Agent
	}
	c := exec.Command(command, s.preset.Args...)
	// Default every session to the user's home dir — not wherever the daemon
	// happened to be launched (usually a repo checkout). An explicit cwd wins.
	dir := expandHome(s.Cwd)
	if s.Cwd == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dir = home
		}
	}
	if dir != "" {
		// Validate up front: a bad Dir surfaces from fork/exec as a misleading
		// "no such file or directory" attributed to the binary itself.
		if st, err := os.Stat(dir); err != nil || !st.IsDir() {
			return errors.New("working directory not found: " + dir)
		}
		c.Dir = dir
		s.Repo = repoName(dir)
	}
	// Strip the launcher's Claude Code session markers before layering preset env,
	// so an explicit preset value still wins (see inheritedSessionMarkers).
	env := append(stripEnv(os.Environ(), inheritedSessionMarkers...), envList(s.preset.Env)...)
	// Ensure the child sees a capable terminal type so agents emit full ANSI
	// (colors, cursor, alt-screen) instead of falling back to dumb output.
	if !hasEnv(env, "TERM") {
		env = append(env, "TERM=xterm-256color")
	}
	// Mark the session's environment so a shell inside it can't launch a nested
	// daemon (which would clobber this daemon's socket — see cmd/agenton guards).
	env = append(env, "AGENTON_SESSION=1")
	c.Env = env
	tty, err := pty.Start(c)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.cmd = c
	s.tty = tty
	s.Status = "running"
	s.LastActivity = time.Now().Unix()
	s.mu.Unlock()
	go s.readLoop(tty)
	return nil
}

func (s *Session) readLoop(r io.Reader) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			out := make([]byte, n)
			copy(out, buf[:n])
			s.handleOutput(out)
		}
		if err != nil {
			s.mu.Lock()
			if s.Status != "killed" {
				s.Status = "exited"
			}
			s.mu.Unlock()
			// Notify the daemon so it tells attached clients and drops the
			// session; once, whether we got here via natural exit or Kill.
			if s.onExit != nil {
				s.exitOnce.Do(s.onExit)
			}
			return
		}
	}
}

func (s *Session) handleOutput(b []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.LastActivity = time.Now().Unix()
	vtmode.UpdatePrivateModes(b, s.modes)
	_, _ = s.emu.Write(b)
	for ch := range s.subs {
		select {
		case ch <- b:
		default:
			// ponytail: drop for a wedged subscriber (dead conn) so the PTY
			// read loop never blocks; live clients are protected by the large
			// buffer. Upgrade path: per-subscriber overflow spill if real
			// clients ever drop frames.
		}
	}
}

func (s *Session) WriteInput(b []byte) error {
	s.mu.Lock()
	tty := s.tty
	s.LastActivity = time.Now().Unix()
	s.mu.Unlock()
	if tty == nil {
		return errors.New("session not started")
	}
	_, err := tty.Write(b)
	return err
}

// Resize updates the PTY window size so the child process reflows its TUI to
// match the attached client's terminal dimensions. Safe to call before or
// after Start; a no-op for not-yet-started sessions.
func (s *Session) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return errors.New("invalid size")
	}
	s.mu.Lock()
	tty := s.tty
	s.cols, s.rows = cols, rows
	s.emu.Resize(cols, rows)
	s.mu.Unlock()
	if tty == nil {
		return errors.New("session not started")
	}
	return setPTYSize(tty, cols, rows)
}

// setPTYSize resizes the PTY window race-safely. pty.Setsize calls tty.Fd(),
// which flips the os.File's poll state and races readLoop's concurrent Read
// (and Kill's Close) on the same fd — a data race the detector flags.
// SyscallConn().Control hands us the fd under the runtime's file lock instead,
// serialized against Read/Close (and it errors cleanly if the fd is closed).
func setPTYSize(tty *os.File, cols, rows int) error {
	conn, err := tty.SyscallConn()
	if err != nil {
		return err
	}
	var ioctlErr error
	if err := conn.Control(func(fd uintptr) {
		ioctlErr = unix.IoctlSetWinsize(int(fd), unix.TIOCSWINSZ, &unix.Winsize{
			Row: uint16(rows), Col: uint16(cols),
		})
	}); err != nil {
		return err
	}
	return ioctlErr
}

// Size returns the PTY size last applied via Resize (0,0 before the first).
func (s *Session) Size() (cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cols, s.rows
}

// WriteText writes user-typed text. A trailing CR is written as its own
// delayed write: agents distinguish a typed Enter from a pasted newline by
// read-chunk boundaries, so "text\r" in one write lands as a soft newline in
// the agent's input box instead of submitting it.
func (s *Session) WriteText(text string) error {
	body, hadCR := strings.CutSuffix(text, "\r")
	if body != "" {
		if err := s.WriteInput([]byte(body)); err != nil {
			return err
		}
	}
	if !hadCR {
		return nil
	}
	if body != "" {
		time.Sleep(keyGap)
	}
	return s.WriteInput([]byte{'\r'})
}

// keyGap separates programmatic keystrokes so agent TUIs read them as distinct
// key presses. Agents detect pastes by read-burst timing and treat a CR inside
// a burst as a soft newline instead of submit; 10ms cleared claude's window but
// not codex's. 50ms is beyond both and still imperceptible to a human.
const keyGap = 50 * time.Millisecond

// DoAction maps an action name to the session's configured key sequence and
// writes it. Keys in a multi-key sequence are written with a short gap so
// ESC-prefixed sequences register as separate key presses (two ESC bytes in
// one write would otherwise be swallowed as one escape-sequence prefix).
func (s *Session) DoAction(action string) error {
	for i, k := range s.actionSequence(action) {
		if i > 0 {
			time.Sleep(keyGap) // also keeps "/new"+Enter outside paste-burst windows
		}
		if err := s.WriteInput(encodeKey(k)); err != nil {
			return err
		}
	}
	return nil
}

// currentKeys resolves the intent-button sequences for whatever agent is
// running in the session *right now*, not just what it was launched as. A
// session started as a plain shell that then runs `claude` gets claude's keys
// (Esc to interrupt, /clear for New), not the shell's Ctrl+C/Ctrl+L — mirroring
// how listSessions already retitles the session from its live process tree.
// Falls back to the launch-time keys when no known agent is running under it
// (a bare shell, or claude launched directly as the session's own process,
// which agentUnder deliberately doesn't match since it scans descendants).
//
// ponytail: one `ps` per agent-button tap (reject/interrupt/mode_switch/
// new_chat only — arrows/accept are hardcoded). Human tap cadence makes this
// cheap; cache off a proc-table watch if it ever shows up in profiles.
func (s *Session) currentKeys() agentKeys {
	p := s.pid()
	if p == 0 {
		return s.keys
	}
	if agent := scanProcs().agentUnder(p); agent != "" {
		return keysForCommand(agent)
	}
	return s.keys
}

// actionSequence maps an action name to its key sequence. Only custom_1 and
// custom_2 come from user config; everything else is fixed per agent (the
// session's keys, resolved from its command line — claude, codex, or shell).
func (s *Session) actionSequence(action string) []string {
	switch action {
	case "accept":
		return []string{"Enter"}
	case "reject":
		return s.currentKeys().reject
	case "up":
		return []string{"Up"}
	case "down":
		return []string{"Down"}
	case "left":
		return []string{"Left"}
	case "right":
		return []string{"Right"}
	case "esc":
		// Raw escape (dismiss menu/dialog). Distinct from interrupt, which is
		// Ctrl+C on shell sessions.
		return []string{"Esc"}
	case "interrupt":
		return s.currentKeys().interrupt
	case "exit":
		// Universal quit: Ctrl+C is the exit key for claude, codex, opencode and
		// plain shells alike. One press per tap (native behavior) — claude/codex
		// need two to fully exit, opencode/shells one.
		return []string{"Ctrl+C"}
	case "rewind":
		// Edit-previous / rewind menu in both claude and codex.
		return []string{"Esc", "Esc"}
	case "mode_switch":
		return s.currentKeys().modeSwitch
	case "new_chat":
		// Start a fresh conversation — typed as a slash command per agent
		// (/clear for claude, /new for codex), Ctrl+L on plain shells.
		return s.currentKeys().newChat
	case "custom_1", "custom_2":
		s.mu.Lock()
		defer s.mu.Unlock()
		v := s.preset.Buttons.Custom1
		if action == "custom_2" {
			v = s.preset.Buttons.Custom2
		}
		if v == "" {
			return nil
		}
		// A custom binding is one keystroke: a key chord ("Ctrl+Space") that
		// encodeKey turns into bytes, or literal text ("/compact"). No key
		// sequences — a chord is modifiers + one base key, not a chain.
		return []string{v}
	}
	return nil
}

// pid returns the session's child process id, or 0 before Start.
func (s *Session) pid() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil && s.cmd.Process != nil {
		return s.cmd.Process.Pid
	}
	return 0
}

// CommandLine returns the full launch command as typed ("ollama run glm"),
// so clients can offer "start another one like this".
func (s *Session) CommandLine() string {
	cmd := s.preset.Command
	if cmd == "" {
		cmd = s.Agent
	}
	return strings.Join(append([]string{cmd}, s.preset.Args...), " ")
}

// repoName returns a short display label for dir: the git repo's top-level
// directory name when dir is inside a work tree, otherwise the folder name.
// The git probe is bounded so a hung filesystem can't stall session startup.
func repoName(dir string) string {
	if dir == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err == nil {
		if top := strings.TrimSpace(string(out)); top != "" {
			return filepath.Base(top)
		}
	}
	return filepath.Base(dir)
}

// customBindings returns the session's current custom_1/custom_2 values, so an
// attaching client can restore its pad labels (the bindings persist on the
// Session across detach/re-attach).
func (s *Session) customBindings() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.preset.Buttons.Custom1, s.preset.Buttons.Custom2
}

// SetCustom rebinds a custom button at runtime (from the TUI). The value is a
// single key name / combo or a literal string — the same grammar as the
// config file. Session-scoped: config.toml is untouched.
func (s *Session) SetCustom(action, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch action {
	case "custom_1":
		s.preset.Buttons.Custom1 = value
	case "custom_2":
		s.preset.Buttons.Custom2 = value
	default:
		return errors.New("not a customizable button: " + action)
	}
	return nil
}

func (s *Session) Unsubscribe(ch chan []byte) {
	s.mu.Lock()
	delete(s.subs, ch)
	s.mu.Unlock()
}

// SubscribeWithScrollback atomically registers a subscription and snapshots
// the scrollback under the same lock, so a client replaying scrollback then
// reading the channel sees every byte exactly once, with no gap or duplicate.
func (s *Session) SubscribeWithScrollback(cw *connWriter) (chan []byte, [][]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		// The session already exited (handleSessionExit ran). Hand back a closed
		// channel so streamTo replays nothing and returns immediately instead of
		// blocking forever on a channel that will never receive or close.
		ch := make(chan []byte)
		close(ch)
		return ch, nil
	}
	ch := make(chan []byte, subBuffer)
	s.subs[ch] = cw
	// Serialize the emulator's full state — mode-restore lead, scrollback
	// history, current screen, cursor — so the attaching client starts with
	// the whole conversation in its own scrollback, scrollable natively.
	return ch, serializeReplay(s.emu, s.modes)
}

// modeRestoreBytes builds the ESC[?Nm sequence that re-enables every currently
// on DEC private mode, in ascending order for stable output. Returns nil if
// none are on.
func modeRestoreBytes(modes map[int]bool) []byte {
	if len(modes) == 0 {
		return nil
	}
	ns := make([]int, 0, len(modes))
	for n := range modes {
		if modes[n] {
			ns = append(ns, n)
		}
	}
	sort.Ints(ns)
	var sb strings.Builder
	for _, n := range ns {
		fmt.Fprintf(&sb, "\x1b[?%dh", n)
	}
	return []byte(sb.String())
}

// status returns the current session status under the lock.
func (s *Session) status() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Status
}

// snapshot returns the session's client-visible info plus its live pid, reading
// all mutable fields (Status, LastActivity, Cwd, Repo, cmd) under s.mu in one
// pass. listSessions calls this instead of touching the fields directly, which
// would race with the readLoop goroutine that updates LastActivity/Status.
func (s *Session) snapshot() (protocol.SessionInfo, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pid := 0
	if s.cmd != nil && s.cmd.Process != nil {
		pid = s.cmd.Process.Pid
	}
	return protocol.SessionInfo{
		ID: s.ID, Name: s.Name, Agent: s.Agent, Status: s.Status,
		Cwd: s.Cwd, Repo: s.Repo, LastActivity: s.LastActivity,
		CommandLine: s.CommandLine(),
	}, pid
}

func (s *Session) Kill() {
	s.mu.Lock()
	tty := s.tty
	cmd := s.cmd
	s.Status = "killed"
	s.mu.Unlock()
	if tty != nil {
		_ = tty.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
}

// --- key encoding helpers ---

func encodeKey(k string) []byte {
	switch k {
	case "Enter":
		// A real terminal's Return key sends CR (0x0d), not LF. Shells read
		// it as a line terminator via ICRNL; raw-mode TUIs (claude, codex)
		// treat CR as submit and LF as a soft newline — sending LF leaves the
		// prompt's input un-submitted (the agent never responds).
		return []byte{'\r'}
	case "Esc":
		return []byte{0x1b}
	case "Tab":
		return []byte{'\t'}
	case "Shift+Tab":
		return []byte{0x1b, '[', 'Z'}
	case "Space":
		return []byte{' '}
	case "Up":
		return []byte{0x1b, '[', 'A'}
	case "Down":
		return []byte{0x1b, '[', 'B'}
	case "Left":
		return []byte{0x1b, '[', 'D'}
	case "Right":
		return []byte{0x1b, '[', 'C'}
	case "Backspace":
		// Terminals send DEL (0x7f) for the backspace key, not BS (0x08).
		return []byte{0x7f}
	case "Delete":
		// Forward delete (fn+delete on a Mac keyboard): VT sequence CSI 3 ~.
		return []byte{0x1b, '[', '3', '~'}
	}
	// A chord — "Ctrl+Space", "Ctrl+K", "Alt+a", "Ctrl+Alt+K" — is modifiers
	// plus one base key. Anything the parser doesn't recognize falls through to
	// literal text (e.g. "/compact"), so slash commands are unaffected.
	if b, ok := encodeChord(k); ok {
		return b
	}
	// Plain text key (e.g. "n", "/compact") -> literal bytes.
	return []byte(k)
}

// encodeChord turns "Mod+...+Base" into terminal bytes, or (nil,false) if it
// isn't a recognizable chord. Terminal reality: Ctrl+letter is a control byte
// (Ctrl+A=0x01…Ctrl+Z=0x1a), Ctrl+Space/@ is NUL, Alt/Option prefixes ESC,
// Shift uppercases a letter. Ctrl+Shift+letter collapses to Ctrl+letter, as it
// does in a real PTY.
func encodeChord(k string) ([]byte, bool) {
	if !strings.Contains(k, "+") {
		return nil, false
	}
	parts := strings.Split(k, "+")
	base := parts[len(parts)-1]
	var ctrl, alt, shift bool
	for _, m := range parts[:len(parts)-1] {
		switch strings.ToLower(m) {
		case "ctrl", "control", "ctl", "c":
			ctrl = true
		case "alt", "opt", "option", "meta", "m":
			alt = true
		case "shift":
			shift = true
		default:
			return nil, false // unknown modifier -> not a chord, treat as literal
		}
	}

	var b byte
	switch base {
	case "Space":
		b = ' '
	case "Enter":
		b = '\r'
	case "Tab":
		b = '\t'
	case "Esc":
		b = 0x1b
	case "Backspace":
		b = 0x7f // Alt+Backspace = ESC DEL (delete word) via the alt prefix below
	default:
		if len(base) != 1 {
			return nil, false // multi-char base that isn't a named key -> literal
		}
		b = base[0]
	}
	if shift && b >= 'a' && b <= 'z' {
		b -= 0x20
	}
	if ctrl {
		switch {
		case b|0x20 >= 'a' && b|0x20 <= 'z':
			b = (b | 0x20) - 'a' + 1 // Ctrl+letter -> 0x01..0x1a
		case b == ' ' || b == '@':
			b = 0x00 // Ctrl+Space / Ctrl+@ -> NUL
		}
	}
	if alt {
		return []byte{0x1b, b}, true // Alt/Option -> ESC prefix
	}
	return []byte{b}, true
}

func expandHome(p string) string {
	if p == "" || p[0] != '~' {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, p[1:])
}

func envList(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

// inheritedSessionMarkers are env vars that identify the *launching* Claude Code
// session. The daemon is long-lived and is often started from inside one (e.g.
// `agenton vpn` run in a Claude Code shell), and every PTY it spawns inherits its
// environment — so without stripping these, a `claude` started in any agenton
// session would think it is a child of whatever launched the daemon: it disables
// transcript saving ("Transcript saving is off — inherited
// CLAUDE_CODE_CHILD_SESSION marker") and reuses a stale session id. A
// daemon-owned PTY is a fresh session, so it must not carry them.
//
// Deliberately narrow: only the two vars that carry session *identity*.
// CLAUDE_CODE_ENTRYPOINT / _EXECPATH are descriptive, and flags like
// CLAUDE_CODE_EXPERIMENTAL_* may be the user's own setting — those pass through.
var inheritedSessionMarkers = []string{
	"CLAUDE_CODE_CHILD_SESSION",
	"CLAUDE_CODE_SESSION_ID",
}

// stripEnv returns env without any KEY=… assignment for the given keys.
func stripEnv(env []string, keys ...string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		drop := false
		for _, k := range keys {
			if strings.HasPrefix(kv, k+"=") {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

// hasEnv reports whether the KEY= prefix already appears in env.
func hasEnv(env []string, key string) bool {
	pfx := key + "="
	for _, e := range env {
		if len(e) >= len(pfx) && e[:len(pfx)] == pfx {
			return true
		}
	}
	return false
}
