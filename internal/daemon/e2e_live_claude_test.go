//go:build live

package daemon

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
)

// Throwaway live check (go test -tags live -run LiveClaude): a real claude
// session must, after its transcript overflows the screen, replay the whole
// conversation to a fresh subscriber.
func TestLiveClaudeReplayCarriesFullTranscript(t *testing.T) {
	s := NewSession(1, "live", Preset{Command: "claude", Cwd: t.TempDir()})
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer s.Kill()
	if err := s.Resize(80, 24); err != nil {
		t.Fatalf("resize: %v", err)
	}
	time.Sleep(8 * time.Second) // let the TUI draw

	prompt := "print the lines ZEBRA-1 through ZEBRA-60, one per line, nothing else, no tools"
	if err := s.WriteText(prompt + "\r"); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.Now().Add(120 * time.Second)
	var all string
	for time.Now().Before(deadline) {
		time.Sleep(5 * time.Second)
		_, snap := s.SubscribeWithScrollback(&connWriter{})
		all = string(bytes.Join(snap, nil))
		// ZEBRA-59 appears only in the answer (the prompt names 1 and 60).
		if strings.Contains(all, "ZEBRA-59") {
			break
		}
	}
	for _, want := range []string{"ZEBRA-1", "ZEBRA-30", "ZEBRA-60", prompt[:30]} {
		if !strings.Contains(all, want) {
			t.Errorf("replay missing %q", want)
		}
	}
	fmt.Printf("replay size: %d bytes\n", len(all))
	if i := strings.Index(all, "ZEBRA-1"); i >= 0 {
		fmt.Printf("first ZEBRA at byte %d\n", i)
	}
}
