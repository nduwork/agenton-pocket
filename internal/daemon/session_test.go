package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testPreset() Preset {
	// Run the testdata echo stub under a PTY. Pin Cwd to the package dir so
	// ./testdata/echo.go resolves (sessions now default cwd to $HOME, not the
	// daemon's launch dir). The stub file is build-tagged `ignore`.
	return Preset{Agent: "stub", Cwd: ".", Command: "go", Args: []string{"run", "./testdata/echo.go"}}
}

func TestSessionStartAndOutput(t *testing.T) {
	s := NewSession(1, "test", testPreset())
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Kill()

	ch, _ := s.SubscribeWithScrollback(nil)
	defer s.Unsubscribe(ch)
	s.WriteInput([]byte("hello\n"))

	var got strings.Builder
	deadline := time.After(3 * time.Second)
	for !strings.Contains(got.String(), "hello") {
		select {
		case b := <-ch:
			got.Write(b)
		case <-deadline:
			t.Fatalf("timeout, got: %q", got.String())
		}
	}
}

func TestDoActionAcceptSendsNewline(t *testing.T) {
	s := NewSession(2, "act", testPreset())
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Kill()
	ch, _ := s.SubscribeWithScrollback(nil)
	defer s.Unsubscribe(ch)

	// type text, then "accept" (Enter) submits the line; stub echoes "echo: xyz"
	if err := s.WriteInput([]byte("xyz")); err != nil {
		t.Fatal(err)
	}
	if err := s.DoAction("accept"); err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	deadline := time.After(3 * time.Second)
	for !strings.Contains(got.String(), "echo: xyz") {
		select {
		case b := <-ch:
			got.Write(b)
		case <-deadline:
			t.Fatalf("timeout, got: %q", got.String())
		}
	}
}

// A session launched as a plain shell that then runs claude inside it must use
// claude's intent keys (Esc to interrupt/reject), not the shell's Ctrl+C — the
// bug where the pad "Stop"/"Reject"/"Mode"/"New" buttons did nothing useful
// against claude when it was started from a shell session in the TUI.
func TestCurrentKeysFollowsAgentLaunchedInsideShell(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep binary to stand in for the agent")
	}
	// a fake "claude" so the process table shows a command matching the agent
	// regex — a copy (not symlink: macOS ps reports the symlink target's name).
	dir := t.TempDir()
	fake := filepath.Join(dir, "claude")
	if err := copyExec(sleep, fake); err != nil {
		t.Fatal(err)
	}

	s := NewSession(9, "shell", Preset{Command: "sh"})
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Kill()

	// before any agent runs, the shell session uses shell keys: Ctrl+C to reject
	// and NO mode key at all (a plain shell has no mode to switch — this is why
	// the Mode button did nothing against claude-in-a-shell).
	if got := s.currentKeys().reject; len(got) != 1 || got[0] != "Ctrl+C" {
		t.Fatalf("bare shell reject = %v, want [Ctrl+C]", got)
	}
	if got := s.currentKeys().modeSwitch; len(got) != 0 {
		t.Fatalf("bare shell modeSwitch = %v, want none", got)
	}

	// launch the fake claude as a child of the shell.
	s.WriteInput([]byte(fake + " 30 &\n"))

	deadline := time.Now().Add(3 * time.Second)
	for {
		k := s.currentKeys()
		if len(k.reject) == 1 && k.reject[0] == "Esc" &&
			len(k.modeSwitch) == 1 && k.modeSwitch[0] == "Shift+Tab" {
			break // claude keys now in effect, including Mode
		}
		if time.Now().After(deadline) {
			t.Fatalf("keys never switched to claude's; reject=%v modeSwitch=%v", k.reject, k.modeSwitch)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func copyExec(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o755)
}

// When the PTY read loop hits EOF (the process exited), the session status flips
// to "exited" and the onExit hook fires exactly once — the signal the daemon
// uses to notify clients and drop the session.
func TestReadLoopExitCallsOnExit(t *testing.T) {
	s := NewSession(1, "x", Preset{})
	fired := make(chan struct{})
	s.onExit = func() { close(fired) }
	go s.readLoop(strings.NewReader("")) // immediate EOF

	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("onExit not called when the stream ended")
	}
	if s.status() != "exited" {
		t.Fatalf("status = %q, want exited", s.status())
	}
}

// handleSessionExit must drop the session from the list, close every
// subscriber channel (so streamTo returns), and make late subscribers get a
// closed channel instead of blocking forever.
func TestHandleSessionExitRemovesAndClosesSubs(t *testing.T) {
	d := New(Config{}, "")
	id := d.allocID()
	s := NewSession(id, "x", Preset{Command: "sleep"})
	d.mu.Lock()
	d.sessions[id] = s
	d.mu.Unlock()
	ch, _ := s.SubscribeWithScrollback(nil)

	d.handleSessionExit(s)

	if d.session(id) != nil {
		t.Fatal("session not removed from the list after exit")
	}
	if _, ok := <-ch; ok {
		t.Fatal("subscriber channel was not closed on exit")
	}
	// A subscribe that races in after exit gets a closed channel, not a leak.
	ch2, snap := s.SubscribeWithScrollback(nil)
	if snap != nil {
		t.Fatalf("late subscribe returned scrollback %v, want nil", snap)
	}
	if _, ok := <-ch2; ok {
		t.Fatal("late subscriber channel was not closed")
	}
}

