package daemon

import "testing"

// The session title follows the agent running inside a shell: a claude/codex
// descendant of the session's shell is detected (shallowest match), and a bare
// shell with no agent reports none.
func TestAgentUnderProcTree(t *testing.T) {
	// 500 = daemon; 600 = shell session; 700 = claude launched in it; 800 = a
	// child of claude. 900 = a second shell session running nothing.
	psOut := `
  500     1 /path/agenton daemon
  600   500 /bin/zsh -l
  700   600 claude --model o5
  800   700 node /some/helper.js
  900   500 /bin/zsh -l
`
	tree := parseProcTree(psOut)
	if got := tree.agentUnder(600); got != "claude" {
		t.Errorf("agentUnder(shell with claude) = %q, want claude", got)
	}
	if got := tree.agentUnder(900); got != "" {
		t.Errorf("agentUnder(bare shell) = %q, want empty", got)
	}
	if d := tree.descendants(600); len(d) != 2 { // claude + its child
		t.Errorf("descendants(600) = %v, want 2", d)
	}
}

// opencode must be detected too — its keys are wired in keysForCommand, so the
// title/keys resolver has to recognize it like claude and codex.
func TestAgentUnderDetectsOpencode(t *testing.T) {
	tree := parseProcTree("  600     1 /bin/zsh -l\n  700   600 opencode\n")
	if got := tree.agentUnder(600); got != "opencode" {
		t.Errorf("agentUnder(shell with opencode) = %q, want opencode", got)
	}
}

// updateLiveAgent reports a change only on a real transition, so the watcher
// broadcasts a title update once per shell→agent→shell move, not every tick.
func TestUpdateLiveAgent(t *testing.T) {
	s := NewSession(1, "zsh", Preset{Command: "zsh", Agent: "zsh"})
	if s.updateLiveAgent("zsh") { // matches the launch agent set in NewSession
		t.Fatal("no-op update reported a change")
	}
	if !s.updateLiveAgent("claude") {
		t.Fatal("shell→claude not reported as a change")
	}
	if s.updateLiveAgent("claude") {
		t.Fatal("repeat claude reported a change")
	}
	if !s.updateLiveAgent("zsh") {
		t.Fatal("claude→shell not reported as a change")
	}
}

func TestFirstWordBase(t *testing.T) {
	cases := map[string]string{
		"claude":                       "claude",
		"/opt/homebrew/bin/ollama run": "ollama",
		"codex --model o5":             "codex",
	}
	for in, want := range cases {
		if got := firstWordBase(in); got != want {
			t.Errorf("firstWordBase(%q) = %q, want %q", in, got, want)
		}
	}
}
