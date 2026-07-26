package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	src := `
[preset.api-refactor]
agent = "claude"
cwd = "~/repos/api"
command = "claude"

[preset.api-refactor.buttons]
custom_1 = "Ctrl+O"
custom_2 = "/compact"
`
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	p, ok := cfg.Presets["api-refactor"]
	if !ok {
		t.Fatal("preset missing")
	}
	if p.Agent != "claude" || p.Cwd != "~/repos/api" || p.Command != "claude" {
		t.Fatalf("preset fields wrong: %+v", p)
	}
	if p.Buttons.Custom1 != "Ctrl+O" {
		t.Fatalf("custom1 wrong: %v", p.Buttons.Custom1)
	}
	if p.Buttons.Custom2 != "/compact" {
		t.Fatalf("custom2 wrong: %v", p.Buttons.Custom2)
	}
}

func TestLoadConfigEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Presets) != 0 {
		t.Fatalf("expected empty presets, got %d", len(cfg.Presets))
	}
}

func TestKeysForCommand(t *testing.T) {
	cases := []struct {
		name    string
		cmdLine string
		wantRej []string
		wantMod []string
		wantInt []string
		wantNew []string
	}{
		{"claude direct", "claude", []string{"Esc"}, []string{"Shift+Tab"}, []string{"Esc"}, []string{"/clear", "Enter"}},
		{"claude wrapped", "ollama launch claude --model gemma4:e4b", []string{"Esc"}, []string{"Shift+Tab"}, []string{"Esc"}, []string{"/clear", "Enter"}},
		{"codex", "codex", []string{"Esc"}, []string{"Shift+Tab"}, []string{"Esc"}, []string{"/new", "Enter"}},
		{"opencode", "opencode", []string{"Esc"}, []string{"Shift+Tab"}, []string{"Esc"}, []string{"Ctrl+X", "n"}},
		{"shell", "bash", []string{"Ctrl+C"}, nil, []string{"Ctrl+C"}, []string{"Ctrl+L"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			k := keysForCommand(c.cmdLine)
			if !equalSeq(k.reject, c.wantRej) {
				t.Errorf("reject = %v, want %v", k.reject, c.wantRej)
			}
			if !equalSeq(k.modeSwitch, c.wantMod) {
				t.Errorf("mode_switch = %v, want %v", k.modeSwitch, c.wantMod)
			}
			if !equalSeq(k.interrupt, c.wantInt) {
				t.Errorf("interrupt = %v, want %v", k.interrupt, c.wantInt)
			}
			if !equalSeq(k.newChat, c.wantNew) {
				t.Errorf("new_chat = %v, want %v", k.newChat, c.wantNew)
			}
		})
	}
}

func TestApplyButtonDefaultsKeepsExplicit(t *testing.T) {
	// Claude sessions get a /compact default on custom_2, but a user
	// [buttons] table must win; only empty fields are filled.
	p := Preset{Command: "claude"}
	applyButtonDefaults(&p)
	if p.Buttons.Custom2 != "/compact" {
		t.Fatalf("empty custom_2 not defaulted: %v", p.Buttons.Custom2)
	}
	p = Preset{Command: "claude", Buttons: Buttons{Custom2: "/clear"}}
	applyButtonDefaults(&p)
	if p.Buttons.Custom2 != "/clear" {
		t.Fatalf("explicit custom_2 overridden: %v", p.Buttons.Custom2)
	}
}

// equalSeq treats nil and an empty (zero-len) slice as equal for comparison.
func equalSeq(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