func TestScrollbackReplay(t *testing.T) {
	s := NewSession(3, "sb", testPreset())
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Kill()
	ch, _ := s.SubscribeWithScrollback(nil)
	defer s.Unsubscribe(ch)
	s.WriteInput([]byte("persist\n"))

	var got strings.Builder
	deadline := time.After(3 * time.Second)
	for !strings.Contains(got.String(), "persist") {
		select {
		case b := <-ch:
			got.Write(b)
		case <-deadline:
			t.Fatalf("timeout, got: %q", got.String())
		}
	}
	// Snapshot scrollback via the production API — a fresh subscribe returns the
	// current scrollback under the same lock.
	ch2, sb := s.SubscribeWithScrollback(nil)
	defer s.Unsubscribe(ch2)
	var all strings.Builder
	for _, b := range sb {
		all.Write(b)
	}
	if !strings.Contains(all.String(), "persist") {
		t.Fatalf("scrollback missing output: %q", all.String())
	}
}

func TestEncodeKeyGenericCtrlAndArrows(t *testing.T) {
	cases := map[string][]byte{
		"Ctrl+C":        {0x03},
		"Ctrl+O":        {0x0f},
		"Ctrl+J":        {0x0a},
		"Enter":         {0x0d}, // Return sends CR; LF would be a soft newline in raw-mode TUIs
		"Shift+Tab":     {0x1b, '[', 'Z'},
		"Left":          {0x1b, '[', 'D'},
		"Right":         {0x1b, '[', 'C'},
		"Backspace":     {0x7f},                // the backspace key sends DEL, not BS
		"Delete":        {0x1b, '[', '3', '~'}, // forward delete (fn+delete)
		"Alt+Backspace": {0x1b, 0x7f},          // delete word
		"/compact":      []byte("/compact"),    // unknown names stay literal text
	}
	for k, want := range cases {
		if got := encodeKey(k); string(got) != string(want) {
			t.Errorf("encodeKey(%q) = %v, want %v", k, got, want)
		}
	}
}

func TestInterruptAndRewindSequences(t *testing.T) {
	s := NewSession(1, "a", Preset{Command: "claude"})
	if seq := s.actionSequence("interrupt"); len(seq) != 1 || seq[0] != "Esc" {
		t.Errorf("claude interrupt = %v, want [Esc]", seq)
	}
	if seq := s.actionSequence("rewind"); len(seq) != 2 || seq[0] != "Esc" || seq[1] != "Esc" {
		t.Errorf("rewind = %v, want [Esc Esc]", seq)
	}
	// Unrecognized commands are treated as a plain shell: Ctrl+C stops.
	o := NewSession(2, "b", Preset{Command: "bash"})
	if seq := o.actionSequence("interrupt"); len(seq) != 1 || seq[0] != "Ctrl+C" {
		t.Errorf("shell interrupt = %v, want [Ctrl+C]", seq)
	}
	// Raw-key actions are agent-independent; esc stays Esc even on shells.
	for action, want := range map[string]string{"esc": "Esc", "left": "Left", "right": "Right"} {
		if seq := o.actionSequence(action); len(seq) != 1 || seq[0] != want {
			t.Errorf("%s = %v, want [%s]", action, seq, want)
		}
	}
	// exit is the universal quit: Ctrl+C for every agent and shell alike.
	for _, s := range []*Session{s, o} {
		if seq := s.actionSequence("exit"); len(seq) != 1 || seq[0] != "Ctrl+C" {
			t.Errorf("exit = %v, want [Ctrl+C]", seq)
		}
	}
}

func TestNewChatSequencesPerAgent(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    []string
	}{
		{"claude", []string{"/clear", "Enter"}},
		{"codex", []string{"/new", "Enter"}},
		{"bash", []string{"Ctrl+L"}},
	} {
		s := NewSession(1, "a", Preset{Command: tc.command})
		seq := s.actionSequence("new_chat")
		if len(seq) != len(tc.want) {
			t.Errorf("%s new_chat = %v, want %v", tc.command, seq, tc.want)
			continue
		}
		for i := range seq {
			if seq[i] != tc.want[i] {
				t.Errorf("%s new_chat = %v, want %v", tc.command, seq, tc.want)
				break
			}
		}
	}
}

