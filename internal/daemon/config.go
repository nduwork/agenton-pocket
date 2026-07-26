package daemon

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Buttons holds the two user-customizable pad buttons. Each is a single
// value: a recognized key name / combo ("Ctrl+O", "Shift+Tab") or a literal
// string sent as-is ("/compact") — see encodeKey. All other intent buttons
// (accept/reject/up/down/mode_switch/interrupt/rewind) are hardcoded per
// agent — see keysForCommand — and cannot be overridden from config.
type Buttons struct {
	Custom1 string `toml:"custom_1"`
	Custom2 string `toml:"custom_2"`
}

type Preset struct {
	Agent   string            `toml:"agent"`
	Cwd     string            `toml:"cwd"`
	Command string            `toml:"command"`
	Args    []string          `toml:"args"`
	Env     map[string]string `toml:"env"`
	Buttons Buttons           `toml:"buttons"`
}

type Config struct {
	Presets map[string]Preset `toml:"preset"`
}

// DefaultConfigPath returns the canonical config location.
func DefaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "agenton", "config.toml")
}

func LoadConfig(path string) (Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.Presets == nil {
		cfg.Presets = map[string]Preset{}
	}
	return cfg, nil
}

// agentKeys are the fixed (non-configurable) intent-button sequences the TUI
// exposes; the daemon picks them per session based on what agent the session
// runs.
type agentKeys struct {
	reject     []string
	modeSwitch []string
	interrupt  []string
	newChat    []string // start a fresh conversation (/clear, /new, …)
}

// keysForCommand resolves the fixed button sequences for a session's command
// line. The agent name is matched anywhere in the command line so wrapper
// launches like `ollama launch claude --model ...` still resolve to claude.
// Anything unrecognized is treated as a plain shell, where Ctrl+C is the only
// broadly meaningful stop/reject key.
func keysForCommand(cmdLine string) agentKeys {
	switch {
	case strings.Contains(strings.ToLower(cmdLine), "claude"):
		return agentKeys{
			// Esc selects "No, and tell Claude what to do differently" on the
			// tool-permission prompt — a real reject. The prompt's only hotkeys
			// are 1/2/3 and Esc, so a trailing "n" (removed) just typed a stray
			// character into the composer after the prompt closed.
			reject:     []string{"Esc"},
			modeSwitch: []string{"Shift+Tab"},
			interrupt:  []string{"Esc"},
			newChat:    []string{"/clear", "Enter"},
		}
	case strings.Contains(strings.ToLower(cmdLine), "codex"):
		return agentKeys{
			reject:     []string{"Esc"},
			modeSwitch: []string{"Shift+Tab"},
			interrupt:  []string{"Esc"},
			newChat:    []string{"/new", "Enter"},
		}
	case strings.Contains(strings.ToLower(cmdLine), "opencode"):
		// opencode: Esc interrupts (session_interrupt), Ctrl+C exits (app_exit),
		// Shift+Tab cycles agents (its "mode"), and a new session is <leader>n —
		// leader defaults to Ctrl+X, so Ctrl+X then n. It has no /clear or /new
		// slash command, so newChat is the leader combo, not typed text.
		return agentKeys{
			reject:     []string{"Esc"},
			modeSwitch: []string{"Shift+Tab"},
			interrupt:  []string{"Esc"},
			newChat:    []string{"Ctrl+X", "n"},
		}
	default:
		return agentKeys{
			reject:    []string{"Ctrl+C"},
			interrupt: []string{"Ctrl+C"},
			// plain shells have no "new conversation"; clear-screen is the analog
			newChat: []string{"Ctrl+L"},
		}
	}
}

// applyButtonDefaults fills the customizable buttons with per-agent defaults
// when the preset leaves them empty; an explicit [buttons] table always wins.
func applyButtonDefaults(p *Preset) {
	cmdLine := strings.Join(append([]string{p.Command}, p.Args...), " ")
	if p.Buttons.Custom2 == "" && strings.Contains(strings.ToLower(cmdLine), "claude") {
		p.Buttons.Custom2 = "/compact"
	}
}