func TestEncodeChords(t *testing.T) {
	cases := map[string][]byte{
		"Ctrl+Space":    {0x00},             // Ctrl+Space -> NUL
		"Ctrl+K":        {0x0b},             // Ctrl+letter -> control byte
		"Ctrl+k":        {0x0b},             // case-insensitive base
		"Ctrl+Shift+K":  {0x0b},             // shift collapses under ctrl, like a real PTY
		"Alt+a":         {0x1b, 'a'},        // Alt/Option -> ESC prefix
		"Shift+a":       {'A'},              // shift uppercases
		"Ctrl+Enter":    {'\r'},             // Ctrl+Enter (no ctrl mapping) -> CR
		"/compact":      []byte("/compact"), // literal text, not a chord
		"/compact args": []byte("/compact args"),
		// ⌘/Super have no terminal encoding, so they fall through to literal text
		// — which is exactly why the rebind UI blocks them before they get here.
		"Cmd+K":   []byte("Cmd+K"),
		"Super+x": []byte("Super+x"),
	}
	for k, want := range cases {
		if got := encodeKey(k); string(got) != string(want) {
			t.Errorf("encodeKey(%q) = %v, want %v", k, got, want)
		}
	}
}

func TestCustomBindingIsSingleKeystroke(t *testing.T) {
	s := NewSession(1, "a", Preset{Command: "claude"})
	_ = s.SetCustom("custom_1", "Ctrl+Space")
	if seq := s.actionSequence("custom_1"); len(seq) != 1 || seq[0] != "Ctrl+Space" {
		t.Errorf("custom_1 = %v, want [Ctrl+Space]", seq)
	}
}

func TestSetCustomRebindsAtRuntime(t *testing.T) {
	s := NewSession(1, "a", Preset{Command: "claude", Buttons: Buttons{Custom1: "Ctrl+O"}})
	if seq := s.actionSequence("custom_1"); len(seq) != 1 || seq[0] != "Ctrl+O" {
		t.Fatalf("initial custom_1 = %v, want [Ctrl+O]", seq)
	}
	if seq := s.actionSequence("custom_2"); seq != nil {
		t.Fatalf("unset custom_2 = %v, want nil", seq)
	}
	if err := s.SetCustom("custom_1", "/compact"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetCustom("custom_2", "Shift+Tab"); err != nil {
		t.Fatal(err)
	}
	if seq := s.actionSequence("custom_1"); len(seq) != 1 || seq[0] != "/compact" {
		t.Errorf("rebound custom_1 = %v, want [/compact]", seq)
	}
	if seq := s.actionSequence("custom_2"); len(seq) != 1 || seq[0] != "Shift+Tab" {
		t.Errorf("rebound custom_2 = %v, want [Shift+Tab]", seq)
	}
	if err := s.SetCustom("reject", "x"); err == nil {
		t.Error("rebinding a fixed button should fail")
	}
}

// A daemon started from inside a Claude Code session must not pass that
// session's identity down to the agents it spawns — otherwise every `claude` in
// an agenton session disables transcript saving ("inherited
// CLAUDE_CODE_CHILD_SESSION marker"). Drives a real PTY and reads the child's
// actual environment, so it covers the spawn path, not just the helper.
func TestSessionStripsInheritedClaudeSessionMarkers(t *testing.T) {
	t.Setenv("CLAUDE_CODE_CHILD_SESSION", "1")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "should-not-leak")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli") // descriptive: must pass through

	p := Preset{Agent: "env", Cwd: ".", Command: "sh", Args: []string{"-c", "env; echo ENVDONE"}}
	s := NewSession(90, "env", p)
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Kill()
	ch, _ := s.SubscribeWithScrollback(nil)
	defer s.Unsubscribe(ch)

	var got strings.Builder
	deadline := time.After(5 * time.Second)
	for !strings.Contains(got.String(), "ENVDONE") {
		select {
		case b := <-ch:
			got.Write(b)
		case <-deadline:
			t.Fatalf("timeout waiting for env dump, got: %q", got.String())
		}
	}
	out := got.String()
	for _, k := range inheritedSessionMarkers {
		if strings.Contains(out, k+"=") {
			t.Errorf("%s leaked into the session environment", k)
		}
	}
	// Sanity: the child really did inherit an environment (so absence above is
	// stripping, not an empty env), and non-identity vars survive.
	if !strings.Contains(out, "AGENTON_SESSION=1") {
		t.Error("AGENTON_SESSION=1 missing — env not populated as expected")
	}
	if !strings.Contains(out, "CLAUDE_CODE_ENTRYPOINT=cli") {
		t.Error("CLAUDE_CODE_ENTRYPOINT was stripped; only identity markers should be")
	}
}

// A preset that sets a marker explicitly still wins over the strip.
func TestStripEnvPresetOverrideWins(t *testing.T) {
	env := append(stripEnv([]string{"CLAUDE_CODE_SESSION_ID=inherited", "PATH=/bin"},
		inheritedSessionMarkers...), envList(map[string]string{"CLAUDE_CODE_SESSION_ID": "explicit"})...)
	if !hasEnv(env, "PATH") {
		t.Error("PATH dropped")
	}
	var vals []string
	for _, kv := range env {
		if strings.HasPrefix(kv, "CLAUDE_CODE_SESSION_ID=") {
			vals = append(vals, kv)
		}
	}
	if len(vals) != 1 || vals[0] != "CLAUDE_CODE_SESSION_ID=explicit" {
		t.Errorf("want exactly the explicit value, got %v", vals)
	}
}
